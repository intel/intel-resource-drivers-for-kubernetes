package helpers

import (
	"os"
	"path"
	"testing"
)

func TestGetSysfsRoot(t *testing.T) {
	tests := []struct {
		name        string
		envVarValue string
		sysfsPath   string
		expected    string
		setupEnv    bool
	}{
		{
			name:        "Custom sysfs location exists",
			envVarValue: TestSysfsRoot,
			sysfsPath:   "devices",
			expected:    TestSysfsRoot,
			setupEnv:    true,
		},
		{
			name:        "Custom sysfs location does not exist",
			envVarValue: "/invalid/sys",
			sysfsPath:   "devices",
			expected:    sysfsDefaultRoot,
			setupEnv:    true,
		},
		{
			name:        "Default sysfs location",
			envVarValue: "",
			sysfsPath:   "devices",
			expected:    sysfsDefaultRoot,
			setupEnv:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setupEnv {
				os.Setenv(SysfsEnvVarName, tt.envVarValue)
				defer os.Unsetenv(SysfsEnvVarName)
			}

			if tt.envVarValue != "" {
				if err := os.MkdirAll(path.Join(tt.envVarValue, tt.sysfsPath), os.ModePerm); err != nil {
					t.Logf("failed to create directory: %v", err)
				}
				defer os.RemoveAll(tt.envVarValue)
			}

			result := GetSysfsRoot(tt.sysfsPath)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestGetDevRoot(t *testing.T) {
	tests := []struct {
		name        string
		envVarName  string
		envVarValue string
		devPath     string
		expected    string
		setupEnv    bool
	}{
		{
			name:        "Custom devfs location exists",
			envVarName:  DevfsEnvVarName,
			envVarValue: TestDevfsRoot,
			devPath:     "devices",
			expected:    TestDevfsRoot,
			setupEnv:    true,
		},
		{
			name:        "Custom devfs location does not exist",
			envVarName:  DevfsEnvVarName,
			envVarValue: "/invalid/dev",
			devPath:     "devices",
			expected:    devfsDefaultRoot,
			setupEnv:    true,
		},
		{
			name:        "Default devfs location",
			envVarName:  DevfsEnvVarName,
			envVarValue: "",
			devPath:     "devices",
			expected:    devfsDefaultRoot,
			setupEnv:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setupEnv {
				os.Setenv(tt.envVarName, tt.envVarValue)
				defer os.Unsetenv(tt.envVarName)
			}

			if tt.envVarValue != "" {
				if err := os.MkdirAll(path.Join(tt.envVarValue, tt.devPath), os.ModePerm); err != nil {
					t.Logf("failed to create directory: %v", err)
				}
				defer os.RemoveAll(tt.envVarValue)
			}

			result := GetDevRoot(tt.envVarName, tt.devPath)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestPciInfoFromDeviceUID(t *testing.T) {
	tests := []struct {
		name               string
		deviceUID          string
		expectedPCIAddress string
		expectedPCIID      string
	}{
		{
			name:               "Valid device UID",
			deviceUID:          "1234-56-78-9-0x1234",
			expectedPCIAddress: "1234:56:78.9",
			expectedPCIID:      "0x1234",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pciAddress, pciID := PciInfoFromDeviceUID(tt.deviceUID)
			if pciAddress != tt.expectedPCIAddress || pciID != tt.expectedPCIID {
				t.Errorf("expected PCI address %v and PCI ID %v, got PCI address %v and PCI ID %v", tt.expectedPCIAddress, tt.expectedPCIID, pciAddress, pciID)
			}
		})
	}
}

func TestDeviceUIDFromPCIinfo(t *testing.T) {
	tests := []struct {
		name       string
		pciAddress string
		pciid      string
		expected   string
	}{
		{
			name:       "Valid PCI address and ID",
			pciAddress: "0000:00:01.0",
			pciid:      "0x0000",
			expected:   "0000-00-01-0-0x0000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DeviceUIDFromPCIinfo(tt.pciAddress, tt.pciid)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}
