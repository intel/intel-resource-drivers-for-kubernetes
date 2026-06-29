//
// Copyright (C) 2025-2026 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0
//

package helpers

import (
	"fmt"
	"os"
	"path"
	"strings"
)

const (
	SysfsEnvVarName  = "SYSFS_ROOT"
	sysfsDefaultRoot = "/sys"

	DevfsEnvVarName  = "DEVFS_ROOT"
	devfsDefaultRoot = "/dev"

	PCIAddressLength = len("0000:00:00.0")
)

// GetSysfsRoot tries to get path where sysfs is mounted from the env var,
// or fallback to hardcoded path.
func GetSysfsRoot(sysfsPath string) string {
	sysfsRoot, found := os.LookupEnv(SysfsEnvVarName)

	if found {
		if _, err := os.Stat(path.Join(sysfsRoot, sysfsPath)); err == nil {
			return sysfsRoot
		}
	}

	// If /sys is not available, devices discovery will fail gracefully.
	return sysfsDefaultRoot
}

func GetDevfsRoot(devPath string) string {
	devfsRoot, found := os.LookupEnv(DevfsEnvVarName)

	if found {
		if _, err := os.Stat(path.Join(devfsRoot, devPath)); err == nil {
			return devfsRoot
		}
	}

	return devfsDefaultRoot
}

func PciInfoFromDeviceUID(deviceUID string) (string, string) {
	// 0000-00-01-0-0x0000 -> 0000:00:01.0, 0x0000
	rfc1123PCIaddress := deviceUID[:PCIAddressLength]
	pciAddress := strings.Replace(strings.Replace(rfc1123PCIaddress, "-", ":", 2), "-", ".", 1)
	deviceId := deviceUID[PCIAddressLength+1:]

	return pciAddress, deviceId
}

func DeviceUIDFromPCIinfo(pciAddress string, pciid string) string {
	// 0000:00:01.0, 0x0000 -> 0000-00-01-0-0x0000
	// Replace colons and the dot in PCI address with hyphens.
	rfc1123PCIaddress := strings.ReplaceAll(strings.ReplaceAll(pciAddress, ":", "-"), ".", "-")
	deviceId := pciid
	if len(deviceId) == 4 {
		deviceId = "0x" + deviceId
	}
	newUID := fmt.Sprintf("%v-%v", rfc1123PCIaddress, deviceId)

	return newUID
}
