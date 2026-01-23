/*
 * Copyright (c) 2024, Intel Corporation.  All Rights Reserved.
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

package discovery

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gaudi/device"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/helpers"

	"k8s.io/klog/v2"
)

// Detect devices from sysfs.
func DiscoverDevices(sysfsDir, namingStyle string) map[string]*device.DeviceInfo {

	sysfsDriverDir := path.Join(sysfsDir, device.SysfsDriverPath)

	devices := make(map[string]*device.DeviceInfo)

	driverDirFiles, err := os.ReadDir(sysfsDriverDir)
	if err != nil {
		if os.IsNotExist(err) {
			klog.V(5).Infof("No Intel Gaudi devices found on this host. %v does not exist", sysfsDriverDir)
			return devices
		}
		klog.Errorf("could not read sysfs directory %v: %v", driverDirFiles, err)
		return devices
	}

	return scanDevicesFromDriverDirFiles(driverDirFiles, sysfsDriverDir, namingStyle)

}

func scanDevicesFromDriverDirFiles(driverDirFiles []os.DirEntry, sysfsDriverDir string, namingStyle string) map[string]*device.DeviceInfo {
	devices := map[string]*device.DeviceInfo{}
	for _, pciAddress := range driverDirFiles {
		devicePCIAddress := pciAddress.Name()
		// check if file is PCI device
		if !device.PciRegexp.MatchString(devicePCIAddress) {
			continue
		}
		klog.V(5).Infof("Found Gaudi PCI device: %s", devicePCIAddress)

		driverDeviceDir := path.Join(sysfsDriverDir, devicePCIAddress)
		// Read PCI device ID.
		deviceIdFile := path.Join(driverDeviceDir, "device")
		deviceIdBytes, err := os.ReadFile(deviceIdFile)
		if err != nil {
			klog.Errorf("failed detecting device %v PCI ID: %+v", devicePCIAddress, err)
			continue
		}
		deviceId := strings.TrimSpace(string(deviceIdBytes))

		deviceIdx, err := getAccelIndex(path.Join(driverDeviceDir, "accel"))
		if err != nil {
			klog.Errorf("failed detecting device %v accel index: %v", devicePCIAddress, err)
			continue
		}

		moduleIdx, err := getModuleId(driverDeviceDir)
		if err != nil {
			klog.Errorf("failed detecting device %v module index: %v", devicePCIAddress, err)
			continue
		}

		uverbsIdx, err := getUverbsId(driverDeviceDir)
		if err != nil {
			klog.Warningf("could not detect device %v InfiniBand index: %v", devicePCIAddress, err)
			uverbsIdx = device.UverbsMissingIdx
		}

		uid := helpers.DeviceUIDFromPCIinfo(devicePCIAddress, deviceId)
		klog.V(5).Infof("New gaudi UID: %v", uid)
		newDeviceInfo := &device.DeviceInfo{
			UID:        uid,
			PCIAddress: devicePCIAddress,
			Model:      deviceId,
			DeviceIdx:  deviceIdx,
			ModuleIdx:  moduleIdx,
			UVerbsIdx:  uverbsIdx,
		}

		linkSource := path.Join(sysfsDriverDir, devicePCIAddress)
		pciRoot, err := helpers.DeterminePCIRoot(linkSource)
		if err != nil {
			klog.Warningf("could not detect PCI root complex for %v: %v", devicePCIAddress, err)
		} else {
			newDeviceInfo.PCIRoot = pciRoot
		}

		// Set user-friendly ModelName field.
		newDeviceInfo.SetModelName()

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

func getAccelIndex(accelDir string) (uint64, error) {
	matches, _ := filepath.Glob(path.Join(accelDir, device.AccelDevicePattern))
	if len(matches) != 1 {
		return 0, fmt.Errorf("could not find matching accel device file")
	}

	accelFileName := filepath.Base(matches[0])
	deviceIdx, err := strconv.ParseUint(accelFileName[5:], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to convert device %v accel index to a number: %v", accelFileName, err)
	}

	return deviceIdx, nil
}

func getModuleId(driverDeviceDir string) (uint64, error) {
	// Module index is an OAM slot number.
	moduleIdFile := path.Join(driverDeviceDir, "module_id")
	moduleIdBytes, err := os.ReadFile(moduleIdFile)
	if err != nil {
		return 0, fmt.Errorf("failed to read device module_id file %s: %+v", moduleIdFile, err)
	}

	moduleIdx, err := strconv.ParseUint(strings.TrimSpace(string(moduleIdBytes)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to convert device module_id %v (%v) to a number: %v", moduleIdFile, moduleIdBytes, err)
	}

	return moduleIdx, nil
}

func getUverbsId(driverDeviceDir string) (uint64, error) {
	targetPath := path.Join(driverDeviceDir, device.InfinibandVerbsDirName, device.InfinibandVerbsPattern)
	matches, _ := filepath.Glob(targetPath)
	if len(matches) != 1 {
		return 0, fmt.Errorf("could not find matching InfiniBand device file in %s. Found: %d", targetPath, len(matches))
	}

	uverbsFileName := filepath.Base(matches[0])
	uverbsIdx, err := strconv.ParseUint(uverbsFileName[6:], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to convert InfiniBand device %v uverbs index to a number: %v", uverbsFileName, err)
	}

	klog.V(5).Infof("found InfiniBand link %v", uverbsIdx)
	return uverbsIdx, nil
}
