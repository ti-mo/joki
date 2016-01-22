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
  "github.com/spf13/viper"
  "log"
  "reflect"
)

// Compiles a stringmap of `probemaps` from a Viper configuration.
// TODO: Split into readable blocks
func ReadConfig(probesets *map[string]probemap) {

  influxkeys := []string{"measurement", "db", "host", "port", "user", "pass"}

  // Config sanity check
  if !viper.InConfig("influxdb") {
    log.Fatal("Please configure influxdb in config.toml")
  }

  for _, key := range influxkeys {
    keyname := "influxdb." + key
    if !viper.IsSet(keyname) {
      log.Fatal("Please set `influxdb.", key, "` in config.toml")
    }
  }

  if !viper.InConfig("probes") {
    log.Fatal("Please configure probes in config.toml")
  }
  if !viper.InConfig("targets") {
    log.Fatal("Please define targets in config.toml")
  }

  vProbeSets := viper.GetStringMap("probes")

  if viper.GetBool("debug") {
    fmt.Printf("\nProbesets: %v\n", vProbeSets)
  }

  for vProbeMap, vProbes := range vProbeSets {

    if viper.GetBool("debug") {
      fmt.Println("Adding", vProbeMap, "to probesets")
    }

    // Add probemap to probesets and initialize empty probes stringmap
    (*probesets)[vProbeMap] = probemap{
      name:    vProbeMap,
      probes:  map[string]probe{},
      targets: map[string]target{},
    }

    // Declare probes inside the current probemap
    for probename, tosvalue := range vProbes.(map[string]interface{}) {
      if viper.GetBool("debug") {
        fmt.Printf("probename: %v, tosvalue: %v\n", probename, tosvalue)
      }

      if reflect.TypeOf(tosvalue).Kind() != reflect.Int64 {
        log.Fatal("TOS value for probe ", probename, " is not an integer")
      }

      (*probesets)[vProbeMap].probes[probename] = probe{
        name: probename,
        tos:  int(tosvalue.(int64)),
      }
    }
  }

  // Load targets
  vTargets := viper.GetStringMap("targets")

  // Get Viper Target objects and parse them into structs
  for vTname, vTarget := range vTargets {

    avTarget, ok := vTarget.(map[string]interface{})
    if !ok {
      log.Fatal("Error while parsing configuration for target", vTname)
    }

    tLongName, ok := avTarget["name"].(string)
    if !ok {
      log.Fatal("Error parsing ", vTname, "'s target name")
    }

    tAddress, ok := avTarget["address"].(string)
    if !ok {
      log.Fatal("Error parsing address for target ", vTname)
    }

    // Use Viper functions to get the 'links' slice.
    tLinks := viper.GetStringSlice("targets." + vTname + ".links")

    // Assign this to all probeMaps
    if len(tLinks) == 0 || tLinks[0] == "all" {
      for pSet, pMap := range *probesets {
        if viper.GetBool("debug") {
          fmt.Println("Adding probe", vTname, "to probeset", pSet, "[all]")
        }
        pMap.targets[vTname] = target{name: vTname, longname: tLongName, address: tAddress}
      }
    } else {
      // Assign Targets to their defined ProbeMaps
      for _, tProbe := range tLinks {
        // Look up tProbe in probesets
        if pMap, ok := (*probesets)[tProbe]; ok {
          if viper.GetBool("debug") {
            fmt.Println("Adding target", vTname, "to probeset", tProbe)
          }
          pMap.targets[vTname] = target{name: vTname, longname: tLongName, address: tAddress}
        } else {
          log.Printf("Missing probe %s defined in %s, ignoring.", tProbe, vTname)
        }
      }
    }
  }
}
