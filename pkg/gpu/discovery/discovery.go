//
// Copyright (C) 2022-2026 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0
//

package discovery

import (
	"fmt"
	"path"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/helpers"

	"k8s.io/klog/v2"
)

const (
	initialMillicores = 1000
)

// DiscoverDevices detects devices from sysfs and devfs if it can, and returns a map of
// device UID:deviceInfo and a bool indicating if device details were successfully discovered.
// When DRA driver runs in privileged mode, device details are fetched from devfs. Otherwise the
// xpumd device info stream will be used to get device details including health and memory when
// xpumd starts later.
func DiscoverDevices(sysfsRoot, namingStyle string, xpumdEnabled bool) map[string]*device.DeviceInfo {
	devices := processSysfsPCIDevices(sysfsRoot, namingStyle)

	// Try querying memory from the devices in case we run in privileged mode.
	if err := populateDevicesInfoMemory(devices); err != nil && !xpumdEnabled {
		klog.Error("Could not get device details. Enable privileged mode or health monitoring for device capability discovery.")
	}

	return devices
}

// populateDevicesInfoMemory tries to query amount of memory from DRM devices /dev/cardX, and returns
// error as soon as any request fails, or nil otherwise. When DRA driver runs in privileged mode,
// this should succeed.
func populateDevicesInfoMemory(devices map[string]*device.DeviceInfo) error {
	for _, deviceInfo := range devices {
		memoryMiB, err := getLocalMemoryAmountMiB(deviceInfo.CardName, deviceInfo.Driver)
		if err != nil {
			return err
		}
		deviceInfo.MemoryMiB = memoryMiB
	}

	return nil
}

func DetermineDeviceName(info *device.DeviceInfo, namingStyle string) string {
	if namingStyle == "classic" {
		// TODO: FIXME: in survivability mode there is no DRM device even though driver is DRM.
		// When survivability mode field is added to DeviceInfo, use PCI address as a name.
		if info.CurrentDriver == device.SysfsI915DriverName || info.CurrentDriver == device.SysfsXeDriverName {
			return info.CardName
		}

		if info.CurrentDriver == device.SysfsVFIODriverName || info.CurrentDriver == device.SysfsXeVFIODriverName {
			return info.VFIODevice
		}
	}

	return info.UID
}

// Return the amount of local memory the GPU has in MiB.
func getLocalMemoryAmountMiB(cardName string, driver string) (uint64, error) {
	if cardName == "" { // Ignore non-DRM bound devices.
		return 0, nil
	}

	klog.V(5).Infof("Getting local memory for %s with driver %v", cardName, driver)
	switch driver {
	case device.SysfsXeDriverName:
		return GetXeDeviceMemoryMiB(path.Join(helpers.GetDevfsRoot(device.DevfsDriPath), device.DevfsDriPath, cardName))
	case device.SysfsI915DriverName:
		return GetI915DeviceMemoryMiB(path.Join(helpers.GetDevfsRoot(device.DevfsDriPath), device.DevfsDriPath, cardName))
	}

	return 0, fmt.Errorf("unknown DRM driver %v (device %v), cannot query local memory", driver, cardName)
}
