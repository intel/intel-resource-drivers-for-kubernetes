package main

import (
	"context"
	"reflect"
	"testing"

	xpumapi "github.com/intel/xpumanager/xpumd/exporter/api/deviceinfo/v1alpha1"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/fakesysfs"
	gpudevice "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
	testhelpers "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/plugintesthelpers"
)

// WARNING: THIS IS A STATEFUL TEST - THE DRIVER IS _NOT_ CREATED PER TEST CASE,
// INSTEAD IT IS CREATED ONCE BEFORE THE TEST LOOP, AND EVERY NEXT TEST CASE MUTATES
// THE SAME DRIVER STATE.
func TestConsumeXPUMDDeviceDetails(t *testing.T) {
	testDirs, err := testhelpers.NewTestDirs(gpudevice.DriverName)
	defer testhelpers.CleanupTest(t, "GPU TestConsumeXPUMDDeviceDetails", testDirs.TestRoot)
	if err != nil {
		t.Fatalf("setup error creating test dirs: %v", err)
	}

	testDevices := gpudevice.DevicesInfo{
		"0000-00-02-0-0x56c0": {
			UID:        "0000-00-02-0-0x56c0",
			Model:      "0x56c0",
			DeviceType: "gpu",
			Driver:     "i915",
			CardIdx:    0,
			RenderdIdx: 128,
			MemoryMiB:  8192,
			MaxVFs:     16,
		},
	}
	if err := fakesysfs.FakeSysFsGpuContents(testDirs.SysfsRoot, testDirs.DevfsRoot, testDevices, false); err != nil {
		t.Fatalf("could not create fake sysfs: %v", err)
	}

	drv, err := getFakeDriver(testDirs)
	if err != nil {
		t.Fatalf("could not create fake driver: %v", err)
	}
	//	defer func() { _ = drv.Shutdown(context.TODO()) }()

	//nolint:forcetypeassert // We want the code to panic if our assumption turns out to be wrong.
	allocatable := drv.state.Allocatable.(map[string]*gpudevice.DeviceInfo)
	dev := allocatable["0000-00-02-0-0x56c0"]
	// Health becomes gpudevice.HealthUnknown when health monitoring is not enabled.
	dev.Health = gpudevice.HealthHealthy

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tests := []struct {
		name                string
		updates             []*xpumapi.DeviceHealth
		ignoreHealthWarning bool
		expectedHealth      string
		missingDevice       bool // if true, the device should not be in allocatable
	}{
		{
			name: "One device full update with healthy status",
			updates: []*xpumapi.DeviceHealth{
				{
					Info: &xpumapi.DeviceInformation{
						Pci: &xpumapi.PciInfo{
							Bdf:      "0000:00:02.0",
							DeviceId: "56c0",
						},
						Model: "Intel Arc A770",
						Memory: []*xpumapi.MemoryInfo{
							{
								Type: "gddr6",
								Size: uint64(16 * 1024 * 1024 * 1024),
							},
						},
						Firmwares: []*xpumapi.FirmwareInfo{
							{Name: "gfx_data", Version: "Major+%3A+203%2C+OEM+Manufacturing+Data+%3A+5%2C+Major+VCN+%3A+0"},
							{Name: "fantable", Version: "0.0.0.0"},
							{Name: "vrconfig", Version: "0.0.0.0"},
						},
					},
					Health: []*xpumapi.HealthStatus{
						{Name: "frequency", Severity: xpumapi.SeverityLevel_SEVERITY_LEVEL_OK, Reason: "ok"},
						{Name: "memory", Reason: "unknown"},
						{Name: "temperature.core.gpu", Severity: xpumapi.SeverityLevel_SEVERITY_LEVEL_OK, Reason: "ok"},
						{Name: "temperature.memory.memory", Severity: xpumapi.SeverityLevel_SEVERITY_LEVEL_OK, Reason: "ok"},
					},
				},
			},
			ignoreHealthWarning: false,
			expectedHealth:      "Healthy",
		},
		{
			name: "Repeated full device update with healthy status, no changes",
			updates: []*xpumapi.DeviceHealth{
				{
					Info: &xpumapi.DeviceInformation{
						Pci: &xpumapi.PciInfo{
							Bdf:      "0000:00:02.0",
							DeviceId: "56c0",
						},
						Model: "Intel Arc A770",
						Memory: []*xpumapi.MemoryInfo{
							{
								Type: "gddr6",
								Size: uint64(16 * 1024 * 1024 * 1024),
							},
						},
						Firmwares: []*xpumapi.FirmwareInfo{
							{Name: "gfx_data", Version: "Major+%3A+203%2C+OEM+Manufacturing+Data+%3A+5%2C+Major+VCN+%3A+0"},
							{Name: "fantable", Version: "0.0.0.0"},
							{Name: "vrconfig", Version: "0.0.0.0"},
						},
					},
					Health: []*xpumapi.HealthStatus{
						{Name: "frequency", Severity: xpumapi.SeverityLevel_SEVERITY_LEVEL_OK, Reason: "ok"},
						{Name: "memory", Reason: "unknown"},
						{Name: "temperature.core.gpu", Severity: xpumapi.SeverityLevel_SEVERITY_LEVEL_OK, Reason: "ok"},
						{Name: "temperature.memory.memory", Severity: xpumapi.SeverityLevel_SEVERITY_LEVEL_OK, Reason: "ok"},
					},
				},
			},
			ignoreHealthWarning: false,
			expectedHealth:      "Healthy",
		},
		{
			name: "One device full update with unhealthy status",
			updates: []*xpumapi.DeviceHealth{
				{
					Info: &xpumapi.DeviceInformation{
						Pci: &xpumapi.PciInfo{
							Bdf:      "0000:00:02.0",
							DeviceId: "56c0",
						},
						Model: "Intel Arc A770",
						Memory: []*xpumapi.MemoryInfo{
							{
								Type: "gddr6",
								Size: uint64(1 * 1024 * 1024 * 1024),
							},
						},
						Firmwares: []*xpumapi.FirmwareInfo{
							{Name: "gfx_data", Version: "Major+%3A+203%2C+OEM+Manufacturing+Data+%3A+5%2C+Major+VCN+%3A+0"},
							{Name: "fantable", Version: "0.0.0.0"},
							{Name: "vrconfig", Version: "0.0.0.0"},
						},
					},
					Health: []*xpumapi.HealthStatus{
						{Name: "frequency", Severity: xpumapi.SeverityLevel_SEVERITY_LEVEL_CRITICAL, Reason: "oops"},
						{Name: "memory", Reason: "unknown"},
						{Name: "temperature.core.gpu", Severity: xpumapi.SeverityLevel_SEVERITY_LEVEL_OK, Reason: "ok"},
						{Name: "temperature.memory.memory", Severity: xpumapi.SeverityLevel_SEVERITY_LEVEL_OK, Reason: "ok"},
					},
				},
			},
			ignoreHealthWarning: false,
			expectedHealth:      "Unhealthy",
		},
		{
			// TODO: implement rediscovery. For now this just triggers error log message from the driver.
			name: "Unexpected undiscovered device missing from allocatable",
			updates: []*xpumapi.DeviceHealth{
				{
					Info: &xpumapi.DeviceInformation{
						Pci: &xpumapi.PciInfo{
							Bdf:      "0000:03:00.0",
							DeviceId: "56c0",
						},
						Model: "DataCenter Flex 170",
						Memory: []*xpumapi.MemoryInfo{
							{
								Type: "gddr6",
								Size: uint64(16 * 1024 * 1024 * 1024),
							},
						},
					},
				},
			},
			ignoreHealthWarning: false,
			expectedHealth:      "Unhealthy",
			missingDevice:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset device to healthy state before each test.
			dev.Health = gpudevice.HealthHealthy

			// Set the driver's ignoreHealthWarning setting.
			drv.ignoreHealthWarning = tt.ignoreHealthWarning
			drv.ConsumeXPUMDDeviceDetails(ctx, tt.updates)

			// Check drv.state.Allocatable has been updated with the new health and memory info.
			updatedDev, exists := drv.state.Allocatable.(map[string]*gpudevice.DeviceInfo)["0000-00-02-0-0x56c0"]
			if tt.missingDevice {
				// Nothing to check, the error must have been logged.
				return
			}
			if !exists {
				t.Fatalf("device %v not found in allocatable after update", "0000-00-02-0-0x56c0")
			}
			if updatedDev.Health != tt.expectedHealth {
				t.Errorf("expected health %s, got %s", tt.expectedHealth, updatedDev.Health)
			}
			expectedMemoryMiB := tt.updates[0].Info.Memory[0].Size / (1024 * 1024)
			if updatedDev.MemoryMiB != expectedMemoryMiB {
				t.Errorf("expected MemoryMiB %d, got %d", expectedMemoryMiB, updatedDev.MemoryMiB)
			}
		})
	}
}

