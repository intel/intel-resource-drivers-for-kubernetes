package discovery

import (
	"fmt"
	"os"
	"path"
	"reflect"
	"testing"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/fakesysfs"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gaudi/device"
	testhelpers "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/plugintesthelpers"
)

func TestDetermineDeviceName(t *testing.T) {
	tests := []struct {
		name        string
		info        *device.DeviceInfo
		namingStyle string
		expected    string
	}{
		{
			name: "classic naming style",
			info: &device.DeviceInfo{
				DeviceIdx: 1,
			},
			namingStyle: "classic",
			expected:    "accel1",
		},
		{
			name: "UID naming style",
			info: &device.DeviceInfo{
				UID: "unique-id-123",
			},
			namingStyle: "uid",
			expected:    "unique-id-123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := determineDeviceName(tt.info, tt.namingStyle)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}
func TestGetAccelIndexes(t *testing.T) {
	tests := []struct {
		name       string
		setupFunc  func(string) error
		expected   map[string]gaudiIndexesType
		shouldFail bool
	}{
		{
			name:      "valid accel indexes",
			setupFunc: setupValidAccelIndexes,
			expected: map[string]gaudiIndexesType{
				"0000:00:00.0": {accelIdx: 0, moduleIdx: 1},
			},
			shouldFail: false,
		},
		{
			name:       "missing module_id file",
			setupFunc:  setupMissingModuleIDFile,
			expected:   map[string]gaudiIndexesType{},
			shouldFail: false,
		},
		{
			name:       "missing pci_addr file",
			setupFunc:  setupMissingPCIAddrFile,
			expected:   map[string]gaudiIndexesType{},
			shouldFail: false,
		},
		{
			name:       "invalid accel index",
			setupFunc:  setupInvalidAccelIndex,
			expected:   map[string]gaudiIndexesType{},
			shouldFail: false,
		},
		{
			name:       "Sysfs directory does not exist",
			setupFunc:  setupSysfsDirDoesNotExist,
			expected:   map[string]gaudiIndexesType{},
			shouldFail: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testDirs, err := testhelpers.NewTestDirs(device.DriverName)
			if err != nil {
				t.Fatalf("could not create fake system dirs: %v", err)
			}
			defer testhelpers.CleanupTest(t, "TestAddDeviceToAnySpec", testDirs.TestRoot)
			if tt.setupFunc != nil {
				if err := tt.setupFunc(testDirs.SysfsRoot); err != nil {
					t.Fatalf("could not set up test: %v", err)
				}
			}

			result := getAccelIndexes(testDirs.SysfsRoot)
			if !tt.shouldFail && !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func setupValidAccelIndexes(dir string) error {
	if err := os.MkdirAll(path.Join(dir, "accel0/device"), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(path.Join(dir, "accel0/device/module_id"), []byte("1\n"), 0644); err != nil {
		return err
	}
	if err := os.WriteFile(path.Join(dir, "accel0/device/pci_addr"), []byte("0000:00:00.0\n"), 0644); err != nil {
		return err
	}
	return nil
}

func setupMissingModuleIDFile(dir string) error {
	if err := os.MkdirAll(path.Join(dir, "accel0/device"), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(path.Join(dir, "accel0/device/pci_addr"), []byte("0000:00:00.0\n"), 0644); err != nil {
		return err
	}
	return nil
}

func setupMissingPCIAddrFile(dir string) error {
	if err := os.MkdirAll(path.Join(dir, "accel0/device"), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(path.Join(dir, "accel0/device/module_id"), []byte("1\n"), 0644); err != nil {
		return err
	}
	return nil
}

func setupInvalidAccelIndex(dir string) error {
	if err := os.MkdirAll(path.Join(dir, "accelX/device"), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(path.Join(dir, "accelX/device/module_id"), []byte("1\n"), 0644); err != nil {
		return err
	}
	if err := os.WriteFile(path.Join(dir, "accelX/device/pci_addr"), []byte("0000:00:00.0\n"), 0644); err != nil {
		return err
	}
	return nil
}

func setupSysfsDirDoesNotExist(dir string) error {
	os.RemoveAll(dir)
	return nil
}

func TestDiscoverDevices(t *testing.T) {
	tests := []struct {
		name       string
		setupFunc  func(string, string) error
		expected   map[string]*device.DeviceInfo
		shouldFail bool
	}{
		{
			name: "single device",
			setupFunc: func(SysfsRoot, DevfsRoot string) error {
				if err := fakesysfs.FakeSysFsGaudiContents(
					SysfsRoot,
					DevfsRoot,
					device.DevicesInfo{
						"0000-0f-00-0-0x1020": {Model: "0x1020", PCIAddress: "0000:0f:00.0", DeviceIdx: 0, ModuleIdx: 0, UID: "0000-0f-00-0-0x1020", Healthy: true},
					},
					false,
				); err != nil {
					return fmt.Errorf("setup error: could not create fake sysfs: %v", err)
				}

				return nil
			},
			expected: map[string]*device.DeviceInfo{
				"0000-0f-00-0-0x1020": {
					Model:      "0x1020",
					PCIAddress: "0000:0f:00.0",
					DeviceIdx:  0,
					ModuleIdx:  0,
					UID:        "0000-0f-00-0-0x1020",
					Healthy:    true,
					ModelName:  "Gaudi2",
				},
			},
			shouldFail: false,
		},
		{
			name: "sysfsDir does not exist",
			setupFunc: func(SysfsRoot, DevfsRoot string) error {
				os.RemoveAll(SysfsRoot)
				return nil
			},
			expected:   map[string]*device.DeviceInfo{},
			shouldFail: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testDirs, err := testhelpers.NewTestDirs(device.DriverName)
			if err != nil {
				t.Fatalf("could not create fake system dirs: %v", err)
			}
			defer testhelpers.CleanupTest(t, "TestDiscoverDevices", testDirs.TestRoot)

			if err := tt.setupFunc(testDirs.SysfsRoot, testDirs.DevfsRoot); err != nil {
				t.Fatalf("could not set up test: %v", err)
			}
			result := DiscoverDevices(testDirs.SysfsRoot, device.DefaultNamingStyle)
			if !tt.shouldFail && !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("expected %v, got %v", tt.expected["0000-0f-00-0-0x1020"], result["0000-0f-00-0-0x1020"])
			}
		})
	}
}
