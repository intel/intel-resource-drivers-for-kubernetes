/* Copyright (C) 2025 Intel Corporation
 * SPDX-License-Identifier: Apache-2.0
 */

package discovery_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"reflect"
	"testing"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/fakesysfs"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/discovery"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/helpers"
	testhelpers "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/plugintesthelpers"
)

func createFakeSysfsWithSingleGpu(sysfsRoot, devfsRoot string) error {
	if err := fakesysfs.FakeSysFsGpuContents(
		sysfsRoot,
		devfsRoot,
		device.DevicesInfo{
			"0000-0f-00-0-0x56c0": {
				Model:         "0x56c0",
				ModelName:     "Flex 170",
				FamilyName:    "Data Center Flex",
				PCIAddress:    "0000:0f:00.0",
				MemoryMiB:     8192,
				DeviceType:    "gpu",
				CardName:      "card0",
				MEIName:       "mei0",
				RenderDName:   "renderD128",
				Millicores:    1000,
				UID:           "0000-0f-00-0-0x56c0",
				MaxVFs:        16,
				Driver:        device.SysfsI915DriverName,
				CurrentDriver: device.SysfsI915DriverName,
			},
		},
		false,
	); err != nil {
		return fmt.Errorf("could not set up fake sysfs gpu contents: %v", err)
	}
	return nil
}

func createFakeSysfsWithSingleVFIOGpu(sysfsRoot, devfsRoot string) error {
	if err := fakesysfs.FakeSysFsGpuContents(
		sysfsRoot,
		devfsRoot,
		device.DevicesInfo{
			"0000-0f-00-0-0xe211": {
				Model:         "0xe211",
				PCIAddress:    "0000:0f:00.0",
				MemoryMiB:     8192,
				DeviceType:    "gpu",
				VFIODevice:    "vfio0",
				IOMMUGroup:    "15",
				Millicores:    1000,
				UID:           "0000-0f-00-0-0xe211",
				MaxVFs:        16,
				Driver:        device.SysfsXeVFIODriverName,
				CurrentDriver: device.SysfsXeVFIODriverName,
			},
		},
		false,
	); err != nil {
		return fmt.Errorf("could not set up fake sysfs gpu contents: %v", err)
	}
	return nil
}

