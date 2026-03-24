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
	PartitioningDefault            = false
	HealthCareFlagDefault          = false
	IgnoreHealthWarningFlagDefault = true
	HealthcheckPortDefault         = 51516
)

type GPUFlags struct {
	Healthcare          bool
	IgnoreHealthWarning bool // true if Warning status means healthy, false otherwise. Default: true
	HealthcheckPort     int
	XPUMDSocketFilePath string
}

func main() {
	gpuFlags := GPUFlags{}
	cliFlags := []cli.Flag{
		&cli.BoolFlag{
			Name:        "health-monitoring",
			Aliases:     []string{"m"},
			Usage:       "Actively monitor device health information from XPUManager and update ResourceSlice.",
			Value:       HealthCareFlagDefault,
			Destination: &gpuFlags.Healthcare,

			EnvVars: []string{"HEALTH_MONITORING"},
		},
		&cli.BoolFlag{
			Name:        "ignore-health-warning",
			Aliases:     []string{"w"},
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
		&cli.StringFlag{
			Name:        "xpumd-socket",
			Aliases:     []string{"x"},
			Usage:       "Path to XPUM daemon (v2.0+) socket file. Requires [-m|--health-monitoring] to be enabled.",
			Value:       DefaultXPUMDSocketPath,
			Destination: &gpuFlags.XPUMDSocketFilePath,
			EnvVars:     []string{"XPUMD_SOCKET"},
		},
	}

	if err := helpers.NewApp(device.DriverName, newDriver, cliFlags, &gpuFlags).Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
