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
	PciRegexp     = regexp.MustCompile(`[0-9a-f]{4}:[0-9a-f]{2}:[0-9a-f]{2}\.[0-7]$`)
	CardRegexp    = regexp.MustCompile(`^card[0-9]+$`)
	RenderdRegexp = regexp.MustCompile(`^renderD[0-9]+$`)
)

const (
	DevfsDriPath = "dri"

	// driver.sysfsI915Dir and driver.sysfsDRMDir are sysfsI915path and sysfsDRMpath
	// respectively prefixed with $SYSFS_ROOT.
	SysfsI915path = "bus/pci/drivers/i915"
	SysfsDRMpath  = "class/drm/"

	CDIVendor  = "intel.com"
	CDIClass   = "gpu"
	CDIKind    = CDIVendor + "/" + CDIClass
	DriverName = CDIClass + "." + CDIVendor

	UIDLength = len("0000-00-00-0-0x0000")

	PreparedClaimsFileName  = "preparedClaims.json"
	PluginRegistrarFileName = DriverName + ".sock"
	PluginSocketFileName    = "plugin.sock"

	DefaultNamingStyle = "machine"
	GpuDeviceType      = "gpu"
	VfDeviceType       = "vf"
)

// VfAttributeFiles is a list of filenames that needs to be configured for a VF
// profile to be applied.
var VfAttributeFiles = []string{
	"contexts_quota",
	"doorbells_quota",
	"exec_quantum_ms",
	"ggtt_quota",
	"lmem_quota",
	"preempt_timeout_us",
}

var ModelDetails = map[string]map[string]string{
	"0x56a0": {
		"model":  "A770",
		"family": "Arc",
	},
	"0x56a1": {
		"model":  "A750",
		"family": "Arc",
	},
	"0x56a2": {
		"model":  "A580",
		"family": "Arc",
	},
	"0x56b1": {
		"model":  "A40/A50",
		"family": "Arc Pro",
	},
	"0x56c0": {
		"model":  "Flex 170",
		"family": "Data Center Flex",
	},
	"0x56c1": {
		"model":  "Flex 140",
		"family": "Data Center Flex",
	},
	"0x0b69": {
		"model":  "Max 1550",
		"family": "Data Center Max",
	},
	"0x0bd0": {
		"model":  "Max 1550",
		"family": "Data Center Max",
	},
	"0x0bd5": {
		"model":  "Max 1550",
		"family": "Data Center Max",
	},
	"0x0bd6": {
		"model":  "Max 1450",
		"family": "Data Center Max",
	},
	"0x0bd9": {
		"model":  "Max 1100",
		"family": "Data Center Max",
	},
	"0x0bda": {
		"model":  "Max 1100",
		"family": "Data Center Max",
	},
	"0x0bdb": {
		"model":  "Max 1100",
		"family": "Data Center Max",
	},
	"0xa7a0": {
		"model":  "Raptor Lake-P",
		"family": "Iris Xe",
	},
}

// DeviceInfo is an internal structure type to store info about discovered device.
type DeviceInfo struct {
	// UID is a unique identifier on node, used in ResourceSlice K8s API object as RFC1123-compliant identifier.
	// Consists of PCIAddress and Model with colons and dots replaced with hyphens, e.g. 0000-01-02-0-0x12345.
	UID         string `json:"uid"`
	PCIAddress  string `json:"pciaddress"`  // PCI address in Linux DBDF notation for use with sysfs, e.g. 0000:00:00.0
	Model       string `json:"model"`       // PCI device ID
	ModelName   string `json:"modelname"`   // SKU name, usually Series + Model, e.g. Flex 140
	FamilyName  string `json:"familyname"`  // SKU family name, usually Series, e.g. Flex or Max
	CardIdx     uint64 `json:"cardidx"`     // card device number (e.g. 0 for /dev/dri/card0)
	RenderdIdx  uint64 `json:"renderdidx"`  // renderD device number (e.g. 128 for /dev/dri/renderD128)
	MemoryMiB   uint64 `json:"memorymib"`   // in MiB
	Millicores  uint64 `json:"millicores"`  // [0-1000] where 1000 means whole GPU.
	DeviceType  string `json:"devicetype"`  // gpu, vf, any
	MaxVFs      uint64 `json:"maxvfs"`      // if enabled, non-zero maximum amount of VFs
	ParentUID   string `json:"parentuid"`   // uid of gpu device where VF is
	VFProfile   string `json:"vfprofile"`   // name of the SR-IOV profile
	VFIndex     uint64 `json:"vfindex"`     // 0-based PCI index of the VF on the GPU, DRM indexing starts with 1
	Provisioned bool   `json:"provisioned"` // true if the SR-IOV VF is configured and enabled
}

func (g DeviceInfo) CDIName() string {
	return fmt.Sprintf("%s=%s", CDIKind, g.UID)
}

func (g *DeviceInfo) DeepCopy() *DeviceInfo {
	di := *g
	return &di
}

func (g *DeviceInfo) DrmVFIndex() uint64 {
	return g.VFIndex + 1
}

func (g *DeviceInfo) SriovEnabled() bool {
	return g.MaxVFs != 0
}

func (g *DeviceInfo) ParentPCIAddress() string {
	pciAddress, _ := helpers.PciInfoFromDeviceUID(g.ParentUID)
	return pciAddress
}

func (g *DeviceInfo) SetModelInfo() {
	if deviceDetails, found := ModelDetails[g.Model]; found {
		g.ModelName = deviceDetails["model"]
		g.FamilyName = deviceDetails["family"]

		return
	}

	g.ModelName = "Unknown"
	g.FamilyName = "Unknown"
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

func GetDriDevPath() string {
	return filepath.Join(helpers.GetDevRoot(helpers.DevfsEnvVarName, DevfsDriPath), DevfsDriPath)
}