//nolint:cyclop
func TestDiscoverDevices(t *testing.T) {
	tests := []struct {
		name        string
		setupFunc   func(sysfsDir string, namingStyle string) error
		namingStyle string
		expected    map[string]*device.DeviceInfo
	}{
		{
			name:      "no device",
			setupFunc: nil,
			expected:  map[string]*device.DeviceInfo{},
		},
		{
			name:      "single device",
			setupFunc: createFakeSysfsWithSingleGpu,
			expected: map[string]*device.DeviceInfo{
				"0000-0f-00-0-0x56c0": {
					Model:         "0x56c0",
					ModelName:     "Flex 170",
					FamilyName:    "Data Center Flex",
					PCIAddress:    "0000:0f:00.0",
					PCIRoot:       "pci0000:00",
					MemoryMiB:     0,
					DeviceType:    "gpu",
					CardName:      "card0",
					MEIName:       "mei0",
					RenderDName:   "renderD128",
					Millicores:    1000,
					UID:           "0000-0f-00-0-0x56c0",
					MaxVFs:        16,
					Driver:        device.SysfsI915DriverName,
					CurrentDriver: device.SysfsI915DriverName,
					Health:        device.HealthHealthy,
					HealthStatus:  map[string]string{},
				},
			},
		},
		{
			name: "with 1 vf",
			setupFunc: func(sysfsRoot, devfsRoot string) error {
				if err := fakesysfs.FakeSysFsGpuContents(
					sysfsRoot,
					devfsRoot,
					device.DevicesInfo{
						"0000-0f-00-0-0x56c0": {
							Model:         "0x56c0",
							ModelName:     "Flex 170",
							FamilyName:    "Data Center Flex",
							PCIAddress:    "0000:0f:00.0",
							MemoryMiB:     8192,
							DeviceType:    "gpu",
							CardName:      "card0",
							MEIName:       "mei0",
							RenderDName:   "renderD128",
							Millicores:    1000,
							UID:           "0000-0f-00-0-0x56c0",
							MaxVFs:        16,
							Driver:        device.SysfsI915DriverName,
							CurrentDriver: device.SysfsI915DriverName,
						},
						"0000-0f-00-1-0x56c0": {
							Model:         "0x56c0",
							ModelName:     "Flex 170",
							FamilyName:    "Data Center Flex",
							PCIAddress:    "0000:0f:00.1",
							MemoryMiB:     8192,
							DeviceType:    "vf",
							ParentUID:     "0000-0f-00-0-0x56c0",
							CardName:      "card1",
							RenderDName:   "renderD129",
							Millicores:    1000,
							UID:           "0000-0f-00-1-0x56c0",
							MaxVFs:        0,
							Driver:        device.SysfsI915DriverName,
							CurrentDriver: device.SysfsI915DriverName,
						},
					},
					false,
				); err != nil {
					return fmt.Errorf("could not set up fake sysfs gpu contents: %v", err)
				}
				return nil
			},
			expected: map[string]*device.DeviceInfo{
				"0000-0f-00-0-0x56c0": {
					Model:         "0x56c0",
					ModelName:     "Flex 170",
					FamilyName:    "Data Center Flex",
					PCIAddress:    "0000:0f:00.0",
					PCIRoot:       "pci0000:00",
					MemoryMiB:     0,
					DeviceType:    "gpu",
					CardName:      "card0",
					MEIName:       "mei0",
					RenderDName:   "renderD128",
					Millicores:    1000,
					UID:           "0000-0f-00-0-0x56c0",
					MaxVFs:        16,
					Driver:        device.SysfsI915DriverName,
					CurrentDriver: device.SysfsI915DriverName,
					Health:        device.HealthHealthy,
					HealthStatus:  map[string]string{},
				},
				"0000-0f-00-1-0x56c0": {
					Model:         "0x56c0",
					ModelName:     "Flex 170",
					FamilyName:    "Data Center Flex",
					PCIAddress:    "0000:0f:00.1",
					PCIRoot:       "pci0000:00",
					MemoryMiB:     0,
					DeviceType:    "vf",
					ParentUID:     "0000-0f-00-0-0x56c0",
					CardName:      "card1",
					RenderDName:   "renderD129",
					Millicores:    1000,
					UID:           "0000-0f-00-1-0x56c0",
					MaxVFs:        0,
					Driver:        device.SysfsI915DriverName,
					CurrentDriver: device.SysfsI915DriverName,
					Health:        device.HealthHealthy,
					HealthStatus:  map[string]string{},
				},
			},
		},
		{
			name: "i915 device file read error",
			setupFunc: func(sysfsRoot, devfsRoot string) error {
				if err := createFakeSysfsWithSingleGpu(sysfsRoot, devfsRoot); err != nil {
					return err
				}
				return os.Remove(path.Join(sysfsRoot, device.SysfsPCIDriversPath, device.SysfsI915DriverName, "0000:0f:00.0", "device"))
			},
			expected: map[string]*device.DeviceInfo{},
		},
		{
			name: "totalvfs read error",
			setupFunc: func(sysfsRoot, devfsRoot string) error {
				if err := createFakeSysfsWithSingleGpu(sysfsRoot, devfsRoot); err != nil {
					return err
				}
				return os.Remove(path.Join(sysfsRoot, device.SysfsPCIDriversPath, device.SysfsI915DriverName, "0000:0f:00.0", "sriov_totalvfs"))
			},
			expected: map[string]*device.DeviceInfo{
				"0000-0f-00-0-0x56c0": {
					Model:         "0x56c0",
					ModelName:     "Flex 170",
					FamilyName:    "Data Center Flex",
					PCIAddress:    "0000:0f:00.0",
					PCIRoot:       "pci0000:00",
					MemoryMiB:     0,
					DeviceType:    "gpu",
					CardName:      "card0",
					MEIName:       "mei0",
					RenderDName:   "renderD128",
					Millicores:    1000,
					UID:           "0000-0f-00-0-0x56c0",
					MaxVFs:        0,
					Driver:        device.SysfsI915DriverName,
					CurrentDriver: device.SysfsI915DriverName,
					Health:        device.HealthHealthy,
					HealthStatus:  map[string]string{},
				},
			},
		},
		{
			name: "driversAutoprobeFile read error",
			setupFunc: func(sysfsRoot, devfsRoot string) error {
				if err := createFakeSysfsWithSingleGpu(sysfsRoot, devfsRoot); err != nil {
					return err
				}
				return os.RemoveAll(path.Join(sysfsRoot, device.SysfsPCIDriversPath, device.SysfsI915DriverName, "0000:0f:00.0", "sriov_drivers_autoprobe"))
			},
			expected: map[string]*device.DeviceInfo{
				"0000-0f-00-0-0x56c0": {
					Model:         "0x56c0",
					ModelName:     "Flex 170",
					FamilyName:    "Data Center Flex",
					PCIAddress:    "0000:0f:00.0",
					PCIRoot:       "pci0000:00",
					MemoryMiB:     0,
					DeviceType:    "gpu",
					CardName:      "card0",
					MEIName:       "mei0",
					RenderDName:   "renderD128",
					Millicores:    1000,
					UID:           "0000-0f-00-0-0x56c0",
					MaxVFs:        0,
					Driver:        device.SysfsI915DriverName,
					CurrentDriver: device.SysfsI915DriverName,
					Health:        device.HealthHealthy,
					HealthStatus:  map[string]string{},
				},
			},
		},
		{
			name: "drm dir read error",
			setupFunc: func(sysfsRoot, devfsRoot string) error {
				if err := createFakeSysfsWithSingleGpu(sysfsRoot, devfsRoot); err != nil {
					return err
				}
				return os.RemoveAll(path.Join(sysfsRoot, device.SysfsPCIDriversPath, device.SysfsI915DriverName, "0000:0f:00.0", "drm"))
			},
			expected: map[string]*device.DeviceInfo{},
		},
		{
			name: "drm dir read error",
			setupFunc: func(sysfsRoot, devfsRoot string) error {
				if err := createFakeSysfsWithSingleGpu(sysfsRoot, devfsRoot); err != nil {
					return err
				}
				return os.RemoveAll(path.Join(sysfsRoot, device.SysfsPCIDriversPath, device.SysfsI915DriverName, "0000:0f:00.0", "drm"))
			},
			expected: map[string]*device.DeviceInfo{},
		},
		{
			name:        "classic naming style",
			setupFunc:   createFakeSysfsWithSingleGpu,
			namingStyle: "classic",
			expected: map[string]*device.DeviceInfo{
				"card0": {
					Model:         "0x56c0",
					ModelName:     "Flex 170",
					FamilyName:    "Data Center Flex",
					PCIAddress:    "0000:0f:00.0",
					PCIRoot:       "pci0000:00",
					MemoryMiB:     0,
					DeviceType:    "gpu",
					CardName:      "card0",
					MEIName:       "mei0",
					RenderDName:   "renderD128",
					Millicores:    1000,
					UID:           "0000-0f-00-0-0x56c0",
					MaxVFs:        16,
					Driver:        device.SysfsI915DriverName,
					CurrentDriver: device.SysfsI915DriverName,
					Health:        device.HealthHealthy,
					HealthStatus:  map[string]string{},
				},
			},
		},
		{
			name:      "single xe-vfio-pci device",
			setupFunc: createFakeSysfsWithSingleVFIOGpu,
			expected: map[string]*device.DeviceInfo{
				"0000-0f-00-0-0xe211": {
					Model:         "0xe211",
					ModelName:     "B60",
					FamilyName:    "Arc Pro B-Series",
					PCIAddress:    "0000:0f:00.0",
					PCIRoot:       "pci0000:00",
					MemoryMiB:     0,
					DeviceType:    "gpu",
					VFIODevice:    "vfio0",
					IOMMUGroup:    "15",
					Millicores:    1000,
					UID:           "0000-0f-00-0-0xe211",
					MaxVFs:        16,
					Driver:        device.SysfsXeDriverName,
					CurrentDriver: device.SysfsXeVFIODriverName,
					Health:        device.HealthHealthy,
					HealthStatus:  map[string]string{},
				},
			},
		},
	}

	// Unset will be needed after test concludes, env var gets overwritten in test loop.
	defer os.Unsetenv(helpers.SysfsEnvVarName)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testDirs, err := testhelpers.NewTestDirs(device.DriverName)
			defer testhelpers.CleanupTest(t, tt.name, testDirs.TestRoot)
			if err != nil {
				t.Fatalf("could not create fake system dirs: %v", err)
			}
			os.Setenv(helpers.SysfsEnvVarName, testDirs.SysfsRoot)

			// Setup fake sysfs gpu contents
			if tt.setupFunc != nil {
				if err := tt.setupFunc(testDirs.SysfsRoot, testDirs.DevfsRoot); err != nil {
					t.Fatalf("could not set up test: %v", err)
				}
			}

			// Discover devices.
			devices := discovery.DiscoverDevices(testDirs.SysfsRoot, tt.namingStyle, false)

			// Validate results
			if len(devices) != len(tt.expected) {
				t.Errorf("expected %d devices, got %d", len(tt.expected), len(devices))
			}
			for name, expectedDevice := range tt.expected {
				actualDevice, exists := devices[name]
				if !exists {
					t.Errorf("expected device %v not found", name)
					continue
				}
				if !reflect.DeepEqual(actualDevice, expectedDevice) {
					expectedJSON, _ := json.MarshalIndent(expectedDevice, "", "\t")
					actualJSON, _ := json.MarshalIndent(actualDevice, "", "\t")
					t.Errorf("device %s mismatch:\nexpected: %s\nactual: %s", name, expectedJSON, actualJSON)
				}
			}
		})
	}
}
