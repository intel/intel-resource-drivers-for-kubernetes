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
	"time"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/helpers"
)

var (
	PciRegexp     = regexp.MustCompile(`[0-9a-f]{4}:[0-9a-f]{2}:[0-9a-f]{2}\.[0-7]$`)
	CardRegexp    = regexp.MustCompile(`^card[0-9]{1,3}$`)
	RenderdRegexp = regexp.MustCompile(`^renderD[0-9]{1,3}$`)
	MEIRegexp     = regexp.MustCompile(`^mei[0-9]+$`)
)

const (
	DevfsDriPath = "dri"

	// driver.sysfsI915Dir and driver.sysfsDRMDir are sysfsI915path and sysfsDRMpath
	// respectively prefixed with $SYSFS_ROOT.
	SysfsPCIDevicesPath   = "bus/pci/devices"
	SysfsPCIDriversPath   = "bus/pci/drivers"
	SysfsI915DriverName   = "i915"
	SysfsXeDriverName     = "xe"
	SysfsVFIODriverName   = "vfio-pci"
	SysfsXeVFIODriverName = "xe-vfio-pci"
	SysfsDRMpath          = "class/drm/"
	SysfsMEIpath          = "class/mei/"
	DevfsVFIOPath         = "vfio"

	CDIVendor   = "intel.com"
	CDIGPUClass = "gpu"
	CDIGPUKind  = CDIVendor + "/" + CDIGPUClass
	CDIClass    = CDIGPUClass
	CDIKind     = CDIGPUKind
	CDIMEIClass = "gpu-mei"
	CDIMEIKind  = CDIVendor + "/" + CDIMEIClass
	DriverName  = CDIGPUClass + "." + CDIVendor

	VFIODeviceClassName = "gpu-vfio" + "." + CDIVendor

	UIDLength = len("0000-00-00-0-0x0000")

	PreparedClaimsFileName = "preparedClaims.json"

	DefaultNamingStyle = "machine"
	GpuDeviceType      = "gpu"
	VfDeviceType       = "vf"

	DriverChangeDelay = 500 * time.Millisecond

	HealthUnknown                = "Unknown"
	HealthHealthy                = "Healthy"
	HealthUnhealthy              = "Unhealthy"
	HealthStatusDeviceAbsent     = "DeviceAbsent"
	HealthStatusUnexpectedDriver = "UnexpectedDriver"

	PCIVendorId       = "0x8086"
	PCIVendorIdDec    = "8086"
	PCIVGAClassID     = "0x030000"
	PCIDisplayClassID = "0x038000"
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

// DeviceInfo is an internal structure type to store info about discovered device.
type DeviceInfo struct {
	// UID is a unique identifier on node, used in ResourceSlice K8s API object as RFC1123-compliant identifier.
	// Consists of PCIAddress and Model with colons and dots replaced with hyphens, e.g. 0000-01-02-0-0x1234.
	UID           string            `json:"uid"`
	PCIAddress    string            `json:"pciaddress"`    // PCI address in Linux DBDF notation for use with sysfs, e.g. 0000:00:00.0
	Model         string            `json:"model"`         // PCI device ID
	ModelName     string            `json:"modelname"`     // SKU name, usually Series + Model, e.g. Flex 140
	FamilyName    string            `json:"familyname"`    // SKU family name, usually Series, e.g. Flex or Max
	MEIName       string            `json:"meiname"`       // MEI name discovered for this GPU, e.g. mei0 for /dev/mei0
	CardName      string            `json:"cardname"`      // card device name (e.g. card0 for /dev/dri/card0)
	RenderDName   string            `json:"renderdname"`   // renderD device name (e.g. renderD128 for /dev/dri/renderD128)
	MemoryMiB     uint64            `json:"memorymib"`     // in MiB
	Millicores    uint64            `json:"millicores"`    // [0-1000] where 1000 means whole GPU.
	DeviceType    string            `json:"devicetype"`    // gpu, vf
	MaxVFs        uint64            `json:"maxvfs"`        // if enabled, non-zero maximum amount of VFs
	ParentUID     string            `json:"parentuid"`     // uid of gpu device where VF is
	VFProfile     string            `json:"vfprofile"`     // name of the SR-IOV profile
	VFIndex       uint64            `json:"vfindex"`       // 0-based PCI index of the VF on the GPU, DRM indexing starts with 1
	Provisioned   bool              `json:"provisioned"`   // true if the SR-IOV VF is configured and enabled
	Driver        string            `json:"driver"`        // i915 | xe
	CurrentDriver string            `json:"currentdriver"` // Current bound driver: xe, i915, vfio-pci, xe-vfio-pci, or empty if unbound
	PCIRoot       string            `json:"pciroot"`       // PCI Root of the device
	Health        string            `json:"health"`        // Overall health status of the device. One of: Unknown, Healthy, Unhealthy.
	HealthStatus  map[string]string `json:"healthstatus"`  // Detailed per-category health status information
	VFIODevice    string            `json:"vfiodevice"`    // VFIO device name, e.g. vfio0
	IOMMUGroup    string            `json:"iommugroup"`    // IOMMU group of the device, e.g. 12
}

func (g DeviceInfo) CDIName() string {
	return fmt.Sprintf("%s=%s", CDIKind, g.UID)
}

func (g DeviceInfo) MEICDIName() string {
	if g.MEIName == "" {
		return ""
	}

	return fmt.Sprintf("%s=%s", CDIMEIKind, g.MEIName)
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

// IsDRMBound checks if the device is currently bound to its a DRM driver.
func (g *DeviceInfo) IsDRMBound() bool {
	return g.CurrentDriver == SysfsI915DriverName || g.CurrentDriver == SysfsXeDriverName
}

// IsVFIOBound checks if the device is currently bound to any VFIO kernel driver.
func (g *DeviceInfo) IsVFIOBound() bool {
	return g.CurrentDriver == SysfsVFIODriverName || g.CurrentDriver == SysfsXeVFIODriverName
}

// DevicesInfo is a dictionary with DeviceInfo.uid being the key.
type DevicesInfo map[string]*DeviceInfo

func (g DevicesInfo) GetDeviceByCDIName(cdiName string) *DeviceInfo {
	for _, device := range g {
		if device.CDIName() == cdiName {
			return device
		}
	}
	return nil
}

func (g *DevicesInfo) DeepCopy() DevicesInfo {
	devicesInfoCopy := DevicesInfo{}
	for duid, device := range *g {
		devicesInfoCopy[duid] = device.DeepCopy()
	}
	return devicesInfoCopy
}

func GetDriDevPath() string {
	return filepath.Join(helpers.GetDevfsRoot(helpers.DevfsEnvVarName, DevfsDriPath), DevfsDriPath)
}

func IsGPUClass(classId string) bool {
	return classId == PCIVGAClassID || classId == PCIDisplayClassID
}
