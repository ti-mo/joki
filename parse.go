package main

import (
  "fmt"
  "github.com/spf13/viper"
  "log"
  "os"
  "os/exec"
  "strings"
)

type target struct {
  name, longname, address string
}

type probe struct {
  name string
  tos  string
}

type probemap struct {
  name    string
  probes  map[string]probe
  targets map[string]target
}

// Get a Stringmap of strings for reverse lookups, eg.:
// target["8.8.8.8"] returns the internal name of the target
// This is useful to translate back to a name when parsing output
// for logging purposes after the argument has been handed off to FPing
func (probeMap probemap) TargetStringMapRev() map[string]string {

  targetMap := make(map[string]string)

  for targetName, targetValue := range probeMap.targets {
    targetMap[targetValue.address] = targetName
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

func (t *target) ping(p *probe) {
  fmt.Printf("pinging %s as %s with mark 0x%x\n", t.name, t.address, p.tos)
}

func DumpTargets(targets *map[string]target) {
  for vTname, vTarget := range *targets {
    fmt.Printf("\n[target] %s\n\tLong Name: %s\n\tAddress: %s\n",
      vTname, vTarget.longname, vTarget.address)
  }
}

// Extract probemap data from configuration file
func ReadConfig(probesets *map[string]probemap) {

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

    // Add probemap to probesets and initialize empty probes stringmap
    (*probesets)[vProbeMap] = probemap{name: vProbeMap, probes: map[string]probe{}, targets: map[string]target{}}

    // Declare probes inside the current probemap
    for probename, tosvalue := range vProbes.(map[string]interface{}) {
      fmt.Printf("probename: %v, tosvalue: %v\n", probename, tosvalue)
      (*probesets)[vProbeMap].probes[probename] = probe{name: probename, tos: tosvalue.(string)}
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
        fmt.Println("Adding probe", vTname, "to probeset", pSet, "[all]")
        pMap.targets[vTname] = target{name: vTname, longname: tLongName, address: tAddress}
      }
    } else {
      // Assign Targets to their defined ProbeMaps
      for _, tProbe := range tLinks {
        // Look up tProbe in probesets
        if pMap, ok := (*probesets)[tProbe]; ok {
          fmt.Println("Adding probe", vTname, "to probeset", tProbe)
          pMap.targets[vTname] = target{name: vTname, longname: tLongName, address: tAddress}
        } else {
          log.Printf("Missing probe %s defined in %s, ignoring.", tProbe, vTname)
        }
      }
    }
  }
}

func PingWorker(name string, tos string, targets []string, revtargets map[string]string) {
  //fpargs := []string{"-B 1", "-D", "-r0", "-O 0", "-Q 1", "-p 1000", "-l"}

  fmt.Printf("%s, %s: %s\n", name, tos, strings.Join(targets, " "))
  fmt.Printf("%v\n", revtargets)
}

func main() {

  if _, err := exec.LookPath("fping"); err != nil {
    log.Fatal("FPing binary not found. Exiting.")
  }

  // Declare Data Structures
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

  // Read configuration into probesets
  ReadConfig(&probesets)

  // Target Object Dump
  if viper.GetBool("goping.debug") {
    for _, probemap := range probesets {
      DumpTargets(&probemap.targets)
    }
  }

  // Start FPing Tasks
  for _, probeMap := range probesets {

    // Start a goroutine for every probe
    for _, probeValue := range probeMap.probes {

      PingWorker(probeValue.name, probeValue.tos, probeMap.TargetSlice(), probeMap.TargetStringMapRev())
    }
  }
}
