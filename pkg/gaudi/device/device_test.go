package device

import (
	"testing"
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
			expected: "intel.com/gaudi=0000-01-02-0-0x1234",
		},
		{
			name: "Another valid device UID",
			device: DeviceInfo{
				UID: "0000-02-03-0-0x6789",
			},
			expected: "intel.com/gaudi=0000-02-03-0-0x6789",
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
			Model:      "0x1020",
			ModelName:  "Gaudi2",
			DeviceIdx:  1,
			ModuleIdx:  2,
			PCIRoot:    "0000:00",
			Serial:     "1234567890",
			Healthy:    true,
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

func TestSetModelName(t *testing.T) {
	tests := []struct {
		name       string
		deviceInfo DeviceInfo
		expected   string
	}{
		{
			name: "Known model 0x1000",
			deviceInfo: DeviceInfo{
				Model: "0x1000",
			},
			expected: "Gaudi",
		},
		{
			name: "Known model 0x1020",
			deviceInfo: DeviceInfo{
				Model: "0x1020",
			},
			expected: "Gaudi2",
		},
		{
			name: "Unknown model",
			deviceInfo: DeviceInfo{
				Model: "0x9999",
			},
			expected: "Unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.deviceInfo.SetModelName()
			if tt.deviceInfo.ModelName != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, tt.deviceInfo.ModelName)
			}
		})
	}
}
