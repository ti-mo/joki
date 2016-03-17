// Copyright © 2016 Timo Beckers <timo@incline.eu>.
//
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

// Joki is a simple SmokePing substitute that supports
// setting ToS (DSCP) values, pings multiple targets and
// sends all values to InfluxDB.

package main

import (
  "bufio"
  "errors"
  "fmt"
  "github.com/influxdata/influxdb/client/v2"
  "github.com/spf13/viper"
  "math/rand"
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
        time.Sleep(time.Duration(rand.Intn(viper.GetInt("interval"))) * time.Millisecond)
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
  fpparams := []string{"-B 1", "-D", "-r 1", "-i 25", "-e", "-u", "-q"}

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
      // PingParser() returns err upon a parsing error
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
            dchan <- fmt.Sprintf("%s - %s [%s] loss: %d%%, min: %.2f, avg: %.2f, max: %.2f",
             result.probeset, result.target, result.host, result.losspct,
             result.min, result.avg, result.max)
          } else {
            dchan <- fmt.Sprintf("[%s] is down", pval.name)
            dchan <- fmt.Sprintf("on thread %v", fpargs)
          }
        }
      } else if err != nil {
        logErr(errors.New(fmt.Sprintf("%s - %s - %s", pset, pval.name, err.Error())))
      }
    }

    err = cmd.Wait()
    logErr(err)

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
      logErr(errors.New(fmt.Sprintf("Worker %s - %s  yielded no results, sleeping for %d minute(s)",
        pset, pval.name, backoff)))
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

  // Returns a slice of strings, split over whitespace as defined in unicode.isSpace
  fields := strings.Fields(text)

  // Ignore empty lines and do some sanity checking
  if len(fields) > 1 && fields[1] == ":" {

    result.host = fields[0]
    points := fields[2:]

    var total float64

    for _, point := range points {
      fpoint, err := strconv.ParseFloat(point, 64)
      if err == nil {
        total += fpoint
        result.sent++
        result.recv++

        if fpoint < result.min || result.min == 0.0 {
          result.min = fpoint
        }

        if fpoint > result.max {
          result.max = fpoint
        }
      } else if point == "-" {
        result.sent++
      }
    }

    if result.recv > 0 {
      result.avg = total / float64(result.recv)
      result.losspct = (result.sent - result.recv) * 100 / result.sent
      result.up = true
    } else {
      result.losspct = 100
    }
  } else {
    return result, errors.New(fmt.Sprintf("Error parsing FPing output: %s", text))
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
