package discovery

import (
	"os"
	"path"
	"reflect"
	"testing"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/fakesysfs"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gaudi/device"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/helpers"
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

func TestGetAccelIndex(t *testing.T) {
	tests := []struct {
		name        string
		setupFunc   func(string, string) error
		expectedIdx uint64
		shouldFail  bool
	}{
		{
			name:        "valid accel indexes",
			setupFunc:   func(blank string, blank2 string) error { return nil },
			expectedIdx: 1,
			shouldFail:  false,
		},
		{
			name: "invalid accel index",
			setupFunc: func(sysfsroot, pciAddress string) error {
				return os.Rename(
					path.Join(sysfsroot, "bus/pci/drivers/habanalabs", pciAddress, "accel/accel1"),
					path.Join(sysfsroot, "bus/pci/drivers/habanalabs", pciAddress, "accel/accel18446744073709551616"))
			},
			expectedIdx: 0,
			shouldFail:  true,
		},
		{
			name: "invalid accel device name",
			setupFunc: func(sysfsroot, pciAddress string) error {
				return os.Rename(
					path.Join(sysfsroot, "bus/pci/drivers/habanalabs", pciAddress, "accel/accel1"),
					path.Join(sysfsroot, "bus/pci/drivers/habanalabs", pciAddress, "accel/accelX"))
			},
			expectedIdx: 0,
			shouldFail:  true,
		},
		{
			name:        "Sysfs directory does not exist",
			setupFunc:   func(sysfsRoot, pciAddress string) error { return os.RemoveAll(sysfsRoot) },
			expectedIdx: 0,
			shouldFail:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testDirs, err := testhelpers.NewTestDirs(device.DriverName)
			if err != nil {
				t.Fatalf("%v: could not create fake system dirs: %v", tt.name, err)
			}
			defer testhelpers.CleanupTest(t, "TestAddDeviceToAnySpec", testDirs.TestRoot)

			if err := fakesysfs.FakeSysFsGaudiContents(
				testDirs.TestRoot,
				testDirs.SysfsRoot,
				testDirs.DevfsRoot,
				device.DevicesInfo{
					"0000-0f-00-0-0x1020": {
						Model:      "0x1020",
						PCIAddress: "0000:0f:00.0",
						PCIRoot:    "pci0000:00",
						DeviceIdx:  1,
						ModuleIdx:  0,
						UID:        "0000-0f-00-0-0x1020",
					},
				},
				false); err != nil {
				t.Fatalf("%v: could not setup fake sysfs for test: %v", tt.name, err)
			}

			if tt.setupFunc != nil {
				if err := tt.setupFunc(testDirs.SysfsRoot, "0000:0f:00.0"); err != nil {
					t.Fatalf("%v: could not set up test: %v", tt.name, err)
				}
			}

			deviceAccelDir := path.Join(testDirs.SysfsRoot, "bus/pci/drivers/habanalabs/0000:0f:00.0/accel")
			idx, err := getAccelIndex(deviceAccelDir)
			if !tt.shouldFail && tt.expectedIdx != idx {
				t.Errorf("%v: expected idx %v, got %v, error: %v", tt.name, tt.expectedIdx, idx, err)
			}
		})
	}
}

func TestDiscoverDevices(t *testing.T) {
	testDevicesInfo := device.DevicesInfo{
		"0000-0f-00-0-0x1020": {
			Model:      "0x1020",
			ModelName:  "Gaudi2",
			PCIAddress: "0000:0f:00.0",
			DeviceIdx:  0,
			ModuleIdx:  0,
			UID:        "0000-0f-00-0-0x1020",
			PCIRoot:    "pci0000:01",
			UVerbsIdx:  1024, // device.UverbsMissingIdx
		},
	}

	tests := []struct {
		name        string
		setupFunc   func(string, string) error
		cleanupFunc func(string) error
		expected    map[string]*device.DeviceInfo
		shouldFail  bool
	}{
		{
			name: "single device",
			setupFunc: func(sysfsRoot, pciAddr string) error {
				return nil
			},
			expected:   testDevicesInfo,
			shouldFail: false,
		},
		{
			name: "sysfsDir does not exist",
			setupFunc: func(sysfsRoot, pciAddress string) error {
				return os.RemoveAll(sysfsRoot)
			},
			expected:   map[string]*device.DeviceInfo{},
			shouldFail: true,
		},
		{
			name: "sysfsDir exist, but not readable",
			setupFunc: func(sysfsRoot, pciAddress string) error {
				return os.Chmod(sysfsRoot, 0200)
			},
			cleanupFunc: func(sysfsRoot string) error {
				return os.Chmod(sysfsRoot, 0777)
			},
			expected:   map[string]*device.DeviceInfo{},
			shouldFail: true,
		},
		{
			name: "missing module_id file",
			setupFunc: func(sysfsroot, pciAddress string) error {
				return os.Remove(path.Join(sysfsroot, "bus/pci/drivers/habanalabs", pciAddress, "module_id"))
			},
			expected:   map[string]*device.DeviceInfo{},
			shouldFail: true,
		},
		{
			name: "invalid module_id index",
			setupFunc: func(sysfsroot, pciAddress string) error {
				return helpers.WriteFile(path.Join(sysfsroot, "bus/pci/drivers/habanalabs", pciAddress, "module_id"), "X")
			},
			expected:   map[string]*device.DeviceInfo{},
			shouldFail: true,
		},
		{
			name: "device file does not exist",
			setupFunc: func(sysfsRoot, pciAddress string) error {
				return os.RemoveAll(path.Join(sysfsRoot, "bus/pci/drivers/habanalabs", pciAddress, "device"))
			},
			expected:   map[string]*device.DeviceInfo{},
			shouldFail: true,
		},
		{
			name: "accel dir does not exist",
			setupFunc: func(sysfsRoot, pciAddress string) error {
				return os.RemoveAll(path.Join(sysfsRoot, "bus/pci/drivers/habanalabs", pciAddress, "accel"))
			},
			expected:   map[string]*device.DeviceInfo{},
			shouldFail: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testDirs, err := testhelpers.NewTestDirs(device.DriverName)
			if err != nil {
				t.Fatalf("could not create fake system dirs: %v", err)
			}
			defer testhelpers.CleanupTest(t, "TestDiscoverDevices", testDirs.TestRoot)

			if err := fakesysfs.FakeSysFsGaudiContents(
				testDirs.TestRoot,
				testDirs.SysfsRoot,
				testDirs.DevfsRoot,
				testDevicesInfo,
				false,
			); err != nil {
				t.Errorf("setup error: could not create fake sysfs: %v", err)
				return
			}

			if err := tt.setupFunc(testDirs.SysfsRoot, "0000:0f:00.0"); err != nil {
				t.Fatalf("could not set up test: %v", err)
			}
			result := DiscoverDevices(testDirs.SysfsRoot, device.DefaultNamingStyle)
			if !tt.shouldFail && !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("expected %+v, got %+v", tt.expected["0000-0f-00-0-0x1020"], result["0000-0f-00-0-0x1020"])
			}

			// Run cleanup function if provided
			if tt.cleanupFunc != nil {
				if err := tt.cleanupFunc(testDirs.SysfsRoot); err != nil {
					t.Errorf("Could not properly cleanup %v: %v", tt.name, err)
				}
			}
		})
	}
}
