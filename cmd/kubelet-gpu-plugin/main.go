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

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/helpers"
)

const (
	PartitioningDefault            = false
	HealthCareFlagDefault          = false
	HealthCareOptionalFlagDefault  = false
	IgnoreHealthWarningFlagDefault = true
	HealthcheckPortDefault         = 51516
	DefaultManageBinding           = true
)

type GPUFlags struct {
	Healthcare          bool
	HealthcareOptional  bool
	IgnoreHealthWarning bool // true if Warning status means healthy, false otherwise. Default: true
	HealthcheckPort     int
	XPUMDSocketFilePath string
	ManageBinding       bool
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
			Name:        "health-monitoring-optional",
			Aliases:     []string{"o"},
			Usage:       "Allow infinite polling for XPUM daemon without restart.",
			Value:       HealthCareOptionalFlagDefault,
			Destination: &gpuFlags.HealthcareOptional,

			EnvVars: []string{"HEALTH_MONITORING_OPTIONAL"},
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
		&cli.BoolFlag{
			Name:        "manage-binding",
			Aliases:     []string{"b"},
			Usage:       "Actively bind the GPU to DRM or VFIO driver based on requested DeviceClass.",
			Value:       DefaultManageBinding,
			Destination: &gpuFlags.ManageBinding,
			EnvVars:     []string{"MANAGE_BINDING"},
		},
	}

	if err := helpers.NewApp(device.DriverName, newDriver, cliFlags, &gpuFlags).Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
