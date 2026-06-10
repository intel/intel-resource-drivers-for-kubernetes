/* Copyright (C) 2025 Intel Corporation
 * SPDX-License-Identifier: Apache-2.0
 */

package cdihelpers

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"

	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"
	specs "tags.cncf.io/container-device-interface/specs-go"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/plugintesthelpers"
)

func TestAddDetectedDevicesToCDIRegistry(t *testing.T) {

	tests := []struct {
		name             string
		existingSpecs    []*cdiapi.Spec
		detectedDevices  device.DevicesInfo
		expectedError    bool
		expectedMEINames []string
	}{
		{
			name:             "No existing specs, no detected devices",
			existingSpecs:    nil,
			detectedDevices:  device.DevicesInfo{},
			expectedError:    false,
			expectedMEINames: nil,
		},
		{
			name:          "No existing specs, add new devices",
			existingSpecs: nil,
			detectedDevices: device.DevicesInfo{
				"0000-0f-00-0-0x56c0": {
					Model:       "0x56c0",
					ModelName:   "Flex 170",
					FamilyName:  "Data Center Flex",
					PCIAddress:  "0000:0f:00.0",
					MemoryMiB:   8192,
					DeviceType:  "gpu",
					CardName:    "card0",
					MEIName:     "mei0",
					RenderDName: "renderD128",
					Millicores:  1000,
					UID:         "0000-0f-00-0-0x56c0",
					MaxVFs:      16,
				},
				"0000-0f-00-1-0x56c0": {
					Model:       "0x56c0",
					ModelName:   "Flex 170",
					FamilyName:  "Data Center Flex",
					PCIAddress:  "0000:0f:00.1",
					MemoryMiB:   8192,
					DeviceType:  "vf",
					ParentUID:   "0000-0f-00-0-0x56c0",
					CardName:    "card1",
					RenderDName: "renderD129",
					Millicores:  1000,
					UID:         "0000-0f-00-1-0x56c0",
					MaxVFs:      0,
				},
			},
			expectedError:    false,
			expectedMEINames: []string{"mei0"},
		},
		{
			name:          "No existing MEI spec, add devices with MEI",
			existingSpecs: nil,
			detectedDevices: device.DevicesInfo{
				"gpu0": {UID: "gpu0", CardName: "card0", RenderDName: "renderD128", MEIName: "mei0"},
				"gpu1": {UID: "gpu1", CardName: "card1", RenderDName: "renderD129", MEIName: "mei1"},
			},
			expectedError:    false,
			expectedMEINames: []string{"mei0", "mei1"},
		},
		{
			name: "Existing MEI spec is replaced",
			existingSpecs: []*cdiapi.Spec{
				{
					Spec: &specs.Spec{
						Kind:    device.CDIMEIKind,
						Version: "0.6.0",
						Devices: []specs.Device{
							{
								Name: "mei9",
								ContainerEdits: specs.ContainerEdits{
									DeviceNodes: []*specs.DeviceNode{
										{Path: "/dev/mei9", HostPath: "/dev/mei9", Type: "c"},
									},
								},
							},
						},
					},
				},
			},
			detectedDevices: device.DevicesInfo{
				"gpu0": {UID: "gpu0", CardName: "card0", RenderDName: "renderD128", MEIName: "mei0"},
			},
			expectedError:    false,
			expectedMEINames: []string{"mei0"},
		},
		{
			name: "Existing specs, detected devices replace old ones",
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
				"gpu1": {UID: "gpu1", CardName: "card0", RenderDName: "renderD128"},
			},
			expectedError:    false,
			expectedMEINames: nil,
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
				"gpu1": {UID: "gpu1", CardName: "card0", RenderDName: "renderD128"},
			},
			expectedError:    false,
			expectedMEINames: nil,
		},
		{
			name: "Existing specs, remove absent devices",
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
				"gpu1": {UID: "gpu1", CardName: "card0", RenderDName: "renderD128"},
			},
			expectedError:    false,
			expectedMEINames: nil,
		},
		{
			name: "Existing specs, all devices removed",
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
			detectedDevices:  device.DevicesInfo{},
			expectedError:    false,
			expectedMEINames: nil,
		},
		{
			name: "Existing specs, replace with new device",
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
				"gpu2": {UID: "gpu2", CardName: "card1", RenderDName: "renderD129"},
			},
			expectedError:    false,
			expectedMEINames: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testDirs, err := plugintesthelpers.NewTestDirs(device.DriverName)
			defer plugintesthelpers.CleanupTest(t, tt.name, testDirs.TestRoot)
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
			plugintesthelpers.CDICacheDelay()

			t.Logf("existing specs: %v", cdiCache.GetVendorSpecs(device.CDIVendor))

			if err := AddDetectedDevicesToCDIRegistry(cdiCache, tt.detectedDevices); (err != nil) != tt.expectedError {
				t.Errorf("AddDetectedDevicesToCDIRegistry() error = %v, expectedError %v", err, tt.expectedError)
			}

			plugintesthelpers.CDICacheDelay()

			actualMEINames := []string{}
			for _, meiSpec := range getMEISpecs(cdiCache) {
				for _, meiDevice := range meiSpec.Devices {
					actualMEINames = append(actualMEINames, meiDevice.Name)
				}
			}
			sort.Strings(actualMEINames)

			expectedMEINames := tt.expectedMEINames
			sort.Strings(expectedMEINames)

			if len(actualMEINames) != len(expectedMEINames) {
				t.Fatalf("expected MEI CDI devices %v, got %v", expectedMEINames, actualMEINames)
			}
			for i := range actualMEINames {
				if actualMEINames[i] != expectedMEINames[i] {
					t.Fatalf("expected MEI CDI devices %v, got %v", expectedMEINames, actualMEINames)
				}
			}
		})
	}
}

