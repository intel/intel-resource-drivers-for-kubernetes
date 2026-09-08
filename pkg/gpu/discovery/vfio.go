//
// Copyright (C) 2026 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0
//

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
