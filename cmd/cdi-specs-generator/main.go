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
	"strings"

	"github.com/spf13/cobra"
	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"

	gpuCdihelpers "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/cdihelpers"
	gpuDevice "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
	gpuDiscovery "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/discovery"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/helpers"

	gaudiCdihelpers "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gaudi/cdihelpers"
	gaudiDevice "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gaudi/device"
	gaudiDiscovery "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gaudi/discovery"
)

var (
	supportedDevices = map[string]bool{
		"gpu":   true,
		"gaudi": true,
	}
	version = "v0.3.0"
)

func main() {
	command := newCommand()
	err := command.Execute()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}

func cobraRunFunc(cmd *cobra.Command, args []string) error {
	cdiDir := cmd.Flag("cdi-dir").Value.String()
	namingStyle := cmd.Flag("naming").Value.String()

	fmt.Println("Refreshing CDI registry")
	if err := cdiapi.Configure(cdiapi.WithSpecDirs(cdiDir)); err != nil {
		fmt.Printf("unable to refresh the CDI registry: %v", err)
		return err
	}

	cdiCache, err := cdiapi.NewCache(cdiapi.WithAutoRefresh(false), cdiapi.WithSpecDirs(cdiDir))
	if err != nil {
		return err
	}

	dryRun := cmd.Flag("dry-run").Value.String() == "true"

	for _, argx := range args {
		switch strings.ToLower(argx) {
		case "gpu":
			if err := handleGPUDevices(cdiCache, namingStyle, dryRun); err != nil {
				return err
			}
		case "gaudi":
			if err := handleGaudiDevices(cdiCache, namingStyle, dryRun); err != nil {
				return err
			}
		}
	}

	if dryRun {
		return nil
	}

	if err := cdiCache.Refresh(); err != nil {
		return err
	}

	// Fix CDI spec permissions as the default permission (600) prevents
	// use without root or sudo:
	// https://github.com/cncf-tags/container-device-interface/issues/224
	specs := cdiCache.GetVendorSpecs(gpuDevice.CDIVendor) // Vendor is same for both gpu and gaudi
	for _, spec := range specs {
		if err := os.Chmod(spec.GetPath(), 0o644); err != nil {
			return err
		}
	}

	return nil
}

func newCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "intel-cdi-specs-generator [--cdi-dir=<cdi directory>] [--naming=<style>] <gpu | gaudi>",
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
		RunE: cobraRunFunc,
	}

	cmd.Version = version
	cmd.Flags().BoolP("version", "v", false, "Show the version of the binary")
	cmd.Flags().String("cdi-dir", "/etc/cdi", "CDI spec directory")
	cmd.Flags().String("naming", "classic", "Naming of CDI devices. Options: classic, machine")
	cmd.Flags().BoolP("dry-run", "n", false, "Dry-run, do not create CDI manifests")
	cmd.SetVersionTemplate("Intel CDI Specs Generator Version: {{.Version}}\n")

	return cmd
}

func handleGPUDevices(cdiCache *cdiapi.Cache, namingStyle string, dryRun bool) error {
	sysfsDir := helpers.GetSysfsRoot(gpuDevice.SysfsDRMpath)

	fmt.Println("Scanning for GPUs")

	detectedDevices := gpuDiscovery.DiscoverDevices(sysfsDir, namingStyle)
	if len(detectedDevices) == 0 {
		fmt.Println("No supported devices detected")
	}

	fmt.Println("Detected supported devices")
	for gpuName, gpu := range detectedDevices {
		fmt.Printf("GPU: %v=%v (%v)\n", gpuDevice.CDIKind, gpuName, gpu.ModelName)
	}

	if dryRun {
		return nil
	}

	// syncDetectedDevicesWithCdiRegistry overrides uid in detecteddevices from existing cdi spec
	if err := gpuCdihelpers.SyncDetectedDevicesWithRegistry(cdiCache, detectedDevices, true); err != nil {
		fmt.Printf("unable to sync detected devices to CDI registry: %v", err)
		return err
	}

	return nil
}

func handleGaudiDevices(cdiCache *cdiapi.Cache, namingStyle string, dryRun bool) error {
	sysfsDir := helpers.GetSysfsRoot(gaudiDevice.SysfsAccelPath)

	fmt.Println("Scanning for Gaudi accelerators")

	detectedDevices := gaudiDiscovery.DiscoverDevices(sysfsDir, namingStyle)
	if len(detectedDevices) == 0 {
		fmt.Println("No supported devices detected")
	}

	fmt.Println("Detected supported devices")
	for gaudiName, gaudi := range detectedDevices {
		fmt.Printf("Gaudi: %v=%v (%v)\n", gaudiDevice.CDIKind, gaudiName, gaudi.ModelName)
	}

	if dryRun {
		return nil
	}

	// syncDetectedDevicesWithCdiRegistry overrides uid in detecteddevices from existing cdi spec
	if err := gaudiCdihelpers.SyncDetectedDevicesWithRegistry(cdiCache, detectedDevices, true); err != nil {
		fmt.Printf("unable to sync detected devices to CDI registry: %v", err)
		return err
	}

	return nil
}
