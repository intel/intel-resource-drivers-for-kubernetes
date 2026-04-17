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
	"reflect"
	"strings"
	"testing"

	resourcev1 "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
)

func TestDeviceInfoDeepCopy(t *testing.T) {
	di := device.DeviceInfo{
		UID:        "f",
		Model:      "ff",
		CardIdx:    2,
		RenderdIdx: 3,
		MemoryMiB:  4,
		Millicores: 5,
		DeviceType: "fff",
		MaxVFs:     6,
		ParentUID:  "ffff",
		VFProfile:  "fffff",
		VFIndex:    7,
		MEIName:    "mei0",
	}

	dc := di.DeepCopy()

	if !reflect.DeepEqual(&di, dc) {
		t.Fatalf("device infos %v and %v do not match", di, dc)
	}
}

func errorCheck(t *testing.T, name, substr string, err error) {
	switch {
	case err == nil:
		if substr != "" {
			t.Errorf("%v: unexpected success, expected: %s", name, substr)
		}
	case substr == "":
		t.Errorf("%v: unexpected failure: %v", name, err)
	case !strings.Contains(err.Error(), substr):
		t.Errorf("%v: unexpected error: %v, expected error: %v", name, err, substr)
	}
}

func TestGetResourcesTaintsOnlyUnpreparedNonDRMBoundDevices(t *testing.T) {
	state := &nodeState{
		Allocatable: map[string]*device.DeviceInfo{
			"gpu-unprepared": {
				UID:           "gpu-unprepared",
				PCIAddress:    "0000:00:01.0",
				Model:         "0x56c0",
				ModelName:     "Flex 170",
				FamilyName:    "Data Center Flex",
				MemoryMiB:     16384,
				Driver:        "xe",
				CurrentDriver: "vfio-pci",
				Health:        device.HealthHealthy,
			},
			"gpu-prepared": {
				UID:           "gpu-prepared",
				PCIAddress:    "0000:00:02.0",
				Model:         "0x56c1",
				ModelName:     "Flex 140",
				FamilyName:    "Data Center Flex",
				MemoryMiB:     16384,
				Driver:        "xe",
				CurrentDriver: "vfio-pci",
				Health:        device.HealthHealthy,
			},
		},
		Prepared: ClaimPreparations{
			"claim-1": {
				PreparedDevices: []PreparedDevice{
					{
						KubeletpluginDevice: kubeletplugin.Device{
							DeviceName: "gpu-prepared",
						},
						AdminAccess: false,
					},
				},
			},
		},
		NodeName: "test-node",
	}

	resources := state.GetResources()
	devices := resources.Pools["test-node"].Slices[0].Devices
	if len(devices) != 2 {
		t.Fatalf("expected 2 devices, got %d", len(devices))
	}

	deviceByName := map[string]resourcev1.Device{}
	for _, dev := range devices {
		deviceByName[dev.Name] = dev
	}

	if len(deviceByName["gpu-unprepared"].Taints) != 1 {
		t.Fatalf("expected gpu-unprepared to have 1 taint, got %d", len(deviceByName["gpu-unprepared"].Taints))
	}

	if got := deviceByName["gpu-unprepared"].Taints[0].Key; got != "NotDRMBound-vfio-pci" {
		t.Fatalf("unexpected taint key for gpu-unprepared: %s", got)
	}

	if len(deviceByName["gpu-prepared"].Taints) != 0 {
		t.Fatalf("expected gpu-prepared to have no taints, got %d", len(deviceByName["gpu-prepared"].Taints))
	}
}