func TestXpumDevicesToAllocatableDevicesInfo(t *testing.T) {
	tests := []struct {
		name          string
		xpumDevices   []*xpumapi.DeviceHealth
		ignoreWarning bool
		expectDevices gpudevice.DevicesInfo
	}{
		{
			name:          "Empty device list returns empty DevicesInfo",
			xpumDevices:   []*xpumapi.DeviceHealth{},
			ignoreWarning: true,
			expectDevices: gpudevice.DevicesInfo{},
		},
		{
			name: "Healthy device with no severity issues",
			xpumDevices: []*xpumapi.DeviceHealth{
				{
					Info: &xpumapi.DeviceInformation{
						Pci: &xpumapi.PciInfo{
							Bdf:      "0000:00:02.0",
							DeviceId: "56c0",
						},
						Model: "Intel Arc A770",
						Memory: []*xpumapi.MemoryInfo{
							{Size: uint64(16 * 1024 * 1024 * 1024)},
						},
					},
					Health: []*xpumapi.HealthStatus{
						{Name: "CoreThermal", Severity: xpumapi.SeverityLevel_SEVERITY_LEVEL_OK},
					},
				},
			},
			ignoreWarning: true,
			expectDevices: gpudevice.DevicesInfo{
				"0000-00-02-0-0x56c0": &gpudevice.DeviceInfo{
					UID:        "0000-00-02-0-0x56c0",
					PCIAddress: "0000:00:02.0",
					Model:      "0x56c0",
					ModelName:  "Intel Arc A770",
					MemoryMiB:  16384,
					Health:     "Healthy",
					HealthStatus: map[string]string{
						"CoreThermal": "Healthy",
					},
				},
			},
		},
		{
			name: "Device with WARNING severity unhealthy when ignoreWarning=false",
			xpumDevices: []*xpumapi.DeviceHealth{
				{
					Info: &xpumapi.DeviceInformation{
						Pci: &xpumapi.PciInfo{
							Bdf:      "0000:00:02.0",
							DeviceId: "0x56c0",
						},
						Model: "Intel Arc A770",
					},
					Health: []*xpumapi.HealthStatus{
						{Name: "CoreThermal", Severity: xpumapi.SeverityLevel_SEVERITY_LEVEL_WARNING},
					},
				},
			},
			ignoreWarning: false,
			expectDevices: gpudevice.DevicesInfo{
				"0000-00-02-0-0x56c0": &gpudevice.DeviceInfo{
					UID:        "0000-00-02-0-0x56c0",
					PCIAddress: "0000:00:02.0",
					Model:      "0x56c0",
					ModelName:  "Intel Arc A770",
					Health:     "Unhealthy",
					HealthStatus: map[string]string{
						"CoreThermal": "Unhealthy",
					},
				},
			},
		},
		{
			name: "Device with WARNING severity healthy when ignoreWarning=true",
			xpumDevices: []*xpumapi.DeviceHealth{
				{
					Info: &xpumapi.DeviceInformation{
						Pci: &xpumapi.PciInfo{
							Bdf:      "0000:00:02.0",
							DeviceId: "0x56c0",
						},
						Model: "Intel Arc A770",
					},
					Health: []*xpumapi.HealthStatus{
						{Name: "CoreThermal", Severity: xpumapi.SeverityLevel_SEVERITY_LEVEL_WARNING},
					},
				},
			},
			ignoreWarning: true,
			expectDevices: gpudevice.DevicesInfo{
				"0000-00-02-0-0x56c0": &gpudevice.DeviceInfo{
					UID:        "0000-00-02-0-0x56c0",
					PCIAddress: "0000:00:02.0",
					Model:      "0x56c0",
					ModelName:  "Intel Arc A770",
					Health:     "Healthy",
					HealthStatus: map[string]string{
						"CoreThermal": "Healthy",
					},
				},
			},
		},
		{
			name: "Device with CRITICAL severity always unhealthy",
			xpumDevices: []*xpumapi.DeviceHealth{
				{
					Info: &xpumapi.DeviceInformation{
						Pci: &xpumapi.PciInfo{
							Bdf:      "0000:00:02.0",
							DeviceId: "0x56c0",
						},
						Model: "Intel Arc A770",
					},
					Health: []*xpumapi.HealthStatus{
						{Name: "CoreThermal", Severity: xpumapi.SeverityLevel_SEVERITY_LEVEL_CRITICAL},
					},
				},
			},
			ignoreWarning: true,
			expectDevices: gpudevice.DevicesInfo{
				"0000-00-02-0-0x56c0": &gpudevice.DeviceInfo{
					UID:        "0000-00-02-0-0x56c0",
					PCIAddress: "0000:00:02.0",
					Model:      "0x56c0",
					ModelName:  "Intel Arc A770",
					Health:     "Unhealthy",
					HealthStatus: map[string]string{
						"CoreThermal": "Unhealthy",
					},
				},
			},
		},
		{
			name: "Device with multiple health metrics",
			xpumDevices: []*xpumapi.DeviceHealth{
				{
					Info: &xpumapi.DeviceInformation{
						Pci: &xpumapi.PciInfo{
							Bdf:      "0000:00:02.0",
							DeviceId: "0x56c0",
						},
						Model: "Intel Arc A770",
					},
					Health: []*xpumapi.HealthStatus{
						{Name: "CoreThermal", Severity: xpumapi.SeverityLevel_SEVERITY_LEVEL_OK},
						{Name: "Memory", Severity: xpumapi.SeverityLevel_SEVERITY_LEVEL_WARNING},
						{Name: "Power", Severity: xpumapi.SeverityLevel_SEVERITY_LEVEL_OK},
					},
				},
			},
			ignoreWarning: true,
			expectDevices: gpudevice.DevicesInfo{
				"0000-00-02-0-0x56c0": &gpudevice.DeviceInfo{
					UID:        "0000-00-02-0-0x56c0",
					PCIAddress: "0000:00:02.0",
					Model:      "0x56c0",
					ModelName:  "Intel Arc A770",
					Health:     "Healthy",
					HealthStatus: map[string]string{
						"CoreThermal": "Healthy",
						"Memory":      "Healthy",
						"Power":       "Healthy",
					},
				},
			},
		},
		{
			name: "Multiple devices processed correctly",
			xpumDevices: []*xpumapi.DeviceHealth{
				{
					Info: &xpumapi.DeviceInformation{
						Pci: &xpumapi.PciInfo{
							Bdf:      "0000:03:00.0",
							DeviceId: "0x56c0",
						},
						Model: "Intel Arc A770",
					},
					Health: []*xpumapi.HealthStatus{
						{Name: "CoreThermal", Severity: xpumapi.SeverityLevel_SEVERITY_LEVEL_OK},
						{Name: "Memory", Severity: xpumapi.SeverityLevel_SEVERITY_LEVEL_WARNING},
						{Name: "Power", Severity: xpumapi.SeverityLevel_SEVERITY_LEVEL_OK},
					},
				},
				{
					Info: &xpumapi.DeviceInformation{
						Pci: &xpumapi.PciInfo{
							Bdf:      "0000:05:00.0",
							DeviceId: "0x56c1",
						},
						Model: "Intel Arc A750",
					},
					Health: []*xpumapi.HealthStatus{
						{Name: "CoreThermal", Severity: xpumapi.SeverityLevel_SEVERITY_LEVEL_OK},
						{Name: "Memory", Severity: xpumapi.SeverityLevel_SEVERITY_LEVEL_OK},
						{Name: "Power", Severity: xpumapi.SeverityLevel_SEVERITY_LEVEL_CRITICAL},
					},
				},
			},
			ignoreWarning: true,
			expectDevices: gpudevice.DevicesInfo{
				"0000-03-00-0-0x56c0": &gpudevice.DeviceInfo{
					UID:        "0000-03-00-0-0x56c0",
					PCIAddress: "0000:03:00.0",
					Model:      "0x56c0",
					ModelName:  "Intel Arc A770",
					Health:     "Healthy",
					HealthStatus: map[string]string{
						"CoreThermal": "Healthy",
						"Memory":      "Healthy",
						"Power":       "Healthy",
					},
				},
				"0000-05-00-0-0x56c1": &gpudevice.DeviceInfo{
					UID:        "0000-05-00-0-0x56c1",
					PCIAddress: "0000:05:00.0",
					Model:      "0x56c1",
					ModelName:  "Intel Arc A750",
					Health:     "Unhealthy",
					HealthStatus: map[string]string{
						"CoreThermal": "Healthy",
						"Memory":      "Healthy",
						"Power":       "Unhealthy",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			devicesInfo := xpumDevicesToAllocatableDevicesInfo(tt.xpumDevices, tt.ignoreWarning)

			if len(devicesInfo) != len(tt.expectDevices) {
				t.Fatalf("expected %d devices, got %d", len(tt.expectDevices), len(devicesInfo))
			}

			for uid, expectedInfo := range tt.expectDevices {
				actualInfo, exists := devicesInfo[uid]
				if !exists {
					t.Fatalf("expected device with UID %s not found in result", uid)
				}
				if !reflect.DeepEqual(actualInfo, expectedInfo) {
					t.Fatalf("expected device info for UID %s:\n%+v\ngot:\n%+v", uid, expectedInfo, actualInfo)
				}
			}
		})
	}
}
