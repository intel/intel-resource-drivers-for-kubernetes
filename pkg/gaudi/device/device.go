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
)

var (
	PciRegexp          = regexp.MustCompile(`[0-9a-f]{4}:[0-9a-f]{2}:[0-9a-f]{2}\.[0-7]$`)
	AccelRegexp        = regexp.MustCompile(`^accel[0-9]+$`)
	AccelControlRegexp = regexp.MustCompile(`^accel_controlD[0-9]+$`)
)

const (
	DevAccelEnvVarName   = "DEV_ACCEL_PATH"
	devfsDefaultAccelDir = "/dev/accel"
	SysfsEnvVarName      = "SYSFS_ROOT"
	sysfsDefaultRoot     = "/sys"
	// driver.sysfsDriverDir and driver.sysfsAccelDir are sysfsDriverPath and sysfsAccelPath
	// respectively prefixed with $SYSFS_ROOT.
	SysfsDriverPath = "bus/pci/drivers/habanalabs"
	SysfsAccelPath  = "devices/virtual/accel/"
	CDIRoot         = "/etc/cdi"
	CDIVendor       = "intel.com"
	CDIKind         = CDIVendor + "/gaudi"
	PciDBDFLength   = len("0000:00:00.0")
)

// DeviceInfo is an internal structure type to store info about discovered device.
type DeviceInfo struct {
	UID       string `json:"uid"`       // unique identifier, pci_DBDF-pci_device_id
	Model     string `json:"model"`     // pci_device_id
	DeviceIdx uint64 `json:"deviceidx"` // accel device number (e.g. 0 for /dev/accel/accel0)
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
