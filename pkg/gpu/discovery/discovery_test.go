/* Copyright (C) 2025 Intel Corporation
 * SPDX-License-Identifier: Apache-2.0
 */

package discovery_test

import (
	"fmt"
	"os"
	"path"
	"testing"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/fakesysfs"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/discovery"
	testhelpers "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/plugintesthelpers"
)

func createFakeSysfsWithSingleGpu(sysfsRoot, devfsRoot string) error {
	if err := fakesysfs.FakeSysFsGpuContents(
		sysfsRoot,
		devfsRoot,
		device.DevicesInfo{
			"0000-0f-00-0-0x56c0": {
				Model:      "0x56c0",
				ModelName:  "Flex 170",
				FamilyName: "Data Center Flex",
				PCIAddress: "0000:0f:00.0",
				MemoryMiB:  8192,
				DeviceType: "gpu",
				CardIdx:    0,
				RenderdIdx: 128,
				Millicores: 1000,
				UID:        "0000-0f-00-0-0x56c0",
				MaxVFs:     16,
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
					Model:      "0x56c0",
					ModelName:  "Flex 170",
					FamilyName: "Data Center Flex",
					PCIAddress: "0000:0f:00.0",
					MemoryMiB:  8192,
					DeviceType: "gpu",
					CardIdx:    0,
					RenderdIdx: 128,
					Millicores: 1000,
					UID:        "0000-0f-00-0-0x56c0",
					MaxVFs:     16,
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
							Model:      "0x56c0",
							ModelName:  "Flex 170",
							FamilyName: "Data Center Flex",
							PCIAddress: "0000:0f:00.0",
							MemoryMiB:  8192,
							DeviceType: "gpu",
							CardIdx:    0,
							RenderdIdx: 128,
							Millicores: 1000,
							UID:        "0000-0f-00-0-0x56c0",
							MaxVFs:     16,
						},
						"0000-0f-00-1-0x56c0": {
							Model:      "0x56c0",
							ModelName:  "Flex 170",
							FamilyName: "Data Center Flex",
							PCIAddress: "0000:0f:00.1",
							MemoryMiB:  8192,
							DeviceType: "vf",
							ParentUID:  "0000-0f-00-0-0x56c0",
							CardIdx:    1,
							RenderdIdx: 129,
							Millicores: 1000,
							UID:        "0000-0f-00-1-0x56c0",
							MaxVFs:     0,
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
					Model:      "0x56c0",
					ModelName:  "Flex 170",
					FamilyName: "Data Center Flex",
					PCIAddress: "0000:0f:00.0",
					MemoryMiB:  8192,
					DeviceType: "gpu",
					CardIdx:    0,
					RenderdIdx: 128,
					Millicores: 1000,
					UID:        "0000-0f-00-0-0x56c0",
					MaxVFs:     16,
				},
				"0000-0f-00-1-0x56c0": {
					Model:      "0x56c0",
					ModelName:  "Flex 170",
					FamilyName: "Data Center Flex",
					PCIAddress: "0000:0f:00.1",
					MemoryMiB:  8192,
					DeviceType: "vf",
					ParentUID:  "0000-0f-00-0-0x56c0",
					CardIdx:    1,
					RenderdIdx: 129,
					Millicores: 1000,
					UID:        "0000-0f-00-1-0x56c0",
					MaxVFs:     0,
				},
			},
		},
		{
			name: "i915 device file read error",
			setupFunc: func(sysfsRoot, devfsRoot string) error {
				if err := createFakeSysfsWithSingleGpu(sysfsRoot, devfsRoot); err != nil {
					return err
				}
				return os.Remove(path.Join(sysfsRoot, device.SysfsI915path, "0000:0f:00.0", "device"))
			},
			expected: map[string]*device.DeviceInfo{},
		},
		{
			name: "totalvfs read error",
			setupFunc: func(sysfsRoot, devfsRoot string) error {
				if err := createFakeSysfsWithSingleGpu(sysfsRoot, devfsRoot); err != nil {
					return err
				}
				return os.Remove(path.Join(sysfsRoot, device.SysfsI915path, "0000:0f:00.0", "sriov_totalvfs"))
			},
			expected: map[string]*device.DeviceInfo{
				"0000-0f-00-0-0x56c0": {
					Model:      "0x56c0",
					ModelName:  "Flex 170",
					FamilyName: "Data Center Flex",
					PCIAddress: "0000:0f:00.0",
					MemoryMiB:  8192,
					DeviceType: "gpu",
					CardIdx:    0,
					RenderdIdx: 128,
					Millicores: 1000,
					UID:        "0000-0f-00-0-0x56c0",
					MaxVFs:     0,
				},
			},
		},
		{
			name: "driversAutoprobeFile read error",
			setupFunc: func(sysfsRoot, devfsRoot string) error {
				if err := createFakeSysfsWithSingleGpu(sysfsRoot, devfsRoot); err != nil {
					return err
				}
				return os.RemoveAll(path.Join(sysfsRoot, device.SysfsI915path, "0000:0f:00.0", "sriov_drivers_autoprobe"))
			},
			expected: map[string]*device.DeviceInfo{
				"0000-0f-00-0-0x56c0": {
					Model:      "0x56c0",
					ModelName:  "Flex 170",
					FamilyName: "Data Center Flex",
					PCIAddress: "0000:0f:00.0",
					MemoryMiB:  8192,
					DeviceType: "gpu",
					CardIdx:    0,
					RenderdIdx: 128,
					Millicores: 1000,
					UID:        "0000-0f-00-0-0x56c0",
					MaxVFs:     0,
				},
			},
		},
		{
			name: "drm dir read error",
			setupFunc: func(sysfsRoot, devfsRoot string) error {
				if err := createFakeSysfsWithSingleGpu(sysfsRoot, devfsRoot); err != nil {
					return err
				}
				return os.RemoveAll(path.Join(sysfsRoot, device.SysfsI915path, "0000:0f:00.0", "drm"))
			},
			expected: map[string]*device.DeviceInfo{},
		},
		{
			name: "drm dir read error",
			setupFunc: func(sysfsRoot, devfsRoot string) error {
				if err := createFakeSysfsWithSingleGpu(sysfsRoot, devfsRoot); err != nil {
					return err
				}
				return os.RemoveAll(path.Join(sysfsRoot, device.SysfsI915path, "0000:0f:00.0", "drm"))
			},
			expected: map[string]*device.DeviceInfo{},
		},
		{
			name: "lmem_total_bytes read error",
			setupFunc: func(sysfsRoot, devfsRoot string) error {
				if err := createFakeSysfsWithSingleGpu(sysfsRoot, devfsRoot); err != nil {
					return err
				}
				return os.RemoveAll(path.Join(sysfsRoot, device.SysfsDRMpath, "card0", "lmem_total_bytes"))
			},
			expected: map[string]*device.DeviceInfo{
				"0000-0f-00-0-0x56c0": {
					Model:      "0x56c0",
					ModelName:  "Flex 170",
					FamilyName: "Data Center Flex",
					PCIAddress: "0000:0f:00.0",
					MemoryMiB:  0,
					DeviceType: "gpu",
					CardIdx:    0,
					RenderdIdx: 128,
					Millicores: 1000,
					UID:        "0000-0f-00-0-0x56c0",
					MaxVFs:     16,
				},
			},
		},
		{
			name: "invalid lmem_total_bytes value triggers conversion error",
			setupFunc: func(sysfsRoot, devfsRoot string) error {
				if err := createFakeSysfsWithSingleGpu(sysfsRoot, devfsRoot); err != nil {
					return err
				}
				return os.WriteFile(path.Join(sysfsRoot, device.SysfsDRMpath, "card0", "lmem_total_bytes"), []byte("invalid"), 0644)
			},
			expected: map[string]*device.DeviceInfo{
				"0000-0f-00-0-0x56c0": {
					Model:      "0x56c0",
					ModelName:  "Flex 170",
					FamilyName: "Data Center Flex",
					PCIAddress: "0000:0f:00.0",
					MemoryMiB:  0,
					DeviceType: "gpu",
					CardIdx:    0,
					RenderdIdx: 128,
					Millicores: 1000,
					UID:        "0000-0f-00-0-0x56c0",
					MaxVFs:     16,
				},
			},
		},
		{
			name:        "classic naming style",
			setupFunc:   createFakeSysfsWithSingleGpu,
			namingStyle: "classic",
			expected: map[string]*device.DeviceInfo{
				"card0": {
					Model:      "0x56c0",
					ModelName:  "Flex 170",
					FamilyName: "Data Center Flex",
					PCIAddress: "0000:0f:00.0",
					MemoryMiB:  8192,
					DeviceType: "gpu",
					CardIdx:    0,
					RenderdIdx: 128,
					Millicores: 1000,
					UID:        "0000-0f-00-0-0x56c0",
					MaxVFs:     16,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testDirs, err := testhelpers.NewTestDirs(device.DriverName)
			defer testhelpers.CleanupTest(t, tt.name, testDirs.TestRoot)

			if err != nil {
				t.Fatalf("could not create fake system dirs: %v", err)
			}

			// Setup fake sysfs gpu contents
			if tt.setupFunc != nil {
				if err := tt.setupFunc(testDirs.SysfsRoot, testDirs.DevfsRoot); err != nil {
					t.Fatalf("could not set up test: %v", err)
				}
			}

			// Discover devices
			devices := discovery.DiscoverDevices(testDirs.SysfsRoot, tt.namingStyle)

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
				if *actualDevice != *expectedDevice {
					t.Errorf("device %s mismatch: expected %+v, got %+v", name, expectedDevice, actualDevice)
				}
			}
		})
	}
}
