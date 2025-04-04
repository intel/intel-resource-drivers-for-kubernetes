package cdihelpers

import (
	"testing"
	"time"

	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"
	cdiSpecs "tags.cncf.io/container-device-interface/specs-go"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gaudi/device"
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
			existingSpecs: []*cdiapi.Spec{},
			detectedDevices: device.DevicesInfo{
				"device1": {DeviceIdx: 1},
				"device2": {DeviceIdx: 2},
			},
			doCleanup:     false,
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
			detectedDevices: device.DevicesInfo{
				"device1": {DeviceIdx: 1},
				"device2": {DeviceIdx: 2},
			},
			doCleanup:     false,
			expectedError: false,
		},
		{
			name: "Existing specs, no detected devices",
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
			detectedDevices: device.DevicesInfo{},
			doCleanup:       true,
			expectedError:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testDirs, err := testhelpers.NewTestDirs(device.DriverName)
			if err != nil {
				t.Fatalf("could not create fake system dirs: %v", err)
			}
			defer testhelpers.CleanupTest(t, "TestAddDeviceToAnySpec", testDirs.TestRoot)

			cdiCache, err := cdiapi.NewCache(cdiapi.WithSpecDirs(testDirs.CdiRoot))
			if err != nil {
				t.Fatalf("failed to create CDI cache: %v", err)
			}

			for _, spec := range tt.existingSpecs {
				if err := writeSpec(cdiCache, spec.Spec, device.CDIVendor); err != nil {
					t.Fatalf("failed to write spec, %v", err)
				}
			}
			time.Sleep(100 * time.Millisecond)
			t.Logf("existing specs: %v", cdiCache.GetVendorSpecs(device.CDIVendor))

			if err := SyncDetectedDevicesWithRegistry(cdiCache, tt.detectedDevices, tt.doCleanup); (err != nil) != tt.expectedError {
				t.Errorf("SyncDetectedDevicesWithRegistry() error = %v, expectedError %v", err, tt.expectedError)
			}
		})
	}
}

func TestAddDeviceToAnySpec(t *testing.T) {
	tests := []struct {
		name          string
		existingSpecs []*cdiapi.Spec
		newDevice     cdiSpecs.Device
		expectedError bool
	}{
		{
			name: "Add device to existing spec",
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
			newDevice: cdiSpecs.Device{
				Name: "device2",
				ContainerEdits: cdiSpecs.ContainerEdits{
					Env: []string{"VAR2=VAL2"},
				},
			},
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testDirs, err := testhelpers.NewTestDirs(device.DriverName)
			if err != nil {
				t.Fatalf("could not create fake system dirs: %v", err)
			}
			defer testhelpers.CleanupTest(t, "TestAddDeviceToAnySpec", testDirs.TestRoot)

			cdiCache, err := cdiapi.NewCache(cdiapi.WithSpecDirs(testDirs.CdiRoot))
			if err != nil {
				t.Fatalf("failed to create CDI cache: %v", err)
			}

			for _, spec := range tt.existingSpecs {
				if err := writeSpec(cdiCache, spec.Spec, device.CDIVendor); err != nil {
					t.Fatalf("failed to write spec, %v", err)
				}
			}
			time.Sleep(100 * time.Millisecond)
			t.Logf("existing specs: %v", cdiCache.GetVendorSpecs(device.CDIVendor))

			if err := AddDeviceToAnySpec(cdiCache, device.CDIVendor, tt.newDevice); (err != nil) != tt.expectedError {
				t.Errorf("AddDeviceToAnySpec() error = %v, expectedError %v", err, tt.expectedError)
			}

			if !tt.expectedError {
				specs := cdiCache.GetVendorSpecs(device.CDIVendor)
				found := false
				for _, spec := range specs {
					for _, dev := range spec.Devices {
						if dev.Name == tt.newDevice.Name {
							found = true
							break
						}
					}
				}
				if !found {
					t.Errorf("expected device %v to be added, but it was not found", tt.newDevice.Name)
				}
			}
		})
	}
}

func TestDeleteDeviceAndWrite(t *testing.T) {
	tests := []struct {
		name          string
		existingSpecs []*cdiapi.Spec
		claimUID      string
		expectedError bool
	}{
		{
			name: "Delete existing device",
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
			claimUID:      "device1",
			expectedError: false,
		},
		{
			name: "Delete non-existing device",
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
			claimUID:      "device2",
			expectedError: false,
		},
		{
			name: "Delete device from empty spec",
			existingSpecs: []*cdiapi.Spec{
				{
					Spec: &cdiSpecs.Spec{
						Kind:    device.CDIKind,
						Version: "0.6.0",
						Devices: []cdiSpecs.Device{},
					},
				},
			},
			claimUID:      "device1",
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testDirs, err := testhelpers.NewTestDirs(device.DriverName)
			if err != nil {
				t.Fatalf("could not create fake system dirs: %v", err)
			}
			defer testhelpers.CleanupTest(t, "TestDeleteDeviceAndWrite", testDirs.TestRoot)

			cdiCache, err := cdiapi.NewCache(cdiapi.WithSpecDirs(testDirs.CdiRoot))
			if err != nil {
				t.Fatalf("failed to create CDI cache: %v", err)
			}

			for _, spec := range tt.existingSpecs {
				if err := writeSpec(cdiCache, spec.Spec, device.CDIVendor); err != nil {
					t.Fatalf("failed to write spec, %v", err)
				}
			}
			time.Sleep(100 * time.Millisecond)
			t.Logf("existing specs: %v", cdiCache.GetVendorSpecs(device.CDIVendor))

			if err := DeleteDeviceAndWrite(cdiCache, tt.claimUID); (err != nil) != tt.expectedError {
				t.Errorf("DeleteDeviceAndWrite() error = %v, expectedError %v", err, tt.expectedError)
			}

			if !tt.expectedError {
				specs := cdiCache.GetVendorSpecs(device.CDIVendor)
				for _, spec := range specs {
					for _, dev := range spec.Devices {
						if dev.Name == tt.claimUID {
							t.Errorf("expected device %v to be deleted, but it was found", tt.claimUID)
						}
					}
				}
			}
		})
	}
}
