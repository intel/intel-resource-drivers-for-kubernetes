//
// Copyright (C) 2024-2026 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0
//

package discovery

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"k8s.io/klog/v2"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/helpers"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/npu/device"
)

// Detect devices from sysfs.
func DiscoverDevices(sysfsRoot, namingStyle string) map[string]*device.DeviceInfo {

	sysfsDriverDir := path.Join(sysfsRoot, device.SysfsDriverPath)

	devices := make(map[string]*device.DeviceInfo)

	driverDirFiles, err := os.ReadDir(sysfsDriverDir)
	if err != nil {
		if os.IsNotExist(err) {
			klog.V(5).Infof("No Intel NPU devices found on this host. %v does not exist", sysfsDriverDir)
			return devices
		}
		klog.Errorf("could not read sysfs directory %v: %v", sysfsDriverDir, err)
		return devices
	}

	return scanDevicesFromDriverDirFiles(driverDirFiles, sysfsDriverDir, namingStyle)

}

func scanDevicesFromDriverDirFiles(driverDirFiles []os.DirEntry, sysfsDriverDir, namingStyle string) map[string]*device.DeviceInfo {
	devices := map[string]*device.DeviceInfo{}
	for _, pciAddress := range driverDirFiles {
		devicePCIAddress := pciAddress.Name()
		// check if file is PCI device
		if !device.PciRegexp.MatchString(devicePCIAddress) {
			continue
		}
		klog.V(5).Infof("Found Intel NPU PCI device: %s", devicePCIAddress)

		driverDeviceDir := path.Join(sysfsDriverDir, devicePCIAddress)
		// Read PCI device ID.
		deviceIdFile := path.Join(driverDeviceDir, "device")
		deviceIdBytes, err := os.ReadFile(deviceIdFile)
		if err != nil {
			klog.Errorf("failed detecting device %v PCI ID: %+v", devicePCIAddress, err)
			continue
		}
		deviceId := strings.TrimSpace(string(deviceIdBytes))

		deviceIdx, err := getAccelIndex(driverDeviceDir)
		if err != nil {
			klog.Errorf("failed detecting device %v accel index: %v", devicePCIAddress, err)
			continue
		}

		uid := helpers.DeviceUIDFromPCIinfo(devicePCIAddress, deviceId)
		klog.V(5).Infof("New NPU UID: %v", uid)
		newDeviceInfo := &device.DeviceInfo{
			UID:         uid,
			PCIAddress:  devicePCIAddress,
			PCIDeviceId: deviceId,
			DeviceIdx:   deviceIdx,
		}

		devices[determineDeviceName(newDeviceInfo, namingStyle)] = newDeviceInfo
	}

	return devices
}

func determineDeviceName(info *device.DeviceInfo, namingStyle string) string {
	if namingStyle == "classic" {
		return "accel" + strconv.FormatUint(info.DeviceIdx, 10)
	}

	return info.UID
}

func getAccelIndex(driverDeviceDir string) (uint64, error) {
	accelDir := path.Join(driverDeviceDir, "accel")
	matches, _ := filepath.Glob(path.Join(accelDir, device.AccelDevicePattern))
	if len(matches) != 1 {
		return 0, fmt.Errorf("could not find matching accel device dir in %v, falling back to virtual devices scanning", driverDeviceDir)
	}

	accelFileName := filepath.Base(matches[0])
	deviceIdx, err := strconv.ParseUint(accelFileName[5:], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to convert device %v accel index to a number: %v", accelFileName, err)
	}

	return deviceIdx, nil
}