func TestUpdateGPUDevices(t *testing.T) {

	tests := []struct {
		name               string
		existingSpecs      []*cdiapi.Spec
		detectedDevices    []*device.DeviceInfo
		expectedError      bool
		expectedCDIDevices []specs.Device
	}{
		{
			name: "Existing specs, update a device",
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
			detectedDevices: []*device.DeviceInfo{
				{UID: "gpu2", VFIODevice: "vfio0", IOMMUGroup: "15", Driver: "xe", CurrentDriver: "xe-vfio-pci"},
			},
			expectedError: false,
			expectedCDIDevices: []specs.Device{
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
							{Path: "/dev/vfio/15", HostPath: "/dev/vfio/15", Type: "c"},
							{Path: "/dev/vfio/vfio", HostPath: "/dev/vfio/vfio", Type: "c"},
							{Path: "/dev/vfio/devices/vfio0", HostPath: "/dev/vfio/devices/vfio0", Type: "c"},
						},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testDirs, err := plugintesthelpers.NewTestDirs(device.DriverName)
			defer plugintesthelpers.CleanupTest(t, tt.name, testDirs.TestRoot)
			if err != nil {
				t.Fatalf("could not create fake system dirs: %v", err)
			}

			cdiCache, err := cdiapi.NewCache(cdiapi.WithSpecDirs(testDirs.CdiRoot))
			if err != nil {
				t.Fatalf("failed to create CDI cache: %v", err)
			}

			for _, existingSpec := range tt.existingSpecs {
				if err := writeSpec(cdiCache, existingSpec.Spec); err != nil {
					t.Fatalf("failed to write spec, %v", err)
				}
			}
			plugintesthelpers.CDICacheDelay()

			if err := UpdateGPUDevices(cdiCache, tt.detectedDevices); (err != nil) != tt.expectedError {
				t.Errorf("UpdateGPUDevices() error = %v, expectedError %v", err, tt.expectedError)
			}

			plugintesthelpers.CDICacheDelay()

			actualCDIDevices := []specs.Device{}
			for _, gpuSpec := range getGPUSpecs(cdiCache) {
				actualCDIDevices = append(actualCDIDevices, gpuSpec.Devices...)
			}

			actualJSON, _ := json.MarshalIndent(actualCDIDevices, "", "\t")
			expectedJSON, _ := json.MarshalIndent(tt.expectedCDIDevices, "", "\t")
			if !reflect.DeepEqual(actualCDIDevices, tt.expectedCDIDevices) {
				t.Fatalf("expected GPU CDI devices %v, got %v", string(expectedJSON), string(actualJSON))
			}
		})
	}
}
