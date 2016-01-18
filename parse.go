package main

import (
  "bufio"
  "fmt"
  "github.com/spf13/viper"
  "log"
  "os"
  "os/exec"
  "strconv"
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

type pingresult struct {
  host                string
  sent, recv, losspct int
  min, max, avg       float64
  valid, up           bool
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

// Wrapper around log.Fatal(err)
// No-ops is err is nil
func fatalErr(err error) {
  if err != nil {
    log.Fatal(err)
  }
}

func isSlash(c rune) bool {
  return c == '/'
}

// Parse FPing output line by line and return a pingresult
// Example:
// [15:51:41]
// (    0     1       2       3    4         5      6        7     )
// google.com : xmt/rcv/%loss = 1/1/0%, min/avg/max = 10.6/10.6/10.6
// test.com   : xmt/rcv/%loss = 1/0/100%
func PingParser(text string) pingresult {

  // Returns a slice of strings, split over whitespace as defined in unicode.isSpace
  fields := strings.Fields(text)

  result := pingresult{valid: false, up: false}

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

func DumpTargets(targets map[string]target) {
  for vTname, vTarget := range targets {
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
          fmt.Printf("Missing probe %s defined in %s, ignoring.", tProbe, vTname)
        }
      }
    }
  }
}

// PingWorker starts an fping instance given:
// - the feedback channel used for debugging
// - the probemap containing a list of targets
// - the probe containing the name and ToS value used to start the fping instance
func PingWorker(dchan chan<- string, probeMap probemap, probeValue probe) {
  fpparams := []string{"-B 1", "-D", "-r0", "-O 0", "-Q 1", "-p 1000", "-l", "-e"}

  fmt.Printf("Starting worker %s, %s: %s\n", probeValue.name, probeValue.tos, strings.Join(probeMap.TargetSlice(), " "))

  fpargs := append(fpparams, probeMap.TargetSlice()...)

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

  for buff.Scan() {
    // Only ever act if result is valid
    // Timestamp lines will always come back with result.valid set to false
    if result := PingParser(buff.Text()); result.valid {

      // TODO: Insert points into tsdb

      if viper.GetBool("goping.debug") {
        if result.up {
          dchan <- fmt.Sprintf("Host: %s, loss: %d%%, min: %.2f, avg: %.2f, max: %.2f", result.host, result.losspct, result.min, result.avg, result.max)
        } else {
          dchan <- fmt.Sprintf("Host: %s is down", result.host)
        }
      }
    }
  }
}

// Run a PingWorker for every probe in every probemap
func RunWorkers(probesets map[string]probemap, dchan chan<- string) {
  // Start a PingWorker (FPing instance) for every probe
  for _, probeMap := range probesets {
    if viper.GetBool("goping.debug") {
      DumpTargets(probeMap.targets)
    }
    for _, probeValue := range probeMap.probes {
      go PingWorker(dchan, probeMap, probeValue)
    }
  }
}

func main() {

  probesets := make(map[string]probemap)
  dchan := make(chan string)

  if _, err := exec.LookPath("fping"); err != nil {
    log.Fatal("FPing binary not found. Exiting.")
  }

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

  // TODO: extend this with more targeted info, like config search path etc.
  if err != nil {
    log.Fatal("Error loading configuration - exiting.")
  } else {
    fmt.Println("GoPing configuration successfully loaded.")
  }

  ReadConfig(&probesets)

  RunWorkers(probesets, dchan)

  for {
    fmt.Println(<-dchan)
  }
}
