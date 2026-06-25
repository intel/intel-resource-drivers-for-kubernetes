//
// Copyright (C) 2024-2026 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0
//

package device

import (
	"fmt"
	"path/filepath"
	"regexp"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/helpers"
)

var (
	PciRegexp          = regexp.MustCompile(`[0-9a-f]{4}:[0-9a-f]{2}:[0-9a-f]{2}\.[0-7]$`)
	AccelRegexp        = regexp.MustCompile(`^accel[0-9]+$`)
	AccelControlRegexp = regexp.MustCompile(`^accel_controlD[0-9]+$`)
)

const (
	DevfsAccelPath = "accel"
	// driver.sysfsDriverDir and driver.sysfsAccelDir are sysfsDriverPath and sysfsAccelPath
	// respectively prefixed with $SYSFS_ROOT.
	SysfsDriverPath     = "bus/pci/drivers/intel_vpu"
	SysfsAccelClassPath = "class/accel/"

	CDIVendor        = "intel.com"
	CDIClass         = "npu"
	CDIKind          = CDIVendor + "/" + CDIClass
	DriverName       = CDIClass + "." + CDIVendor
	PCIAddressLength = len("0000:00:00.0")

	PreparedClaimsFileName = "preparedClaims.json"

	DefaultNamingStyle = "machine"
	AccelDevicePattern = "accel[0-9]*"
)

// DeviceInfo is an internal structure type to store info about discovered device.
type DeviceInfo struct {
	// UID is a unique identifier on node, used in ResourceSlice K8s API object as RFC1123-compliant identifier.
	// Consists of PCIAddress and Model with colons and dots replaced with hyphens, e.g. 0000-01-02-0-0x1234.
	UID         string `json:"uid"`
	PCIAddress  string `json:"pciaddress"`  // PCI address in Linux DBDF notation for use with sysfs, e.g. 0000:00:00.0
	PCIDeviceId string `json:"pcideviceid"` // PCI device ID in Linux notation, e.g. 0x1234
	DeviceIdx   uint64 `json:"deviceidx"`   // accel device number (e.g. 0 for /dev/accel/accel0)
}

func (g DeviceInfo) CDIName() string {
	return fmt.Sprintf("%s=%s", CDIKind, g.UID)
}

func (g *DeviceInfo) DeepCopy() *DeviceInfo {
	di := *g
	return &di
}

// DevicesInfo is a dictionary with DeviceInfo.uid being the key.
type DevicesInfo map[string]*DeviceInfo

func (g *DevicesInfo) DeepCopy() DevicesInfo {
	devicesInfoCopy := DevicesInfo{}
	for duid, device := range *g {
		devicesInfoCopy[duid] = device.DeepCopy()
	}
	return devicesInfoCopy
}

func GetAccelDevfsPath() string {
	return filepath.Join(helpers.GetDevfsRoot(DevfsAccelPath), DevfsAccelPath)
}
