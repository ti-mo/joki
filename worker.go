// Copyright © 2016 Timo Beckers <timo@incline.eu>.
//
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

// GoPing is a simple SmokePing substitute that supports
// setting ToS (DSCP) values, pings multiple targets and
// sends all values to InfluxDB.

package main

import (
  "bufio"
  "errors"
  "fmt"
  "github.com/influxdata/influxdb/client/v2"
  "github.com/spf13/viper"
  "os"
  "os/exec"
  "strconv"
  "strings"
  "time"
)

// Runs PingWorkers for every probe in every probemap
// Sends debug output to a given string channel
func RunWorkers(probesets map[string]probemap, dbclient client.Client, dchan chan<- string) {
  // Start a PingWorker (FPing instance) for every probe
  for pset, pmap := range probesets {
    if viper.GetBool("debug") {
      pmap.DumpTargets()
    }
    for _, pval := range pmap.probes {
      // Only start workers for probes that have at least one target
      if len(pmap.targets) > 0 {
        // Stagger workers to avoid hitting rate limits when pinging
        time.Sleep(time.Duration(33) * time.Millisecond)
        go PingWorker(pmap, pset, pval, dbclient, dchan)
      }
    }
  }
}

// PingWorker starts an fping instance given:
// - the probemap containing a list of targets
// - the probe containing the name and ToS value used to start the fping instance
// - the feedback channel used for debugging
// Calls PingParser on every FPing event and sends the result to WritePoints
func PingWorker(pmap probemap, pset string, pval probe, dbclient client.Client, dchan chan<- string) {
  fpparams := []string{"-B 1", "-D", "-r 1", "-i 10", "-e", "-u"}

  // Build FPing Parameters
  fpargs := append(fpparams, "-O", strconv.Itoa(pval.tos))
  fpargs = append(fpargs, "-p", strconv.Itoa(viper.GetInt("interval")))
  fpargs = append(fpargs, "-C", strconv.Itoa(viper.GetInt("cycle")))

  fpargs = append(fpargs, pmap.TargetSlice()...)

  backoff := 0

  fmt.Printf("%s - starting worker %s, %d: %s\n", pset, pval.name, pval.tos, strings.Join(pmap.TargetSlice(), " "))

  // Run FPing -C for one sequence (polling cycle)
  // batch all the results received from PingParser()
  // and send the batch to InfluxDB with WritePoints()
  for {
    // exec.Command() uses LookPath internally to look up fping binary path
    cmd := exec.Command("fping", fpargs...)

    stderr, err := cmd.StderrPipe()
    fatalErr(err)

    // Run FPing process
    err = cmd.Start()
    fatalErr(err)

    pingresults := make([]pingresult, 0)

    // fmt.Println("waiting.")
    // Wait for output from FPing
    // Compile a slice of pingresults returned by PingParser
    buff := bufio.NewScanner(stderr)
    for buff.Scan() {
      if viper.GetBool("debug") {
        fmt.Println(buff.Text())
      }

      // PingParser() returns err when FPing says "address not found"
      if result, err := PingParser(buff.Text()); err == nil {

        // Reset backoff factor on successful parse
        backoff = 0

        // Fill in missing data from FPing output to pass to InfluxDB as tags
        result.probename = pval.name
        result.probeset = pset
        result.target = pmap.TargetStringMapRev()[result.host]

        pingresults = append(pingresults, result)

        if viper.GetBool("debug") {
          if result.up {
            dchan <- fmt.Sprintf("%s - %s [%s] loss: %d%%, min: %.2f, avg: %.2f, max: %.2f", result.probeset, result.target, result.host, result.losspct, result.min, result.avg, result.max)
          } else {
            dchan <- fmt.Sprintf("[%s] is down", result.host)
          }
        }
      } else if err != nil {
        // FPing returns with "address not found"
        logErr(errors.New(fmt.Sprintf("%s - %s", pval.name, err.Error())))
      }
    }

    cmd.Wait()

    // Wait for at least <interval>ms before starting the next cycle
    timer := time.NewTimer(time.Millisecond * time.Duration(viper.GetInt("interval")))

    // Result processing happens async, this way the intervals don't desync
    if len(pingresults) > 0 {
      go WritePoints(dbclient, pingresults, dchan)
    } else {
      // Workers that yield nothing are put on incremental backoff
      if backoff <= 9 {
        backoff++
      }
      timer = time.NewTimer(time.Minute * time.Duration(backoff))
      logErr(errors.New(fmt.Sprintf("Worker %s yielded no results, sleeping for %d minute(s)", pval.name, backoff)))
    }

    <-timer.C

  }
}

// Parse FPing output line by line and return a pingresult
// Example:
// $ fping -C 2 -u blah.test 10.1.1.1 8.8.8.8 google.com
// blah.test address not found
// 10.1.1.1   : 0.24 0.28
// 8.8.8.8    : 30.31 29.13
// google.com : 10.51 12.89
func PingParser(text string) (pingresult, error) {

  result := pingresult{up: false}

  // Make sure hostname can be resolved
  if strings.Contains(text, "address not found") {
    return result, errors.New(text)
  }

  // Returns a slice of strings, split over whitespace as defined in unicode.isSpace
  fields := strings.Fields(text)

  // Ignore empty lines
  if len(fields) > 1 {

    result.host = fields[0]
    lossString := fields[4]

    lossData := strings.FieldsFunc(lossString, isSlash)

    // Strip optional comma when host is up
    lossData[2] = strings.TrimRight(lossData[2], ",")
    // Strip percentage from %loss to interpret as int
    lossData[2] = strings.TrimRight(lossData[2], "%")

    sent, err := strconv.Atoi(lossData[0])
    fatalErr(err)
    recv, err := strconv.Atoi(lossData[1])
    fatalErr(err)
    losspct, err := strconv.Atoi(lossData[2])
    fatalErr(err)

    result.sent, result.recv, result.losspct = sent, recv, losspct

    // Result has exactly 8 fields if host is up
    if len(fields) == 8 {
      rttString := fields[7]
      rttData := strings.FieldsFunc(rttString, isSlash)

      min, err := strconv.ParseFloat(rttData[0], 64)
      fatalErr(err)
      avg, err := strconv.ParseFloat(rttData[1], 64)
      fatalErr(err)
      max, err := strconv.ParseFloat(rttData[2], 64)
      fatalErr(err)

      // Target is confirmed to be up
      result.min, result.avg, result.max, result.up = min, avg, max, true
    }
  }

  return result, nil
}

func WritePoints(dbclient client.Client, points []pingresult, dchan chan<- string) {
  measurement := viper.GetString("influxdb.measurement")
  hostname, _ := os.Hostname()

  // Create new point batch
  batch, _ := client.NewBatchPoints(client.BatchPointsConfig{
    Database:  viper.GetString("influxdb.db"),
    Precision: "ns",
  })

  for _, point := range points {
    fields := map[string]interface{}{
      "losspct": point.losspct,
    }

    tags := map[string]string{
      "src_host":    hostname,
      "target_host": point.host,
      "target_name": point.target,
    }

    // min, max, avg only given when point.up == true
    if point.up {
      fields["min"] = point.min
      fields["avg"] = point.avg
      fields["max"] = point.max
    }

    if point.probename != "" {
      tags["probe"] = point.probename
    }

    if point.probeset != "" {
      tags["probe_set"] = point.probeset
    }

    // Create point and add to batch
    influxpt, err := client.NewPoint(measurement, tags, fields, time.Now())

    if err != nil {
      dchan <- err.Error()
    }

    batch.AddPoint(influxpt)
  }

  err := dbclient.Write(batch)

  fatalErr(err)
}
