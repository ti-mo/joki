// Copyright © 2016 Timo Beckers <timo@incline.eu>.
//
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

// Joki is a simple SmokePing substitute that supports
// setting ToS (DSCP) values, pings multiple targets and
// sends all values to InfluxDB.

package main

import (
	"fmt"
	"log"
	"reflect"

	"github.com/spf13/viper"
)

// ReadConfig compiles a stringmap of `probemaps` from a Viper configuration.
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

	vprobesets := viper.GetStringMap("probes")

	if viper.GetBool("debug") {
		fmt.Printf("\nProbesets: %v\n", vprobesets)
	}

	for probesetname, probeset := range vprobesets {

		if viper.GetBool("debug") {
			fmt.Println("Adding", probesetname, "to probesets")
		}

		// Add probemap to probesets and initialize empty probes stringmap
		(*probesets)[probesetname] = probemap{
			name:    probesetname,
			probes:  map[string]probe{},
			targets: map[string]target{},
		}

		// Declare probes inside the current probemap
		for probename, tosvalue := range probeset.(map[string]interface{}) {
			if viper.GetBool("debug") {
				fmt.Printf("probename: %v, tosvalue: %v\n", probename, tosvalue)
			}

			if reflect.TypeOf(tosvalue).Kind() != reflect.Int64 {
				log.Fatal("TOS value for probe ", probename, " is not an integer")
			}

			(*probesets)[probesetname].probes[probename] = probe{
				name: probename,
				tos:  int(tosvalue.(int64)),
			}
		}
	}

	// Load targets
	vtargets := viper.GetStringMap("targets")

	// Get Viper Target objects and parse them into structs
	for vtname, vtarget := range vtargets {

		tmap, ok := vtarget.(map[string]interface{})
		if !ok {
			log.Fatal("Error while parsing configuration for target", vtname)
		}

		tlongname, ok := tmap["name"].(string)
		if !ok {
			log.Fatal("Error parsing ", vtname, "'s target name")
		}

		taddress, ok := tmap["address"].(string)
		if !ok {
			log.Fatal("Error parsing address for target ", vtname)
		}

		// Use Viper functions to get the 'probes' slice.
		tprobes := viper.GetStringSlice("targets." + vtname + ".probes")

		// Assign this to all probeMaps
		if len(tprobes) == 0 || tprobes[0] == "all" {
			for pset, pmap := range *probesets {
				if viper.GetBool("debug") {
					fmt.Println("Adding probe", vtname, "to probeset", pset, "[all]")
				}
				pmap.targets[vtname] = target{name: vtname, longname: tlongname, address: taddress}
			}
		} else {
			// Assign Targets to their defined ProbeMaps
			for _, tprobe := range tprobes {
				// Look up tprobe in probesets
				if pmap, ok := (*probesets)[tprobe]; ok {
					if viper.GetBool("debug") {
						fmt.Println("Adding target", vtname, "to probeset", tprobe)
					}
					pmap.targets[vtname] = target{name: vtname, longname: tlongname, address: taddress}
				} else {
					log.Printf("Missing probe %s defined in %s, ignoring.", tprobe, vtname)
				}
			}
		}
	}
}