func TestIsDevicePrepared(t *testing.T) {
	state := &nodeState{
		Allocatable: map[string]*device.DeviceInfo{
			"gpu-prepared": {
				UID:        "gpu-prepared",
				PCIAddress: "0000:00:02.0",
			},
			"gpu-free": {
				UID:        "gpu-free",
				PCIAddress: "0000:00:03.0",
			},
			"gpu-prepared-with-admin-access": {
				UID:        "gpu-prepared-with-admin-access",
				PCIAddress: "0000:00:02.0",
			},
		},
		Prepared: ClaimPreparations{
			"claim-1": {
				PreparedDevices: []PreparedDevice{
					{
						KubeletpluginDevice: kubeletplugin.Device{
							DeviceName: "gpu-prepared",
						},
					},
				},
			},
			"claim-2": {
				PreparedDevices: []PreparedDevice{
					{
						KubeletpluginDevice: kubeletplugin.Device{
							DeviceName: "gpu-prepared-with-admin-access",
						},
						AdminAccess: true,
					},
				},
			},
		},
	}

	testcases := []struct {
		name        string
		uid         string
		expected    bool
		expectError bool
	}{
		{
			name:     "prepared device",
			uid:      "gpu-prepared",
			expected: true,
		},
		{
			name:     "unprepared device",
			uid:      "gpu-free",
			expected: false,
		},
		{
			name:     "unknown device",
			uid:      "gpu-unknown",
			expected: false,
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			got := state.IsDevicePrepared(testcase.uid)

			if got != testcase.expected {
				t.Fatalf("expected IsDevicePrepared()=%v, got %v", testcase.expected, got)
			}
		})
	}
}

func TestIsDeviceUsedExclusivelyAlready(t *testing.T) {
	state := &nodeState{
		Allocatable: map[string]*device.DeviceInfo{
			"gpu-prepared": {
				UID:        "gpu-prepared",
				PCIAddress: "0000:00:02.0",
			},
			"gpu-being-prepared": {
				UID:        "gpu-being-prepared",
				PCIAddress: "0000:00:04.0",
			},
			"gpu-free": {
				UID:        "gpu-free",
				PCIAddress: "0000:00:03.0",
			},
			"gpu-prepared-with-admin-access": {
				UID:        "gpu-prepared-with-admin-access",
				PCIAddress: "0000:00:01.0",
			},
		},
		Prepared: ClaimPreparations{
			"claim-1": {
				PreparedDevices: []PreparedDevice{
					{
						KubeletpluginDevice: kubeletplugin.Device{
							DeviceName: "gpu-prepared",
							PoolName:   "pool0",
						},
					},
				},
			},
			"claim-2": {
				PreparedDevices: []PreparedDevice{
					{
						KubeletpluginDevice: kubeletplugin.Device{
							DeviceName: "gpu-prepared-with-admin-access",
							PoolName:   "pool0",
						},
						AdminAccess: true,
					},
				},
			},
			"claim-new": {
				PreparedDevices: []PreparedDevice{
					{
						KubeletpluginDevice: kubeletplugin.Device{
							DeviceName: "gpu-being-prepared",
							PoolName:   "pool0",
						},
					},
				},
			},
		},
	}

	testcases := []struct {
		name        string
		uid         string
		expected    bool
		expectError bool
		claimUid    types.UID
	}{
		{
			name:     "prepared device",
			uid:      "gpu-prepared",
			expected: true,
			claimUid: "claim-x",
		},
		{
			name:     "unprepared device",
			uid:      "gpu-free",
			claimUid: "claim-x",
			expected: false,
		},
		{
			name:     "unknown device",
			uid:      "gpu-unknown",
			claimUid: "claim-x",
			expected: false,
		},
		{
			name:     "prepared device with admin access",
			uid:      "gpu-prepared-with-admin-access",
			claimUid: "claim-x",
			expected: false, // AdminAccess devices should not be considered prepared for exclusivity checks.
		},
		{
			name:     "already-prepared-claim repeats",
			uid:      "gpu-being-prepared",
			claimUid: "claim-new",
			expected: false, // AdminAccess devices should not be considered prepared for exclusivity checks.
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			got := state.isDeviceUsedExclusivelyAlready(testcase.uid, "pool0", testcase.claimUid)

			if got != testcase.expected {
				t.Fatalf("expected IsDeviceUsedExclusivelyAlready()=%v, got %v", testcase.expected, got)
			}
		})
	}
}
