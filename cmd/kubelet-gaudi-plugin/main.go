//
// Copyright (C) 2022-2026 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0
//

package main

import (
	"fmt"
	"os"

	"github.com/urfave/cli/v2"

	gaudi "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gaudi/device"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/helpers"
)

type GaudiFlags struct {
	GaudiHookPath      string
	Healthcare         bool
	HealthcareInterval int
}

const (
	HealthCareFlagDefault         = true
	HealthcareIntervalFlagMin     = 1
	HealthcareIntervalFlagMax     = 3600
	HealthcareIntervalFlagDefault = 5
)

func main() {
	gaudiFlags := GaudiFlags{
		GaudiHookPath:      gaudi.DefaultHabanaHookPath,
		Healthcare:         HealthCareFlagDefault,
		HealthcareInterval: HealthcareIntervalFlagDefault,
	}
	cliFlags := []cli.Flag{
		&cli.StringFlag{
			Name:        "gaudi-hook-path",
			Aliases:     []string{"p"},
			Usage:       "full path to the habana-container-hook",
			Value:       "", // Default value is set in getGaudiFlags().
			Destination: &gaudiFlags.GaudiHookPath,
			EnvVars:     []string{"GAUDI_HOOK_PATH"},
		},
		&cli.BoolFlag{
			Name:        "health-monitoring",
			Aliases:     []string{"m"},
			Usage:       "Actively monitor device health and update ResourceSlice. Requires privileges.",
			Value:       HealthCareFlagDefault,
			Destination: &gaudiFlags.Healthcare,
			EnvVars:     []string{"HEALTH_MONITORING"},
		},
		&cli.IntFlag{
			Name:        "health-interval",
			Aliases:     []string{"i"},
			Usage:       fmt.Sprintf("Number of seconds between health-monitoring checks [%v ~ %v]", HealthcareIntervalFlagMin, HealthcareIntervalFlagMax),
			Value:       HealthcareIntervalFlagDefault,
			Destination: &gaudiFlags.HealthcareInterval,
			EnvVars:     []string{"HEALTH_INTERVAL"},
		},
	}

	if err := helpers.NewApp(gaudi.DriverName, newDriver, cliFlags, &gaudiFlags).Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
