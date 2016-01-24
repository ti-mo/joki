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
  fpparams := []string{"-B 1", "-D", "-r 1", "-i 10", "-l", "-e"}

  // Build FPing Parameters
  fpargs := append(fpparams, "-O", strconv.Itoa(pval.tos))
  fpargs = append(fpargs, "-p", strconv.Itoa(viper.GetInt("rate")))
  fpargs = append(fpargs, "-Q", strconv.Itoa(viper.GetInt("interval")))

  fpargs = append(fpargs, pmap.TargetSlice()...)

  fmt.Printf("%s - starting worker %s, %d: %s\n", pset, pval.name, pval.tos, strings.Join(pmap.TargetSlice(), " "))

  // exec.Command() uses LookPath internally to look up fping binary path
  cmd := exec.Command("fping", fpargs...)

  // stdout, err := cmd.StdoutPipe()
  // fatalErr(err)

  stderr, err := cmd.StderrPipe()
  fatalErr(err)

  err = cmd.Start()
  fatalErr(err)

  // fping echoes all results to stderr
  buff := bufio.NewScanner(stderr)

  // Listen for FPing echo events
  for buff.Scan() {
    // Only ever act if result is valid
    // Timestamp lines will always come back with result.valid set to false
    if result := PingParser(buff.Text()); result.valid {

      result.probename = pval.name
      result.probeset = pset
      result.target = pmap.TargetStringMapRev()[result.host]

      WritePoints(dbclient, result, dchan)

      if viper.GetBool("debug") {
        if result.up {
          dchan <- fmt.Sprintf("%s - %s [%s] loss: %d%%, min: %.2f, avg: %.2f, max: %.2f", result.probeset, result.target, result.host, result.losspct, result.min, result.avg, result.max)
        } else {
          dchan <- fmt.Sprintf("[%s] is down", result.host)
        }
      }
    }
  }
}

// Parse FPing output line by line and return a pingresult
// Example:
// [15:51:41]
// (    0     1       2       3    4         5      6        7     )
// google.com : xmt/rcv/%loss = 1/1/0%, min/avg/max = 10.6/10.6/10.6
// test.com   : xmt/rcv/%loss = 1/0/100%
func PingParser(text string) pingresult {

  result := pingresult{valid: false, up: false}

  // Make sure hostname can be resolved
  if strings.ContainsAny(text, "address not found") {
    return result
  }

  // Returns a slice of strings, split over whitespace as defined in unicode.isSpace
  fields := strings.Fields(text)

  // Timestamp is echoed on a separate line once every polling cycle -Q
  // Only run the parser on lines that contain more than one field
  // result.valid will remain false and will not be inserted into tsdb
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

    // 'valid' means okay for insertion into tsdb
    result.sent, result.recv, result.losspct, result.valid = sent, recv, losspct, true

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

  return result
}

func WritePoints(dbclient client.Client, point pingresult, dchan chan<- string) {
  measurement := viper.GetString("influxdb.measurement")
  hostname, _ := os.Hostname()

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

  // Create new point batch
  batch, _ := client.NewBatchPoints(client.BatchPointsConfig{
    Database:  viper.GetString("influxdb.db"),
    Precision: "ns",
  })

  // Create point and add to batch
  influxpt, err := client.NewPoint(measurement, tags, fields, time.Now())

  if err != nil {
    dchan <- err.Error()
  }

  batch.AddPoint(influxpt)

  err = dbclient.Write(batch)

  fatalErr(err)
}
