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

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/helpers"
)

const (
	HealthCareFlagDefault          = false
	IgnoreHealthWarningFlagDefault = true
	HealthcheckPortDefault         = 51516
)

type GPUFlags struct {
	Healthcare          bool
	IgnoreHealthWarning bool // true if Warning status means healthy, false otherwise. Default: true
	HealthcheckPort     int
}

func main() {
	gpuFlags := GPUFlags{}
	cliFlags := []cli.Flag{
		&cli.BoolFlag{
			Name:        "health-monitoring",
			Aliases:     []string{"m"},
			Usage:       "Actively monitor device health and update ResourceSlice. Requires privileges.",
			Value:       HealthCareFlagDefault,
			Destination: &gpuFlags.Healthcare,
			EnvVars:     []string{"HEALTH_MONITORING"},
		},
		&cli.BoolFlag{
			Name:    "ignore-health-warning",
			Aliases: []string{"w"},
			// https://github.com/intel/xpumanager/blob/master/core/src/device/gpu/gpu_device_stub.cpp#L4142
			Usage:       "Ignore temperature & power thresholds and degraded memory health warnings (= react only to critical memory state). Default: true",
			Value:       IgnoreHealthWarningFlagDefault,
			Destination: &gpuFlags.IgnoreHealthWarning,
			EnvVars:     []string{"IGNORE_HEALTH_WARNING"},
		},
		&cli.IntFlag{
			Name:        "healthcheck-port",
			Usage:       "gRPC health check port. Set to -1 to disable.",
			Value:       HealthcheckPortDefault,
			Destination: &gpuFlags.HealthcheckPort,
			EnvVars:     []string{"HEALTHCHECK_PORT"},
		},
	}

	if err := helpers.NewApp(device.DriverName, newDriver, cliFlags, &gpuFlags).Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
