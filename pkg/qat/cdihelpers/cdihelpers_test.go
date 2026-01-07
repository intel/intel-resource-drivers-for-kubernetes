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

package cdihelpers

import (
	"sort"
	"testing"

	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"
	cdiSpecs "tags.cncf.io/container-device-interface/specs-go"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/fakesysfs"
	testhelpers "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/plugintesthelpers"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/qat/device"
)

const DriverName = "qat"

func TestSyncDetectedDevicesWithRegistry(t *testing.T) {

	tests := []struct {
		name            string
		existingSpecs   []*cdiapi.Spec
		detectedDevices fakesysfs.QATDevices
		expectedError   bool
		expectedUIDs    []string
	}{
		{
			name:          "No existing specs, add new devices",
			existingSpecs: []*cdiapi.Spec{},
			detectedDevices: fakesysfs.QATDevices{
				{
					Device:   "0000:4b:00.0",
					State:    "up",
					NumVFs:   2,
					TotalVFs: 2,
				},
			},
			expectedUIDs:  []string{"qatvf-0000-4b-00-1", "qatvf-0000-4b-00-2"},
			expectedError: false,
		},
		{
			name: "Existing specs, update devices",
			existingSpecs: []*cdiapi.Spec{
				{
					Spec: &cdiSpecs.Spec{
						Kind:    device.CDIKind,
						Version: "0.6.0",
						Devices: []cdiSpecs.Device{{
							Name: "device1",
							ContainerEdits: cdiSpecs.ContainerEdits{
								Env: []string{"VAR1=VAL1"},
							},
						}},
					},
				},
			},
			detectedDevices: fakesysfs.QATDevices{
				{
					Device:   "0000:4b:00.0",
					State:    "up",
					NumVFs:   2,
					TotalVFs: 2,
				},
			},
			expectedUIDs:  []string{"qatvf-0000-4b-00-1", "qatvf-0000-4b-00-2"},
			expectedError: false,
		},
		{
			name: "Existing specs, no detected devices",
			existingSpecs: []*cdiapi.Spec{
				{
					Spec: &cdiSpecs.Spec{
						Kind:    device.CDIKind,
						Version: "0.6.0",
						Devices: []cdiSpecs.Device{},
					},
				},
			},
			detectedDevices: fakesysfs.QATDevices{},
			expectedUIDs:    []string{},
			expectedError:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testDirs, err := testhelpers.NewTestDirs(device.DriverName)
			if err != nil {
				t.Fatalf("could not create fake system dirs: %v", err)
			}
			defer testhelpers.CleanupTest(t, tt.name, testDirs.TestRoot)

			t.Setenv("SYSFS_ROOT", testDirs.SysfsRoot)
			defer device.ClearSysfsRoot()

			cdiCache, err := cdiapi.NewCache(cdiapi.WithSpecDirs(testDirs.CdiRoot))
			if err != nil {
				t.Fatalf("failed to create CDI cache: %v", err)
			}

			specName := cdiapi.GenerateSpecName(device.CDIVendor, device.CDIClass)
			for _, spec := range tt.existingSpecs {
				if err := writeSpec(cdiCache, spec.Spec, specName); err != nil {
					t.Fatalf("failed to write spec, %v", err)
				}
			}
			testhelpers.CDICacheDelay()

			if err := fakesysfs.FakeSysFsQATContents(testDirs.SysfsRoot, tt.detectedDevices); err != nil {
				t.Errorf("setup error: could not create fake sysfs: %v", err)
			}

			devs, err := device.New()
			if err != nil {
				t.Fatalf("New error: %v", err)
			}

			vfDevices := device.GetCDIDevices(devs)

			t.Logf("existing specs: %v", cdiCache.GetVendorSpecs(device.CDIVendor))

			if err := AddDetectedDevicesToCDIRegistry(cdiCache, vfDevices); (err != nil) != tt.expectedError {
				t.Errorf("SyncDetectedDevicesWithRegistry() error = %v, expectedError %v", err, tt.expectedError)
			}

			testhelpers.CDICacheDelay()

			specs := cdiCache.GetVendorSpecs(device.CDIVendor)
			var foundUIDs []string
			for _, spec := range specs {
				for _, d := range spec.Devices {
					foundUIDs = append(foundUIDs, d.Name)
				}
			}
			sort.Strings(foundUIDs)

			if len(foundUIDs) != len(tt.expectedUIDs) {
				t.Fatalf("Mismatch in number of devices: expected %d, got %d", len(tt.expectedUIDs), len(foundUIDs))
			}

			for i, foundUID := range foundUIDs {
				if foundUID != tt.expectedUIDs[i] {
					t.Errorf("Mismatch at index %d: expected %s, got %s", i, tt.expectedUIDs[i], foundUID)
				}
			}
		})
	}
}
