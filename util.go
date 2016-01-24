// Copyright © 2016 Timo Beckers <timo@incline.eu>.
//
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

// GoPing is a simple SmokePing substitute that supports
// setting ToS (DSCP) values, pings multiple targets and
// sends all values to InfluxDB.

package main

import (
  "fmt"
  "log"
)

type target struct {
  name, longname, address string
}

type probe struct {
  name string
  tos  int
}

type probemap struct {
  name    string
  probes  map[string]probe
  targets map[string]target
}

type pingresult struct {
  host, probename, probeset, target string
  sent, recv, losspct               int
  min, max, avg                     float64
  up                                bool
}

// Get a Stringmap of strings for reverse lookups, eg.:
// target["8.8.8.8"] returns the internal name of the target
// This is useful to translate back to a name when parsing output
// for logging purposes after the argument has been handed off to FPing
func (probeMap probemap) TargetStringMapRev() map[string]string {
  targetMap := make(map[string]string)

  for targetid, targetval := range probeMap.targets {
    targetMap[targetval.address] = targetid
  }

  return targetMap
}

// Returns a slice of strings containing all targets
// Used for generating arguments for the ping worker
func (probeMap probemap) TargetSlice() []string {

  targets := make([]string, 0, len(probeMap.targets))

  for _, probeTarget := range probeMap.targets {
    targets = append(targets, probeTarget.address)
  }

  return targets
}

// Given a probemap, dumps a list of its targets
func (pmap probemap) DumpTargets() {
  for name, target := range pmap.targets {
    fmt.Printf("\n[target] %s\n\tLong Name: %s\n\tAddress: %s\n",
      name, target.longname, target.address)
  }
}

// Wrapper around log.Fatal(err)
// No-ops if err is nil
func fatalErr(err error) {
  if err != nil {
    log.Fatal(err)
  }
}

// Wrapper around log.Println(err)
// No-ops if err is nil
func logErr(err error) {
  if err != nil {
    log.Println(err.Error())
  }
}

// Splitter function that determines if input rune is slash
func isSlash(c rune) bool {
  return c == '/'
}
