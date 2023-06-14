// Copyright 2023, Franklin "Snaipe" Mathieu <me@snai.pe>
//
// Use of this source-code is govered by the MIT license, which
// can be found in the LICENSE file.

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime/debug"
)

type ConfigCmd struct{}

var version string

func (ConfigCmd) Run() error {
	var config struct {
		Driver struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"driver"`
	}

	config.Driver.Name = "gitlab-runner-kubevirt"
	if binfo, ok := debug.ReadBuildInfo(); ok {
		var k8sdep *debug.Module
		for _, mod := range binfo.Deps {
			if mod.Path == "k8s.io/api" {
				k8sdep = mod
				break
			}
		}

		if version == "" {
			version = binfo.Main.Version
		}
		if version == "(devel)" {
			for _, s := range binfo.Settings {
				if s.Key == "vcs.revision" {
					version = s.Value + " (devel)"
					break
				}
			}
		}
		version = fmt.Sprintf("%v (%v; k8s.io/api: %v)", version, binfo.GoVersion, k8sdep.Version)
	}
	config.Driver.Version = version

	return json.NewEncoder(os.Stdout).Encode(&config)
}
