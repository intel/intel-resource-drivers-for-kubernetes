/* Copyright (C) 2025 Intel Corporation
 * SPDX-License-Identifier: Apache-2.0
 */

package cdihelpers

import (
	"testing"

	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"
	specs "tags.cncf.io/container-device-interface/specs-go"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
	testhelpers "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/plugintesthelpers"
)

func TestSyncDetectedDevicesWithRegistry(t *testing.T) {

	tests := []struct {
		name            string
		existingSpecs   []*cdiapi.Spec
		detectedDevices device.DevicesInfo
		doCleanup       bool
		expectedError   bool
	}{
		{
			name:          "No existing specs, add new devices",
			existingSpecs: nil,
			detectedDevices: device.DevicesInfo{
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
			doCleanup:     false,
			expectedError: false,
		},
		{
			name: "Existing specs, no changes needed",
			existingSpecs: []*cdiapi.Spec{
				{
					Spec: &specs.Spec{
						Kind:    device.CDIKind,
						Version: "0.6.0",
						Devices: []specs.Device{
							{
								Name: "gpu1",
								ContainerEdits: specs.ContainerEdits{
									DeviceNodes: []*specs.DeviceNode{
										{Path: "/dev/dri/card0", HostPath: "/dev/dri/card0", Type: "c"},
										{Path: "/dev/dri/renderD128", HostPath: "/dev/dri/renderD128", Type: "c"},
									},
								},
							},
						},
					},
				},
			},
			detectedDevices: device.DevicesInfo{
				"gpu1": {UID: "gpu1", CardIdx: 0, RenderdIdx: 128},
			},
			doCleanup:     false,
			expectedError: false,
		},
		{
			name: "Existing specs, update device indices",
			existingSpecs: []*cdiapi.Spec{
				{
					Spec: &specs.Spec{
						Kind:    device.CDIKind,
						Version: "0.6.0",
						Devices: []specs.Device{
							{
								Name: "gpu1",
								ContainerEdits: specs.ContainerEdits{
									DeviceNodes: []*specs.DeviceNode{
										{Path: "/dev/dri/card1", HostPath: "/dev/dri/card1", Type: "c"},
										{Path: "/dev/dri/renderD129", HostPath: "/dev/dri/renderD129", Type: "c"},
									},
								},
							},
						},
					},
				},
			},
			detectedDevices: device.DevicesInfo{
				"gpu1": {UID: "gpu1", CardIdx: 0, RenderdIdx: 128},
			},
			doCleanup:     false,
			expectedError: false,
		},
		{
			name: "Existing specs, remove absent devices with cleanup",
			existingSpecs: []*cdiapi.Spec{
				{
					Spec: &specs.Spec{
						Kind:    device.CDIKind,
						Version: "0.6.0",
						Devices: []specs.Device{
							{
								Name: "gpu1",
								ContainerEdits: specs.ContainerEdits{
									DeviceNodes: []*specs.DeviceNode{
										{Path: "/dev/dri/card0", HostPath: "/dev/dri/card0", Type: "c"},
										{Path: "/dev/dri/renderD128", HostPath: "/dev/dri/renderD128", Type: "c"},
									},
								},
							},
							{
								Name: "gpu2",
								ContainerEdits: specs.ContainerEdits{
									DeviceNodes: []*specs.DeviceNode{
										{Path: "/dev/dri/card1", HostPath: "/dev/dri/card1", Type: "c"},
										{Path: "/dev/dri/renderD129", HostPath: "/dev/dri/renderD129", Type: "c"},
									},
								},
							},
						},
					},
				},
			},
			detectedDevices: device.DevicesInfo{
				"gpu1": {UID: "gpu1", CardIdx: 0, RenderdIdx: 128},
			},
			doCleanup:     true,
			expectedError: false,
		},
		{
			name: "Existing specs, add new devices to existing spec",
			existingSpecs: []*cdiapi.Spec{
				{
					Spec: &specs.Spec{
						Kind:    device.CDIKind,
						Version: "0.6.0",
						Devices: []specs.Device{
							{
								Name: "gpu1",
								ContainerEdits: specs.ContainerEdits{
									DeviceNodes: []*specs.DeviceNode{
										{Path: "/dev/dri/card0", HostPath: "/dev/dri/card0", Type: "c"},
										{Path: "/dev/dri/renderD128", HostPath: "/dev/dri/renderD128", Type: "c"},
									},
								},
							},
						},
					},
				},
			},
			detectedDevices: device.DevicesInfo{
				"gpu2": {UID: "gpu2", CardIdx: 1, RenderdIdx: 129},
			},
			doCleanup:     false,
			expectedError: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testDirs, err := testhelpers.NewTestDirs(device.DriverName)
			if err != nil {
				t.Fatalf("could not create fake system dirs: %v", err)
			}

			cdiCache, err := cdiapi.NewCache(cdiapi.WithSpecDirs(testDirs.CdiRoot))
			if err != nil {
				t.Fatalf("failed to create CDI cache: %v", err)
			}

			for _, spec := range tt.existingSpecs {
				if err := cdiCache.WriteSpec(spec.Spec, device.CDIVendor); err != nil {
					t.Fatalf("failed to write spec, %v", err)
				}
			}
			t.Logf("existing specs: %v", cdiCache.GetVendorSpecs(device.CDIVendor))

			if err := SyncDetectedDevicesWithRegistry(cdiCache, tt.detectedDevices, tt.doCleanup); (err != nil) != tt.expectedError {
				t.Errorf("SyncDetectedDevicesWithRegistry() error = %v, expectedError %v", err, tt.expectedError)
			}
		})
	}
}
