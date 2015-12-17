package main

import (
  "fmt"
  "github.com/pelletier/go-toml"
  "log"
)

type target struct {
  name, longname, address, links string
}

type probe struct {
  name string
  tos  int
}

type probemap struct {
  name    string
  probes  map[string]*probe
  targets map[string]*target
}

func (t *target) ping(p *probe) {
  fmt.Printf("pinging %s as %s with mark 0x%x\n", t.name, t.address, p.tos)
}

func LoadProbes(config *toml.TomlTree, probemap *probemap) {

}

// Given a Toml (sub)tree, append all targets with
func LoadTargets(config *toml.TomlTree, probemap *probemap) {

}

// Extract probemap data from configuration file
func ReadConfig(config *toml.TomlTree, probemaps *map[string]probemap) {
  // Config sanity check
  if !config.Has("influxdb") {
    log.Fatal("Please configure influxdb in config.toml")
  }
  if !config.Has("targets") {
    log.Fatal("Please define targets in config.toml")
  }
  if !config.Has("probes") {
    log.Fatal("Please configure probes in config.toml")
  }

  // Get objects from configuration
  tomltargets, _ := config.Get("targets").([]*toml.TomlTree)
  tomlprobes, _ := config.Get("probes").([]*toml.TomlTree)

  if len(tomltargets) == 0 {
    log.Fatal("No targets found, exiting..")
  } else {
    fmt.Printf("%d targets found in configuration:\n", len(tomltargets))
  }

  // Build map of probes by 'name'
  for _, v := range tomlprobes {
    pmap := probemap{
      name: v.Get("name").(string),
    }
    (*probemaps)[pmap.name] = pmap
  }

  // Build map of targets by 'name'
  targets := make(map[string]target)

  for _, v := range tomltargets {
    tstruct := target{
      name:     v.Get("name").(string),
      longname: v.Get("longname").(string),
      address:  v.Get("address").(string),
      links:    v.Get("links").([]string),
    }
    targets[tstruct.name] = tstruct
  }

  fmt.Printf("%v\n", targets)

  // name and address need to be set, tos is optional (pass 0 to fping)

  // Build map of targets

  // Return targets

}

func main() {

  config, _ := toml.LoadFile("config.toml")

  probemaps := make(map[string]probemap)

  // Get probemaps from configuration
  ReadConfig(config, &probemaps)

}
