package main

import (
  "fmt"
  "github.com/spf13/viper"
  "log"
  "os"
)

type target struct {
  name, longname, address string
  links                   []string
}

type probe struct {
  name string
  tos  string
}

type probemap struct {
  name   string
  probes map[string]probe
}

func (t *target) ping(p *probe) {
  fmt.Printf("pinging %s as %s with mark 0x%x\n", t.name, t.address, p.tos)
}

// Extract probemap data from configuration file
func ReadConfig(targets *map[string]target, probesets *map[string]probemap) {
  // Config sanity check
  if !viper.InConfig("influxdb") {
    log.Fatal("Please configure influxdb in config.toml")
  }
  if !viper.InConfig("probes") {
    log.Fatal("Please configure probes in config.toml")
  }
  if !viper.InConfig("targets") {
    log.Fatal("Please define targets in config.toml")
  }

  // Load probes
  vProbeSets := viper.GetStringMap("probes")
  fmt.Printf("\nProbesets: %v\n", vProbeSets)

  for vProbeMap, vProbes := range vProbeSets {

    fmt.Println("Adding", vProbeMap, "to probesets")
    fmt.Printf("vProbes: %v\n", vProbes)

    // Add probemap to probesets and initialize empty probes stringmap
    (*probesets)[vProbeMap] = probemap{name: vProbeMap, probes: map[string]probe{}}

    // Declare probes inside the current probemap
    for probename, tosvalue := range vProbes.(map[string]interface{}) {
      fmt.Printf("probename: %v, tosvalue: %v\n", probename, tosvalue)
      (*probesets)[vProbeMap].probes[probename] = probe{name: probename, tos: tosvalue.(string)}
    }
  }

  // Load targets
  vTargets := viper.GetStringMap("targets")
  fmt.Printf("\nTargets: %v\n", vTargets)

  // Get Viper Target objects and parse them into structs
  for vTname, vtarget := range vTargets {
    avTarget, ok := vtarget.(map[string]interface{})
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

    fmt.Println("Adding", vTname, "to targets (", avTarget, ")")
    (*targets)[vTname] = target{name: vTname, longname: tLongName, address: tAddress, links: tLinks}

    // Target Object Dump
    if viper.GetBool("goping.debug") {
      fmt.Printf("Object dump for target %s\n\tLong Name: %s\n\tAddress: %s\n\tLinks: %v\n",
        vTname, (*targets)[vTname].longname, (*targets)[vTname].address, (*targets)[vTname].links)
    }
  }

  // TODO: Assign Targets to their defined ProbeMaps
}

func main() {

  // Declare Data Structures
  targets := make(map[string]target)
  probesets := make(map[string]probemap)

  // Get working directory
  pwd, err := os.Getwd()
  if err != nil {
    log.Fatal(err)
  }

  // Viper metaconfiguration
  viper.SetConfigName("config")
  viper.AddConfigPath(pwd)
  err = viper.ReadInConfig()

  // Set Configuration Defaults
  viper.SetDefault("goping.debug", true)

  // Config error handling
  // TODO: extend this with more targeted info, like config search path etc.
  if err != nil {
    log.Fatal("Error loading configuration - exiting.")
  } else {
    fmt.Println("GoPing configuration successfully loaded.")
  }

  ReadConfig(&targets, &probesets)
}
