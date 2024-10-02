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
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gaudi/device"

	"k8s.io/klog/v2"
)

type gaudiIndexesType struct {
	accelIdx  uint64 // /dev/accel/accelX
	moduleIdx uint64 // OAM slot number for networking logic
}

// Detect devices from sysfs.
func DiscoverDevices(sysfsDir, namingStyle string) map[string]*device.DeviceInfo {

	sysfsDriverDir := path.Join(sysfsDir, device.SysfsDriverPath)
	sysfsAccelDir := path.Join(sysfsDir, device.SysfsAccelPath)

	devices := make(map[string]*device.DeviceInfo)

	driverDirFiles, err := os.ReadDir(sysfsDriverDir)
	if err != nil {
		if err == os.ErrNotExist {
			klog.V(5).Infof("No Intel Gaudi devices found on this host. %v does not exist", sysfsDriverDir)
			return devices
		}
		klog.Errorf("could not read sysfs directory %v: %v", sysfsAccelDir, err)
		return devices
	}

	deviceIndexes := getAccelIndexes(sysfsAccelDir)

	for _, pciAddress := range driverDirFiles {
		devicePCIAddress := pciAddress.Name()
		// check if file is PCI device
		if !device.PciRegexp.MatchString(devicePCIAddress) {
			continue
		}
		klog.V(5).Infof("Found Gaudi PCI device: %s", devicePCIAddress)

		deviceIdFile := path.Join(sysfsDriverDir, devicePCIAddress, "device")
		deviceIdBytes, err := os.ReadFile(deviceIdFile)
		if err != nil {
			klog.Errorf("Failed reading device file (%s): %+v", deviceIdFile, err)
			continue
		}
		deviceId := strings.TrimSpace(string(deviceIdBytes))
		uid := device.DeviceUIDFromPCIinfo(devicePCIAddress, deviceId)
		klog.V(5).Infof("New gaudi UID: %v", uid)
		newDeviceInfo := &device.DeviceInfo{
			UID:        uid,
			PCIAddress: devicePCIAddress,
			Model:      deviceId,
			DeviceIdx:  0,
		}
		newDeviceInfo.SetModelName()

		deviceIdx, found := deviceIndexes[devicePCIAddress]
		if !found {
			klog.V(5).Infof("Could not find device %v Accel index", devicePCIAddress)
			continue
		}

		newDeviceInfo.DeviceIdx = deviceIdx.accelIdx
		newDeviceInfo.ModuleIdx = deviceIdx.moduleIdx
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

func getAccelIndexes(sysfsAccelDir string) map[string]gaudiIndexesType {
	devices := map[string]gaudiIndexesType{}
	accelDirFiles, err := os.ReadDir(sysfsAccelDir)
	if err != nil {
		if err == os.ErrNotExist {
			klog.V(5).Infof("No Accel devices found on this host. %v does not exist", sysfsAccelDir)
			return devices
		}
		klog.Errorf("could not read sysfs directory %v: %v", sysfsAccelDir, err)
		return devices
	}

	for _, accelFile := range accelDirFiles {
		accelFileName := accelFile.Name()
		if device.AccelRegexp.MatchString(accelFileName) {
			indexes := gaudiIndexesType{}

			// accelX
			deviceIdx, err := strconv.ParseUint(accelFileName[5:], 10, 64)
			if err != nil {
				klog.V(5).Infof("failed to parse index of Accel device '%v', skipping", accelFileName)
				continue
			}
			indexes.accelIdx = deviceIdx

			// Module index is an OAM slot number.
			moduleIdFile := path.Join(sysfsAccelDir, accelFileName, "device/module_id")
			moduleIdBytes, err := os.ReadFile(moduleIdFile)
			if err != nil {
				klog.Errorf("failed reading device module_id file (%s): %+v", moduleIdFile, err)
				continue
			}

			moduleIdx, err := strconv.ParseUint(strings.TrimSpace(string(moduleIdBytes)), 10, 64)
			if err != nil {
				klog.V(5).Infof("failed to parse module index of Accel device '%v', skipping", accelFileName)
				continue
			}
			indexes.moduleIdx = moduleIdx

			// read PCI address
			pciAddrFilePath := path.Join(sysfsAccelDir, accelFileName, "device/pci_addr")
			pciAddrBytes, err := os.ReadFile(pciAddrFilePath)
			if err != nil {
				klog.Errorf("failed reading device PCI address file (%s): %+v", pciAddrFilePath, err)
				continue
			}
			pciAddr := strings.TrimSpace(string(pciAddrBytes))
			devices[pciAddr] = indexes
		}
	}

	return devices
}
