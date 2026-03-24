/*
 * Copyright (c) 2025, Intel Corporation.  All Rights Reserved.
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

package helpers

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"k8s.io/klog/v2"
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
			klog.V(5).Infof("using custom sysfs location: %v\n", sysfsRoot)
			return sysfsRoot
		} else {
			klog.V(5).Infof("could not find sysfs at '%v' from %v env var: %v\n", sysfsPath, SysfsEnvVarName, err)
		}
	}

	klog.V(5).Infof("using default sysfs location: %v\n", sysfsDefaultRoot)
	// If /sys is not available, devices discovery will fail gracefully.
	return sysfsDefaultRoot
}

func GetDevfsRoot(devfsRootEnvVarName string, devPath string) string {
	devfsRoot, found := os.LookupEnv(devfsRootEnvVarName)

	if found {
		if _, err := os.Stat(path.Join(devfsRoot, devPath)); err == nil {
			klog.V(5).Infof("using custom devfs location: %v\n", devfsRoot)
			return devfsRoot
		} else {
			klog.V(5).Infof("could not find devfs at '%v' from %v env var: %v\n", devPath, devfsRootEnvVarName, err)
		}
	}

	klog.V(5).Infof("using default devfs root: %v\n", devfsDefaultRoot)
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

func DeterminePCIRoot(link string) (string, error) {
	// e.g. /sys/devices/pci0000:16/0000:16:02.0/0000:17:00.0/0000:18:00.0/0000:19:00.0
	linkTarget, err := filepath.EvalSymlinks(link)
	if err != nil {
		return "", fmt.Errorf("could not determine PCI root complex ID from '%v': %v", link, err)
	}
	klog.V(5).Infof("PCI device location: %v", linkTarget)
	parts := strings.Split(linkTarget, "/")

	// To support arbitrary sysfs location, discard leading path elements
	// before devices minus one.
	trueSysfsRootIdx := 0
	for idx, pathElement := range parts {
		if pathElement == "devices" && idx > 0 {
			trueSysfsRootIdx = idx - 1
			break
		}
	}
	if trueSysfsRootIdx != 0 {
		parts = parts[trueSysfsRootIdx:]
	}

	if len(parts) > 2 && parts[1] == "devices" {
		return parts[2], nil
	}

	return "", fmt.Errorf("could not parse sysfs link target %v: %v", linkTarget, parts)
}
