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
  vprobesets := viper.GetStringMap("probes")
  fmt.Printf("%v\n", vprobesets)

  for vpmname, vprobes := range vprobesets {

    fmt.Println("Adding", vpmname, "to probesets")
    fmt.Printf("vprobes: %v\n", vprobes)

    // Add probemap to probesets and initialize empty probes stringmap
    (*probesets)[vpmname] = probemap{name: vpmname, probes: map[string]probe{}}

    // Declare probes inside the current probemap
    for probename, tosvalue := range vprobes.(map[string]interface{}) {
      fmt.Printf("probename: %v, tosvalue: %v\n", probename, tosvalue)
      (*probesets)[vpmname].probes[probename] = probe{name: probename, tos: tosvalue.(string)}
    }
  }

  // Load targets
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

  // Config error handling
  // TODO: extend this with more targeted info, like config search path etc.
  if err != nil {
    log.Fatal("Error loading configuration - exiting.")
  } else {
    fmt.Println("GoPing configuration successfully loaded.")
  }

  ReadConfig(&targets, &probesets)
}
