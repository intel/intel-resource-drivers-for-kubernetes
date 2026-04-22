/*
 * Copyright (c) 2026, Intel Corporation.  All Rights Reserved.
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
	"strconv"
	"strings"

	"k8s.io/klog/v2"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gaudi/device"
)

const (
	SysfsAccelPath = "devices/virtual/accel/"
)

type gaudiIndexesType struct {
	accelIdx  uint64 // /dev/accel/accelX
	moduleIdx uint64 // OAM slot number for networking logic
}

// fallbackGetAccelIndexes is called when /sys/bus/pci/drivers/habanalabs/<pci-addr>/accel
// is absent, older kernels didn't populate this directory.
func fallbackGetAccelIndex(sysfsRoot, pciAddress string) (uint64, error) {
	indexMap, err := populateIndexMap(sysfsRoot)
	if err != nil {
		return 0, err
	}

	if indexes, ok := indexMap[pciAddress]; ok {
		return indexes.accelIdx, nil
	}

	return 0, fmt.Errorf("could not find accel index for PCI address %s", pciAddress)
}

// fallbackGetModuleIndexes is called when /sys/bus/pci/drivers/habanalabs/<pci-addr>/accel
// is absent, older kernels didn't populate this directory.
func fallbackGetModuleIndex(sysfsRoot, pciAddress string) (uint64, error) {
	indexMap, err := populateIndexMap(sysfsRoot)
	if err != nil {
		return 0, err
	}

	if indexes, ok := indexMap[pciAddress]; ok {
		return indexes.moduleIdx, nil
	}

	return 0, fmt.Errorf("could not find module index for PCI address %s", pciAddress)
}

func populateIndexMap(sysfsRoot string) (map[string]gaudiIndexesType, error) {
	devices := map[string]gaudiIndexesType{}
	sysfsAccelDir := path.Join(sysfsRoot, SysfsAccelPath)
	accelDirFiles, err := os.ReadDir(sysfsAccelDir)

	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no Accel devices found on this host. %v does not exist", sysfsAccelDir)
		}
		return nil, fmt.Errorf("could not read sysfs directory %v: %v", sysfsAccelDir, err)
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
	return devices, nil
}
