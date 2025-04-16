/* Copyright (C) 2025 Intel Corporation
 * SPDX-License-Identifier: Apache-2.0
 */

package device

import (
	"testing"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/helpers"
)

func TestCDIName(t *testing.T) {
	tests := []struct {
		name     string
		device   DeviceInfo
		expected string
	}{
		{
			name: "Valid device UID",
			device: DeviceInfo{
				UID: "0000-01-02-0-0x1234",
			},
			expected: "intel.com/gpu=0000-01-02-0-0x1234",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.device.CDIName()
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestDevicesInfoDeepCopy(t *testing.T) {
	original := DevicesInfo{
		"0000-01-02-0-0x1234": {
			UID:        "0000-01-02-0-0x1234",
			PCIAddress: "0000:01:02.0",
			DeviceType: "GPU",
		},
	}

	copy := original.DeepCopy()

	if &copy == &original {
		t.Error("DeepCopy() returned the same pointer, expected different pointers")
	}

	for key, originalDevice := range original {
		copyDevice, exists := copy[key]
		if !exists {
			t.Errorf("DeepCopy() missing device with key %v", key)
			continue
		}

		if copyDevice == originalDevice {
			t.Errorf("DeepCopy() returned the same pointer for device with key %v, expected different pointers", key)
		}

		if *copyDevice != *originalDevice {
			t.Errorf("DeepCopy() returned different values for device with key %v, expected identical values", key)
		}
	}
}

func TestDrmVFIndex(t *testing.T) {
	device := DeviceInfo{
		VFIndex: 5,
	}
	expected := uint64(6)
	if device.DrmVFIndex() != expected {
		t.Errorf("expected %d, got %d", expected, device.DrmVFIndex())
	}
}

func TestSriovEnabled(t *testing.T) {
	device := DeviceInfo{
		MaxVFs: 4,
	}
	if !device.SriovEnabled() {
		t.Error("expected SR-IOV to be enabled, but it was not")
	}

	device.MaxVFs = 0
	if device.SriovEnabled() {
		t.Error("expected SR-IOV to be disabled, but it was enabled")
	}
}

func TestParentPCIAddress(t *testing.T) {
	t.Run("Valid Parent UID", func(t *testing.T) {
		device := DeviceInfo{
			ParentUID: "0000-01-02-0-0x1234",
		}
		expected := "0000:01:02.0"
		result := device.ParentPCIAddress()
		if result != expected {
			t.Errorf("expected %v, got %v", expected, result)
		}
	})
}

func TestSetModelInfo(t *testing.T) {
	tests := []struct {
		name           string
		device         DeviceInfo
		expectedName   string
		expectedFamily string
	}{
		{
			name: "Known model ID",
			device: DeviceInfo{
				Model: "0x56a0",
			},
			expectedName:   "A770",
			expectedFamily: "Arc",
		},
		{
			name: "Unknown model ID",
			device: DeviceInfo{
				Model: "0x9999",
			},
			expectedName:   "Unknown",
			expectedFamily: "Unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.device.SetModelInfo()
			if tt.device.ModelName != tt.expectedName {
				t.Errorf("expected model name %v, got %v", tt.expectedName, tt.device.ModelName)
			}
			if tt.device.FamilyName != tt.expectedFamily {
				t.Errorf("expected family name %v, got %v", tt.expectedFamily, tt.device.FamilyName)
			}
		})
	}
}

func TestGetDriDevPath(t *testing.T) {
	tests := []struct {
		name         string
		envVarValue  string
		expectedPath string
	}{
		{
			name:         "Default devfs path",
			envVarValue:  "",
			expectedPath: "/dev/dri",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(helpers.DevfsEnvVarName, tt.envVarValue)
			result := GetDriDevPath()
			if result != tt.expectedPath {
				t.Errorf("expected %v, got %v", tt.expectedPath, result)
			}
		})
	}
}
