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
	"github.com/influxdata/influxdb/client/v2"
	"github.com/spf13/viper"
	"log"
	"os"
	"os/exec"
)

func main() {

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
	viper.AddConfigPath("/etc/joki/")
	viper.AddConfigPath("$HOME/.joki/")

	// Configuration Defaults
	// Nested configuration is blocked by spf13/viper issue #71
	viper.SetDefault("debug", false)
	viper.SetDefault("interval", 1000)
	viper.SetDefault("cycle", 10)

	err = viper.ReadInConfig()

	// TODO: extend this with more targeted info, like config search path etc.
	if err != nil {
		log.Fatal("Error loading configuration - exiting.")
	} else {
		fmt.Println("Joki configuration successfully loaded.")
	}

	// Make HTTP client for InfluxDB
	dbclient, err := client.NewUDPClient(client.UDPConfig{
		Addr: fmt.Sprintf("%s:%d", viper.GetString("influxdb.host"), viper.GetInt("influxdb.port")),
	})
	if err != nil {
		fmt.Println("Error creating InfluxDB Client: ", err.Error())
	}
	defer dbclient.Close()

	probesets := make(map[string]probemap)
	ReadConfig(&probesets)

	dchan := make(chan string)
	RunWorkers(probesets, dbclient, dchan)

	for {
		fmt.Println(<-dchan)
	}
}
