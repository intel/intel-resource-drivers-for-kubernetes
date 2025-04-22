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
	ModelNames         = map[string]string{
		"0x1000": "Gaudi",
		"0x1010": "Gaudi",
		"0x1001": "Gaudi",
		"0x1011": "Gaudi",
		"0x1020": "Gaudi2",
		"0x1030": "Gaudi3",
		"0x1060": "Gaudi3",
		"0x1061": "Gaudi3",
		"0x1062": "Gaudi3",
	}
)

const (
	DevfsAccelPath = "accel"

	// driver.sysfsDriverDir and driver.sysfsAccelDir are sysfsDriverPath and sysfsAccelPath
	// respectively prefixed with $SYSFS_ROOT.
	SysfsDriverPath = "bus/pci/drivers/habanalabs"
	SysfsAccelPath  = "devices/virtual/accel/"

	CDIVendor        = "intel.com"
	CDIClass         = "gaudi"
	CDIKind          = CDIVendor + "/" + CDIClass
	DriverName       = CDIClass + "." + CDIVendor
	PCIAddressLength = len("0000:00:00.0")

	PreparedClaimsFileName  = "preparedClaims.json"
	PluginRegistrarFileName = DriverName + ".sock"
	PluginSocketFileName    = "plugin.sock"

	DefaultNamingStyle       = "machine"
	VisibleDevicesEnvVarName = "HABANA_VISIBLE_DEVICES"
)

// DeviceInfo is an internal structure type to store info about discovered device.
type DeviceInfo struct {
	// UID is a unique identifier on node, used in ResourceSlice K8s API object as RFC1123-compliant identifier.
	// Consists of PCIAddress and Model with colons and dots replaced with hyphens, e.g. 0000-01-02-0-0x1234.
	UID        string `json:"uid"`
	PCIAddress string `json:"pciaddress"` // PCI address in Linux DBDF notation for use with sysfs, e.g. 0000:00:00.0
	Model      string `json:"model"`      // PCI device ID
	ModelName  string `json:"modelname"`  // SKU name of the device, e.g. Gaudi2
	DeviceIdx  uint64 `json:"deviceidx"`  // accel device number (e.g. 0 for /dev/accel/accel0)
	ModuleIdx  uint64 `json:"moduleidx"`  // OAM slot number, needed for Habana Runtime to set networking
	PCIRoot    string `json:"pciroot"`    // PCI Root complex ID
	Serial     string `json:"serial"`     // Serial number obtained through HLML library
	Healthy    bool   `json:"healthy"`    // True if device is usable, false otherwise
}

func (g DeviceInfo) CDIName() string {
	return fmt.Sprintf("%s=%s", CDIKind, g.UID)
}

func (g *DeviceInfo) DeepCopy() *DeviceInfo {
	di := *g
	return &di
}

func (g *DeviceInfo) SetModelName() {
	if modelName, found := ModelNames[g.Model]; found {
		g.ModelName = modelName
		return
	}
	g.ModelName = "Unknown"
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
	return filepath.Join(helpers.GetDevRoot(helpers.DevfsEnvVarName, DevfsAccelPath), DevfsAccelPath)
}
