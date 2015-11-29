package main

import (
	"os"

	"github.com/elastic/libbeat/beat"
	"github.com/elastic/libbeat/logp"
	winlogbeat "github.com/elastic/winlogbeat/beat"
)

// Depends on go get github.com/josephspurrier/goversioninfo/cmd/goversioninfo
// Run go generate after updating the version number.
//go:generate goversioninfo -icon=beats.ico -ver-major=0 -ver-minor=0 -ver-patch=1

// Version of winlogbeat.
var Version = "0.0.1"

// Name of this beat.
var Name = "winlogbeat"

func main() {
	// Create Beater.
	wlb := &winlogbeat.Winlogbeat{}

	// Initialize beat.
	b := beat.NewBeat(Name, Version, wlb)

	// Additional command line arguments are used to overwrite config options.
	b.CommandLineSetup()

	// Load base config.
	b.LoadConfig()

	// Configures beat.
	err := wlb.Config(b)
	if err != nil {
		logp.Critical("Config error: %v", err)
		os.Exit(1)
	}

	// Run beat. This calls first beater.Setup,
	// then beater.Run and beater.Cleanup in the end
	b.Run()
}
