package main

import (
  "github.com/pelletier/go-toml"
  "fmt"
  "log"
)

type target struct {
  name, longname, address, links string
  tos int
}

func (t *target) ping() {
  fmt.Printf("pinging %s as %s with mark 0x%x\n", t.name, t.address, t.tos)
}

func GetTargets(config *toml.TomlTree) {
  // Check if we have at least one target
  if config.Has("target") {
    targets, _ := config.Get("target").([]*toml.TomlTree)
    fmt.Printf("%d targets found in configuration.\n", len(targets))

    // Target Map
    tgtmap := make(map[string]target)

    // Check and append
    for _, v := range targets {
      tstruct := target{
        name: v.Get("name").(string),
        longname: v.Get("longname").(string),
        address: v.Get("address").(string),
        //links: v.Get("links").(string),
      }

      tgtmap[tstruct.name] = tstruct
    }

    fmt.Printf("%v", tgtmap)
  } else {
      log.Fatal("No targets found in config.toml")
  }

  

  // Perform sanity check on all targets
  // name and address need to be set, tos is optional (pass 0 to fping)

  // Build map of targets

  // Return targets

}

func main() {

  config, _ := toml.LoadFile("config.toml")

  GetTargets(config)

}
