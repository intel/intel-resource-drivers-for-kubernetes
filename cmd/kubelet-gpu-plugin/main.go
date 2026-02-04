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
	HealthcareIntervalFlagMin      = 1
	HealthcareIntervalFlagMax      = 3600
	HealthcareIntervalFlagDefault  = 5

	// The following limits have no inherent default; a value of 0 means "flag not provided".
	// We use *Unset naming instead of *Default to avoid implying a meaningful default value.
	HealthCoreThermalLimitUnset   = 0
	HealthCoreThermalLimitMin     = 1
	HealthCoreThermalLimitMax     = 130
	HealthMemoryThermalLimitUnset = 0
	HealthMemoryThermalLimitMin   = 1
	HealthMemoryThermalLimitMax   = 100
	HealthPowerLimitUnset         = 0
	HealthPowerLimitMin           = 1
	HealthPowerLimitMax           = 300
)

type GPUFlags struct {
	Partitioning        bool
	Healthcare          bool
	IgnoreHealthWarning bool // true if Warning status means healthy, false otherwise. Default: true
	HealthcareInterval  int
	CoreThermalLimit    int
	MemoryThermalLimit  int
	PowerLimit          int
}

func main() {
	gpuFlags := GPUFlags{
		Partitioning:       PartitioningDefault,
		Healthcare:         HealthCareFlagDefault,
		HealthcareInterval: HealthcareIntervalFlagDefault,
	}
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
			Name:        "health-interval",
			Aliases:     []string{"i"},
			Usage:       fmt.Sprintf("Number of seconds between health-monitoring checks [%v ~ %v]", HealthcareIntervalFlagMin, HealthcareIntervalFlagMax),
			Value:       HealthcareIntervalFlagDefault,
			Destination: &gpuFlags.HealthcareInterval,
			EnvVars:     []string{"HEALTH_INTERVAL"},
		},
		&cli.IntFlag{
			Name:        "core-thermal-limit",
			Usage:       fmt.Sprintf("Temperature threshold value [%v ~ %v] in degrees Celsius for xpu-smi health config", HealthCoreThermalLimitMin, HealthCoreThermalLimitMax),
			Value:       HealthCoreThermalLimitUnset,
			Destination: &gpuFlags.CoreThermalLimit,
			EnvVars:     []string{"CORE_THERMAL_LIMIT"},
		},
		&cli.IntFlag{
			Name:        "memory-thermal-limit",
			Usage:       fmt.Sprintf("Temperature threshold value [%v ~ %v] in degrees Celsius for xpu-smi health config", HealthMemoryThermalLimitMin, HealthMemoryThermalLimitMax),
			Value:       HealthMemoryThermalLimitUnset,
			Destination: &gpuFlags.MemoryThermalLimit,
			EnvVars:     []string{"MEMORY_THERMAL_LIMIT"},
		},
		&cli.IntFlag{
			Name:        "power-limit",
			Usage:       fmt.Sprintf("Power usage threshold value [%v ~ %v] in watts for the xpu-smi health config", HealthPowerLimitMin, HealthPowerLimitMax),
			Value:       HealthPowerLimitUnset,
			Destination: &gpuFlags.PowerLimit,
			EnvVars:     []string{"POWER_LIMIT"},
		},
		&cli.BoolFlag{
			Name:        "partitioning-management",
			Aliases:     []string{"p"},
			Usage:       "Manage partitioning physical devices into virtual. [Not Supported]",
			Value:       PartitioningDefault,
			Destination: &gpuFlags.Partitioning,
			EnvVars:     []string{"PARTITIONING"},
		},
	}

	if err := helpers.NewApp(device.DriverName, newDriver, cliFlags, &gpuFlags).Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
