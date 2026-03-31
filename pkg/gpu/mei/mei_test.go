/*
 * Copyright (c) 2026, Intel Corporation.  All Rights Reserved.
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

package mei

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/fakesysfs"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/helpers"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/plugintesthelpers"
)

func TestDiscoverMeiDeviceForGPU(t *testing.T) {
	tests := []struct {
		name             string
		devices          device.DevicesInfo
		postSetup        func(t *testing.T, sysfsRoot string)
		expectedMEINames map[string]string
	}{
		{
			name: "finds mei from class symlink",
			devices: device.DevicesInfo{
				"0000-00-02-0-0x56c0": {
					UID:        "0000-00-02-0-0x56c0",
					Model:      "0x56c0",
					PCIAddress: "0000:00:02.0",
					PCIRoot:    "pci0000:00",
					CardIdx:    0,
					RenderdIdx: 128,
					MemoryMiB:  8192,
					DeviceType: device.GpuDeviceType,
					Driver:     device.SysfsI915DriverName,
					MEIName:    "mei1",
				},
				"0000-00-04-0-0x56c0": {
					UID:        "0000-00-04-0-0x56c0",
					Model:      "0x56c0",
					PCIAddress: "0000:00:04.0",
					PCIRoot:    "pci0000:00",
					CardIdx:    1,
					RenderdIdx: 129,
					MemoryMiB:  8192,
					DeviceType: device.GpuDeviceType,
					Driver:     device.SysfsXeDriverName,
					MEIName:    "mei2",
				},
			},
			expectedMEINames: map[string]string{
				"0000-00-02-0-0x56c0": "mei1",
				"0000-00-04-0-0x56c0": "mei2",
			},
		},
		{
			name: "falls back to glob when class symlink is absent",
			devices: device.DevicesInfo{
				"0000-00-02-0-0x56c0": {
					UID:        "0000-00-02-0-0x56c0",
					Model:      "0x56c0",
					PCIAddress: "0000:00:02.0",
					PCIRoot:    "pci0000:00",
					CardIdx:    0,
					RenderdIdx: 128,
					MemoryMiB:  8192,
					DeviceType: device.GpuDeviceType,
					Driver:     device.SysfsI915DriverName,
					MEIName:    "mei9",
				},
			},
			postSetup: func(t *testing.T, sysfsRoot string) {
				if err := os.Remove(filepath.Join(sysfsRoot, device.SysfsMEIpath, "mei9")); err != nil {
					t.Fatalf("removing class symlink to force fallback: %v", err)
				}
			},
			expectedMEINames: map[string]string{
				"0000-00-02-0-0x56c0": "mei9",
			},
		},
		{
			name: "returns empty when mei is not present",
			devices: device.DevicesInfo{
				"0000-00-02-0-0x56c0": {
					UID:        "0000-00-02-0-0x56c0",
					Model:      "0x56c0",
					PCIAddress: "0000:00:02.0",
					PCIRoot:    "pci0000:00",
					CardIdx:    0,
					RenderdIdx: 128,
					MemoryMiB:  8192,
					DeviceType: device.GpuDeviceType,
					Driver:     device.SysfsI915DriverName,
				},
			},
			expectedMEINames: map[string]string{
				"0000-00-02-0-0x56c0": "",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testDirs, err := plugintesthelpers.NewTestDirs(device.DriverName)
			if err != nil {
				t.Fatalf("could not create fake system dirs: %v", err)
			}
			defer plugintesthelpers.CleanupTest(t, tc.name, testDirs.TestRoot)

			sysfsRoot := testDirs.SysfsRoot
			devfsRoot := testDirs.DevfsRoot
			t.Setenv(helpers.SysfsEnvVarName, sysfsRoot)

			if err := fakesysfs.FakeSysFsGpuContents(sysfsRoot, devfsRoot, tc.devices, false); err != nil {
				t.Fatalf("creating fake sysfs: %v", err)
			}

			if tc.postSetup != nil {
				tc.postSetup(t, sysfsRoot)
			}

			for uid, gpu := range tc.devices {
				sysfsDeviceDir := filepath.Join(sysfsRoot, "bus", "pci", "drivers", gpu.Driver, gpu.PCIAddress)
				actualMEIName := DiscoverMEIDeviceForGPU("", sysfsDeviceDir)
				if actualMEIName != tc.expectedMEINames[uid] {
					t.Errorf("device %q: expected %q, got %q", uid, tc.expectedMEINames[uid], actualMEIName)
				}
			}
		})
	}
}
