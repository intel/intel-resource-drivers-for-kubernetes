/*
 * Copyright (c) 2025, Intel Corporation.  All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"fmt"
	"os"

	"github.com/urfave/cli/v2"

	gaudi "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gaudi/device"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/helpers"
)

type GaudiFlags struct {
	Healthcare         bool
	HealthcareInterval int
}

const (
	HealthCareFlagDefault         = false
	HealthcareIntervalFlagMin     = 1
	HealthcareIntervalFlagMax     = 3600
	HealthcareIntervalFlagDefault = 5
)

func main() {
	gaudiFlags := GaudiFlags{
		Healthcare:         HealthCareFlagDefault,
		HealthcareInterval: HealthcareIntervalFlagDefault,
	}

	cliFlags := []cli.Flag{
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
