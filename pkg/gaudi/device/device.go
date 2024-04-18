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
	PciRegexp          = regexp.MustCompile(`[0-9a-f]{4}:[0-9a-f]{2}:[0-9a-f]{2}\.[0-7]$`)
	AccelRegexp        = regexp.MustCompile(`^accel[0-9]+$`)
	AccelControlRegexp = regexp.MustCompile(`^accel_controlD[0-9]+$`)
	ModelNames         = map[string]string{
		"0x1000": "Gaudi",
		"0x1010": "Gaudi",
		"0x1020": "Gaudi2",
		"0x1030": "Gaudi3",
	}
)

const (
	DevAccelEnvVarName   = "DEV_ACCEL_PATH"
	devfsDefaultAccelDir = "/dev/accel"
	SysfsEnvVarName      = "SYSFS_ROOT"
	sysfsDefaultRoot     = "/sys"
	// driver.sysfsDriverDir and driver.sysfsAccelDir are sysfsDriverPath and sysfsAccelPath
	// respectively prefixed with $SYSFS_ROOT.
	SysfsDriverPath        = "bus/pci/drivers/habanalabs"
	SysfsAccelPath         = "devices/virtual/accel/"
	CDIRoot                = "/etc/cdi"
	CDIVendor              = "intel.com"
	CDIKind                = CDIVendor + "/gaudi"
	PCIAddressLength       = len("0000:00:00.0")
	PreparedClaimsFileName = "preparedClaims.json"
)

// DeviceInfo is an internal structure type to store info about discovered device.
type DeviceInfo struct {
	// UID is a unique identifier on node, used in ResourceSlice K8s API object as RFC1123-compliant identifier.
	// Consists of PCIAddress and Model with colons and dots replaced with hyphens, e.g. 0000-01-02-0-0x12345.
	UID        string `json:"uid"`
	PCIAddress string `json:"pciaddress"` // PCI address in Linux DBDF notation for use with sysfs, e.g. 0000:00:00.0
	Model      string `json:"model"`      // PCI device ID
	DeviceIdx  uint64 `json:"deviceidx"`  // accel device number (e.g. 0 for /dev/accel/accel0)
}

func (g DeviceInfo) CDIName() string {
	return fmt.Sprintf("%s=%s", CDIKind, g.UID)
}

func (g *DeviceInfo) DeepCopy() *DeviceInfo {
	di := *g
	return &di
}

func (g *DeviceInfo) ModelName() string {
	if model, found := ModelNames[g.Model]; found {
		return model
	}
	return "Unknown"
}

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

// DevicesInfo is a dictionary with DeviceInfo.uid being the key.
type DevicesInfo map[string]*DeviceInfo

func (g *DevicesInfo) DeepCopy() DevicesInfo {
	devicesInfoCopy := DevicesInfo{}
	for duid, device := range *g {
		devicesInfoCopy[duid] = device.DeepCopy()
	}
	return devicesInfoCopy
}

func GetDevfsAccelDir() string {
	devfsAccelDir, found := os.LookupEnv(DevAccelEnvVarName)

	if found {
		fmt.Printf("using custom devfs accel location: %v\n", devfsAccelDir)
		return devfsAccelDir
	}

	fmt.Printf("using default devfs accel location: %v\n", devfsDefaultAccelDir)
	return devfsDefaultAccelDir
}

// GetSysfsRoot tries to get path where sysfs is mounted from
// env var, or fallback to hardcoded path.
func GetSysfsRoot() string {
	sysfsPath, found := os.LookupEnv(SysfsEnvVarName)

	if found {
		if _, err := os.Stat(path.Join(sysfsPath, SysfsAccelPath)); err == nil {
			fmt.Printf("using custom sysfs location: %v\n", sysfsPath)
			return sysfsPath
		}
	}

	fmt.Printf("using default sysfs location: %v\n", sysfsDefaultRoot)
	// If /sys is not available, devices discovery will fail gracefully.
	return sysfsDefaultRoot
}
