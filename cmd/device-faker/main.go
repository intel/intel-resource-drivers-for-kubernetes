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
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/fakesysfs"
	gaudiDevice "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gaudi/device"
	gpuDevice "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
	helpers "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/plugintesthelpers"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/version"
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
			if len(args) != 1 {
				return fmt.Errorf("too many arguments: only one device at a time supported")
			}

			deviceType := strings.ToLower(args[0])

			targetDir := cmd.Flag("target-dir").Value.String()

			realDevices := cmd.Flag("real-devices").Value.String() == "true"

			cleanup := cmd.Flag("cleanup").Value.String() == "true"

			newTemplate := cmd.Flag("new-template").Value.String() == "true"
			if newTemplate {
				return createNewTemplate(deviceType)
			}

			template := cmd.Flag("template").Value.String()
			if template == "" {
				return fmt.Errorf("template parameter is missing")
			}

			var testDirs helpers.TestDirsType
			var err error
			var driverName string

			switch deviceType {
			case "gpu":
				driverName = "gpu.intel.com"
			case "gaudi":
				driverName = "gaudi.intel.com"
			}

			if targetDir == "" {
				testDirs, err = helpers.NewTestDirs(driverName)
			} else {
				testDirs, err = helpers.NewTestDirsAt(targetDir, driverName)
			}
			if err != nil {
				return fmt.Errorf("error creating temp dirs: %v", err)
			}

			fmt.Println(cmd.Version)

			switch deviceType {
			case "gpu":
				err = handleGPUDevices(template, testDirs, realDevices)
			case "gaudi":
				err = handleGaudiDevices(template, testDirs, realDevices)
			}
			if err != nil {
				fmt.Printf("ERROR: %v", err)
			}

			return printAndWaitForCleanup(testDirs, cmd.Flag("print").Value.String() == "true", cleanup)
		},
	}

	cmd.Version = version.GetVersion() + " (git " + version.GetGitCommit() + "). Built " + version.GetBuildDate()
	cmd.Flags().BoolP("version", "v", false, "Show the version of the binary")
	cmd.Flags().BoolP("new-template", "n", false, "Create new template file for given accelerator")
	cmd.Flags().StringP("template", "t", "", "Template file to populate devices from")
	cmd.Flags().StringP("target-dir", "d", "", "Target directory, default is random /tmp/test-*")
	cmd.Flags().BoolP("real-devices", "r", false, "Create real device files (requires root)")
	cmd.Flags().BoolP("cleanup", "c", false, "Wait for SIGTERM, cleanup before exiting")
	cmd.Flags().BoolP("print", "p", false, "Print resulting file-system tree")
	cmd.SetVersionTemplate("device-faker version: {{.Version}}\n")

	return cmd
}

func handleGPUDevices(templateFilePath string, testDirs helpers.TestDirsType, realDevices bool) error {
	devices := make(gpuDevice.DevicesInfo)
	devicesBytes, err := os.ReadFile(templateFilePath)
	if err != nil {
		return fmt.Errorf("could not read template file %v. Err: %v", templateFilePath, err)
	}

	if err := json.Unmarshal(devicesBytes, &devices); err != nil {
		return fmt.Errorf("failed parsing file %v. Err: %v", templateFilePath, err)
	}

	err = fakesysfs.FakeSysFsGpuContents(testDirs.SysfsRoot, testDirs.DevfsRoot, devices, realDevices)
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

func handleGaudiDevices(templateFilePath string, testDirs helpers.TestDirsType, realDevices bool) error {
	devices := make(gaudiDevice.DevicesInfo)
	devicesBytes, err := os.ReadFile(templateFilePath)
	if err != nil {
		return fmt.Errorf("could not read template file %v. Err: %v", templateFilePath, err)
	}

	if err := json.Unmarshal(devicesBytes, &devices); err != nil {
		return fmt.Errorf("failed parsing file %v. Err: %v", templateFilePath, err)
	}

	err = fakesysfs.FakeSysFsGaudiContents(testDirs.TestRoot, testDirs.SysfsRoot, testDirs.DevfsRoot, devices, realDevices)
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

func createNewTemplate(deviceType string) error {
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
				Driver:     "i915",
				MaxVFs:     8,
				VFProfile:  "",
				PCIRoot:    "pci0000:01",
			},
			"card1": {
				UID:        "0000-04-00-1-0xe20b",
				PCIAddress: "0000:04:00.1",
				Model:      "0xe20b",
				CardIdx:    1,
				RenderdIdx: 129,
				MemoryMiB:  2048,
				Millicores: 1000,
				DeviceType: "gpu",
				Driver:     "xe",
				MaxVFs:     0,
				ParentUID:  "0000-04-00-0-0xe20b",
				VFProfile:  "",
				PCIRoot:    "pci0000:02",
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
				PCIRoot:    "pci0000:01",
			},
			"accel1": {
				UID:        "0000-b0-00-0-0x1020",
				PCIAddress: "0000:b0:00.0",
				Model:      "0x1020",
				DeviceIdx:  1,
				ModuleIdx:  1,
				PCIRoot:    "pci0000:02",
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

func printAndWaitForCleanup(testDirs helpers.TestDirsType, doPrint, doCleanup bool) error {
	if doPrint {
		fmt.Println("Resulting file-system tree:")
		if err := filepath.Walk(testDirs.TestRoot, func(name string, info os.FileInfo, err error) error {
			fmt.Println(strings.Replace(name, testDirs.TestRoot, "", 1))
			return nil
		}); err != nil {
			// Cleanup is still needed, do not quit on error here.
			fmt.Printf("error walking file-system tree: %v\n", err)
		}
	}

	if !doCleanup {
		return nil
	}

	fmt.Println("Waiting for SIGTERM to cleanup...")
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	sig := <-sigc

	fmt.Printf("Received %v, cleaning up...\n", sig)

	anyErr := false
	// Do not cleanup the top level directory, it might be a mount point.
	for _, dirname := range []string{testDirs.SysfsRoot, testDirs.DevfsRoot, testDirs.CdiRoot} {
		if err := os.RemoveAll(dirname); err != nil {
			fmt.Printf("Error cleaning up fake sysfs %v: %v\n", testDirs.TestRoot, err)
			anyErr = true
		}
	}

	if anyErr {
		fmt.Println("Cleanup completed with errors.")
		return fmt.Errorf("cleanup completed with errors")
	}

	fmt.Println("Cleanup completed successfully.")
	return nil
}
