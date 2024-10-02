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
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/fakesysfs"
	gaudiDevice "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gaudi/device"
	gpuDevice "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
	helpers "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/plugintesthelpers"
)

var (
	supportedDevices = map[string]bool{
		"gpu":   true,
		"gaudi": true,
	}
	version = "v0.2.0"
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
		Use:   "device-faker <gpu | gaudi>",
		Short: "device-faker",
		Long:  "device-faker creates fake sysfs and devfs in /tmp for Intel GPU or Intel Gaudi based on template ",
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
					if cmd.Flag("new-template").Value.String() == "true" {
						return newTemplate("gpu")
					}
					if cmd.Flag("template").Value.String() == "" {
						return fmt.Errorf("template parameter is missing")
					}
					if err := handleGPUDevices(cmd.Flag("template").Value.String()); err != nil {
						return err
					}
				case "gaudi":
					if cmd.Flag("new-template").Value.String() == "true" {
						return newTemplate("gaudi")
					}
					if cmd.Flag("template").Value.String() == "" {
						return fmt.Errorf("template parameter is missing")
					}
					if err := handleGaudiDevices(cmd.Flag("template").Value.String()); err != nil {
						return err
					}
				}
			}
			return nil
		},
	}

	cmd.Version = version
	cmd.Flags().BoolP("version", "v", false, "Show the version of the binary")
	cmd.Flags().BoolP("new-template", "n", false, "Create new template file for given accelerator")
	cmd.Flags().StringP("template", "t", "", "Template file to populate devices from")
	cmd.SetVersionTemplate("device-faker version: {{.Version}}\n")

	return cmd
}

func handleGPUDevices(templateFilePath string) error {
	devices := make(gpuDevice.DevicesInfo)
	devicesBytes, err := os.ReadFile(templateFilePath)
	if err != nil {
		return fmt.Errorf("could not read template file %v. Err: %v", templateFilePath, err)
	}

	if err := json.Unmarshal(devicesBytes, &devices); err != nil {
		return fmt.Errorf("failed parsing file %v. Err: %v", templateFilePath, err)
	}

	testDirs, err := helpers.NewTestDirs("gpu.intel.com")
	if err != nil {
		fmt.Printf("error creating temp dirs: %v\n", err)
		return err
	}

	err = fakesysfs.FakeSysFsGpuContents(testDirs.SysfsRoot, testDirs.DevfsRoot, devices, true)
	if err != nil {
		fmt.Printf("could not setup fake filesystem in %v: %v\n", testDirs.TestRoot, err)
		if err := os.RemoveAll(testDirs.TestRoot); err != nil {
			fmt.Printf("could not cleanup temp directory %v: %v\n", testDirs.TestRoot, err)
		}
		return err
	}

	fmt.Printf("fake file system: %v\n", testDirs.TestRoot)
	fmt.Printf("fake sysfs: %v\n", testDirs.SysfsRoot)
	fmt.Printf("fake devfs: %v\n", testDirs.DevfsRoot)
	fmt.Printf("fake CDI: %v\n", testDirs.CdiRoot)
	return nil
}

func handleGaudiDevices(templateFilePath string) error {
	devices := make(gaudiDevice.DevicesInfo)
	devicesBytes, err := os.ReadFile(templateFilePath)
	if err != nil {
		return fmt.Errorf("could not read template file %v. Err: %v", templateFilePath, err)
	}

	if err := json.Unmarshal(devicesBytes, &devices); err != nil {
		return fmt.Errorf("failed parsing file %v. Err: %v", templateFilePath, err)
	}

	testDirs, err := helpers.NewTestDirs("gaudi.intel.com")
	if err != nil {
		fmt.Printf("error creating temp dirs: %v\n", err)
		return err
	}

	err = fakesysfs.FakeSysFsGaudiContents(testDirs.SysfsRoot, testDirs.DevfsRoot, devices, true)
	if err != nil {
		fmt.Printf("could not setup fake filesystem in %v: %v\n", testDirs.TestRoot, err)
		if err := os.RemoveAll(testDirs.TestRoot); err != nil {
			fmt.Printf("could not cleanup temp directory %v: %v\n", testDirs.TestRoot, err)
		}
		return err
	}

	fmt.Printf("fake file system: %v\n", testDirs.TestRoot)
	fmt.Printf("fake sysfs: %v\n", testDirs.SysfsRoot)
	fmt.Printf("fake devfs: %v\n", testDirs.DevfsRoot)
	fmt.Printf("fake CDI: %v\n", testDirs.CdiRoot)
	return nil
}

func newTemplate(deviceType string) error {
	var templateText []byte
	templateFilePath, err := os.CreateTemp("/tmp/", fmt.Sprintf("%s-template-*.json", deviceType))
	if err != nil {
		fmt.Printf("Could not create temp file for template: %v", err)
	}

	switch deviceType {
	case "gpu":
		templateData := gpuDevice.DevicesInfo{
			"card0": {
				UID:        "0000-03-00-0-0x56c0",
				PCIAddress: "0000:03:00.0",
				Model:      "0x56c0",
				CardIdx:    0,
				RenderdIdx: 128,
				MemoryMiB:  1024,
				Millicores: 1000,
				DeviceType: "gpu",
				MaxVFs:     8,
				VFProfile:  "",
			},
			"card1": {
				UID:        "0000-03-00-1-0x56c0",
				PCIAddress: "0000:03:00.1",
				Model:      "0x56c0",
				CardIdx:    1,
				RenderdIdx: 129,
				MemoryMiB:  512,
				Millicores: 1000,
				DeviceType: "vf",
				MaxVFs:     0,
				ParentUID:  "0000-03-00-0-0x56c0",
				VFProfile:  "",
				VFIndex:    0,
			},
		}
		templateText, err = json.MarshalIndent(templateData, "", "  ")
		if err != nil {
			return fmt.Errorf("GPU template JSON encoding failed. Err: %v", err)
		}
	case "gaudi":
		templateData := gaudiDevice.DevicesInfo{
			"accel0": {
				UID:        "0000-a0-00-0-0x1020",
				PCIAddress: "0000:a0:00.0",
				Model:      "0x1020",
				DeviceIdx:  0,
				ModuleIdx:  0,
			},
			"accel1": {
				UID:        "0000-b0-00-0-0x1020",
				PCIAddress: "0000:b0:00.0",
				Model:      "0x1020",
				DeviceIdx:  1,
				ModuleIdx:  1,
			},
		}
		templateText, err = json.MarshalIndent(templateData, "", "  ")
		if err != nil {
			return fmt.Errorf("gaudi template JSON encoding failed. Err: %v", err)
		}
	}

	err = os.WriteFile(templateFilePath.Name(), templateText, 0660)
	if err != nil {
		fmt.Printf("Could not write new template file %v: %v", templateFilePath, err)
	}
	fmt.Printf("new template: %v\n", templateFilePath.Name())
	return nil
}
