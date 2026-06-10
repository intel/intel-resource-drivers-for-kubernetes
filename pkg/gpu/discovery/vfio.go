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
	"regexp"

	"k8s.io/klog/v2"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/helpers"
)

var (
	VFIORegexp = regexp.MustCompile(`^vfio[0-9]+$`)
)

func GetIOMMUGroup(pciAddress string) (string, error) {
	iommuGroupPath := path.Join(helpers.GetSysfsRoot(device.SysfsPCIDevicesPath), device.SysfsPCIDevicesPath, pciAddress, "iommu_group")
	iommuGroupLink, err := os.Readlink(iommuGroupPath)
	if err != nil {
		return "", fmt.Errorf("failed to read IOMMU group link for device %s: %v", pciAddress, err)
	}

	iommuGroup := path.Base(iommuGroupLink)
	return iommuGroup, nil
}

func GetVFIODevice(pciAddress string) (string, error) {
	// read vfio-dev/vfioX device name
	vfioDeviceDir := path.Join(helpers.GetSysfsRoot(device.SysfsPCIDevicesPath), device.SysfsPCIDevicesPath, pciAddress, "vfio-dev")
	vfioDevices, err := os.ReadDir(vfioDeviceDir)
	if err != nil {
		return "", fmt.Errorf("cannot read device folder %v: %v", vfioDeviceDir, err)
	}

	foundDevice := ""
	foundDevices := 0
	for _, vfioDevice := range vfioDevices {
		vfioDeviceName := vfioDevice.Name()
		if VFIORegexp.MatchString(vfioDeviceName) {
			foundDevice = vfioDeviceName
			foundDevices++
			klog.V(5).Infof("Found VFIO device %s for PCI address %s", vfioDeviceName, pciAddress)
		}
	}

	if foundDevices != 1 {
		return "", fmt.Errorf("expected exactly one VFIO device for PCI address %s, found %d", pciAddress, foundDevices)
	}

	return foundDevice, nil
}
