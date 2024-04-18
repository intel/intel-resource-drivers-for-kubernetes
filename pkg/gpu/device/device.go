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
	"os"
	"path"
	"regexp"
	"strings"
)

var (
	PciRegexp     = regexp.MustCompile(`[0-9a-f]{4}:[0-9a-f]{2}:[0-9a-f]{2}\.[0-7]$`)
	CardRegexp    = regexp.MustCompile(`^card[0-9]+$`)
	RenderdRegexp = regexp.MustCompile(`^renderD[0-9]+$`)
)

const (
	DevDriEnvVarName = "DEV_DRI_PATH"
	SysfsEnvVarName  = "SYSFS_ROOT"
	// driver.sysfsI915Dir and driver.sysfsDRMDir are sysfsI915path and sysfsDRMpath
	// respectively prefixed with $SYSFS_ROOT.
	SysfsI915path          = "bus/pci/drivers/i915"
	SysfsDRMpath           = "class/drm/"
	CDIRoot                = "/etc/cdi"
	CDIVendor              = "intel.com"
	CDIKind                = CDIVendor + "/gpu"
	PCIAddressLength       = len("0000:00:00.0")
	PreparedClaimsFileName = "preparedClaims.json"
)

var SRIOVDeviceToModelMap = map[string]string{
	"0x56c0": "flex170",
	"0x56c1": "flex140",
	"0x0b69": "max1550",
	"0x0bd0": "max1550",
	"0x0bd5": "max1550",
	"0x0bd6": "max1450",
	"0x0bd9": "max1100",
	"0x0bda": "max1100",
	"0x0bdb": "max1100",
}

// DeviceInfo is an internal structure type to store info about discovered device.
type DeviceInfo struct {
	// UID is a unique identifier on node, used in ResourceSlice K8s API object as RFC1123-compliant identifier.
	// Consists of PCIAddress and Model with colons and dots replaced with hyphens, e.g. 0000-01-02-0-0x12345.
	UID        string `json:"uid"`
	PCIAddress string `json:"pciaddress"` // PCI address in Linux DBDF notation for use with sysfs, e.g. 0000:00:00.0
	Model      string `json:"model"`      // PCI device ID
	CardIdx    uint64 `json:"cardidx"`    // card device number (e.g. 0 for /dev/dri/card0)
	RenderdIdx uint64 `json:"renderdidx"` // renderD device number (e.g. 128 for /dev/dri/renderD128)
	MemoryMiB  uint64 `json:"memorymib"`  // in MiB
	Millicores uint64 `json:"millicores"` // [0-1000] where 1000 means whole GPU.
	DeviceType string `json:"devicetype"` // gpu, vf, any
	MaxVFs     uint64 `json:"maxvfs"`     // if enabled, non-zero maximum amount of VFs
	ParentUID  string `json:"parentuid"`  // uid of gpu device where VF is
	VFProfile  string `json:"vfprofile"`  // name of the SR-IOV profile
	VFIndex    uint64 `json:"vfindex"`    // 0-based PCI index of the VF on the GPU, DRM indexing starts with 1
	EccOn      bool   `json:"eccon"`      // true of ECC is enabled, false otherwise
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
	pciAddress, _ := PciInfoFromDeviceUID(g.ParentUID)
	return pciAddress
}

func (g *DeviceInfo) ModelName() string {
	if modelName, found := SRIOVDeviceToModelMap[g.Model]; found {
		return modelName
	}
	return "Unknown"
}

// DevicesInfo is a dictionary with DeviceInfo.uid being the key.
type DevicesInfo map[string]*DeviceInfo

func DeviceUIDFromPCIinfo(pciAddress string, pciid string) string {
	// 0000:00:01.0, 0x0000 -> 0000-00-01-0-0x0000
	// Replace colons and the dot in PCI address with hyphens.
	rfc1123PCIaddress := strings.ReplaceAll(strings.ReplaceAll(pciAddress, ":", "-"), ".", "-")
	newUID := fmt.Sprintf("%v-%v", rfc1123PCIaddress, pciid)

	return newUID
}

func PciInfoFromDeviceUID(deviceUID string) (string, string) {
	// 0000-00-01-0-0x0000 -> 0000:00:01.0, 0x0000
	rfc1123PCIaddress := deviceUID[:PCIAddressLength]
	pciAddress := strings.Replace(strings.Replace(rfc1123PCIaddress, "-", ":", 2), "-", ".", 1)
	deviceId := deviceUID[PCIAddressLength:]

	return pciAddress, deviceId
}

func (g *DevicesInfo) DeepCopy() DevicesInfo {
	devicesInfoCopy := DevicesInfo{}
	for duid, device := range *g {
		devicesInfoCopy[duid] = device.DeepCopy()
	}
	return devicesInfoCopy
}

func GetDevfsDriDir() string {
	devfsDriDir, found := os.LookupEnv(DevDriEnvVarName)

	if found {
		fmt.Printf("using custom devfs dri location: %v\n", devfsDriDir)
		return devfsDriDir
	}

	fmt.Println("using default devfs dri location: /dev/dri")
	return "/dev/dri"
}

// GetSysfsDir tries to get path where sysfs is mounted from
// env var, or fallback to hardcoded path.
func GetSysfsDir() string {
	sysfsPath, found := os.LookupEnv(SysfsEnvVarName)

	if found {
		if _, err := os.Stat(path.Join(sysfsPath, SysfsDRMpath)); err == nil {
			fmt.Printf("using custom sysfs location: %v\n", sysfsPath)
			return sysfsPath
		}
	}

	fmt.Println("using default sysfs location: /sys")
	// If /sys is not available, devices discovery will fail gracefully.
	return "/sys"
}
