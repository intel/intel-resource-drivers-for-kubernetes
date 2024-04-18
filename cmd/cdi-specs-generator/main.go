/*
 * Copyright (c) 2024, Intel Corporation.  All Rights Reserved.
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
	"strings"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/cdihelpers"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/discovery"
	"github.com/spf13/cobra"
	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"
)

var (
	supportedDevices = map[string]bool{
		"gpu":   true,
		"gaudi": true,
	}
)

func main() {
	command := newCommand()
	err := command.Execute()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}

func newCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "intel-cdi-specs-generator <gpu | gaudi>",
		Short: "Intel CDI Spec Generator",
		Long:  "Intel CDI Specs Generator detects supported accelerators and creates CDI specs for them.",
		Args: func(cmd *cobra.Command, args []string) error {
			// arguments validation
			if err := cobra.MinimumNArgs(1)(cmd, args); err != nil {
				return err
			}

			for _, argx := range args {
				if _, found := supportedDevices[strings.ToLower(argx)]; !found {
					return fmt.Errorf("invalid device type specified: %s", argx)
				}
			}

			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			for _, argx := range args {
				switch strings.ToLower(argx) {
				case "gpu":
					if err := handleGPUDevices(); err != nil {
						return err
					}
				case "gaudi":
					if err := handleGaudiDevices(); err != nil {
						return err
					}
				}
			}
			return nil
		},
	}

	return cmd
}

func handleGPUDevices() error {
	sysfsDir := device.GetSysfsDir()

	detectedDevices := discovery.DiscoverDevices(sysfsDir)
	if len(detectedDevices) == 0 {
		fmt.Println("No supported devices detected")
	}

	fmt.Println("Refreshing CDI registry")
	if err := cdiapi.Configure(cdiapi.WithSpecDirs(device.CDIRoot)); err != nil {
		fmt.Printf("unable to refresh the CDI registry: %v", err)
		return err
	}
	cdiCache := cdiapi.GetDefaultCache()

	// syncDetectedDevicesWithCdiRegistry overrides uid in detecteddevices from existing cdi spec
	if err := cdihelpers.SyncDetectedDevicesWithRegistry(cdiCache, detectedDevices, true); err != nil {
		fmt.Printf("unable to sync detected devices to CDI registry: %v", err)
	}

	return nil
}

func handleGaudiDevices() error {
	return fmt.Errorf("not implemented")
}
