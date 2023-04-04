/*
 * Copyright (c) 2023, Intel Corporation.  All Rights Reserved.
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

package main

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	intelcrd "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/crd/intel/v1alpha/api"
	"k8s.io/klog/v2"
)

const (
	sysfsI915DriverDir = "/sys/bus/pci/drivers/i915"
	pciAddressRE       = `[0-9a-f]{4}:[0-9a-f]{2}:[0-9a-f]{2}\.[0-7]$`
	cardRE             = `^card[0-9]+$`
	renderdIdRE        = `^renderD[0-9]+$`
)

// Detect devices from sysfs drm directory (card id and renderD id).
func enumerateAllPossibleDevices() map[string]*DeviceInfo {

	devices := make(map[string]*DeviceInfo)

	cardregexp := regexp.MustCompile(cardRE)
	renderdregexp := regexp.MustCompile(renderdIdRE)
	pciregexp := regexp.MustCompile(pciAddressRE)
	files, err := os.ReadDir(sysfsI915DriverDir)

	if err != nil {
		if err == os.ErrNotExist {
			klog.V(5).Infof("No Intel GPU devices found on this host. %v does not exist", sysfsI915DriverDir)
			return devices
		}
		klog.Errorf("Cannot read sysfs folder: %v", err)
		return devices
	}

	for _, i915file := range files {
		// check if file is pci device
		if !pciregexp.MatchString(i915file.Name()) {
			continue
		}
		klog.V(5).Infof("Found GPU PCI device: " + i915file.Name())

		deviceIdFile := path.Join(sysfsI915DriverDir, i915file.Name(), "device")
		deviceIdBytes, err := os.ReadFile(deviceIdFile)
		if err != nil {
			klog.Errorf("Failed reading device file (%s): %+v", deviceIdFile, err)
			continue
		}
		deviceId := strings.TrimSpace(string(deviceIdBytes))
		uid := fmt.Sprintf("%v-%v", i915file.Name(), deviceId)
		klog.V(5).Infof("New gpu UID: %v", uid)
		newDeviceInfo := &DeviceInfo{
			uid:        uid,
			model:      deviceId,
			memory:     0,
			deviceType: intelcrd.GpuDeviceType, // presume GPU, detect the physfn / parent lower
			cardidx:    0,
			renderdidx: 0,
		}

		// get card and renderD indexes
		drmDir := path.Join(sysfsI915DriverDir, i915file.Name(), "drm")
		drmFiles, err := os.ReadDir(drmDir)

		if err != nil { // ignore this device
			klog.Errorf("Cannot read device folder %v: %v", drmDir, err)
			continue
		}

		for _, drmFile := range drmFiles {
			if cardregexp.MatchString(drmFile.Name()) {
				cardIdx, err := strconv.Atoi(drmFile.Name()[4:])
				if err != nil {
					klog.Errorf("Failed to parse index of DRM card device '%v', skipping", drmFile)
					continue
				}
				newDeviceInfo.cardidx = cardIdx
				newDeviceInfo.memory = getLocalMemoryAmountMiB(drmFile.Name())
			} else if renderdregexp.MatchString(drmFile.Name()) {
				renderDidx, err := strconv.Atoi(drmFile.Name()[7:])
				if err != nil {
					klog.Errorf("failed to parse renderDN device: %v, skipping", drmFile)
					continue
				}
				newDeviceInfo.renderdidx = renderDidx
			}
		}

		detectSRIOV(newDeviceInfo, i915file, deviceId)
		devices[newDeviceInfo.uid] = newDeviceInfo

	}
	return devices
}

// Detects if the GPU is a VF or PF. For PF check if SR-IOV is enabled, and the maximum
// number of VFs. For VF detects parent PR.
func detectSRIOV(newDeviceInfo *DeviceInfo, i915file fs.DirEntry, deviceID string) {
	totalvfsFile := path.Join(sysfsI915DriverDir, i915file.Name(), "sriov_totalvfs")
	totalvfsByte, err := os.ReadFile(totalvfsFile)
	if err != nil {
		klog.V(5).Infof("Could not read totalvfs file (%s): %+v. Checking for physfn.", totalvfsFile, err)
		// Detect parent if device this is a VF
		physfnLink := path.Join(sysfsI915DriverDir, i915file.Name(), "physfn")
		parentLink, err := os.Readlink(physfnLink)
		if err != nil {
			klog.Errorf("Failed reading %v: %v. Ignoring SRIOV for device %v", physfnLink, err, i915file.Name())

			return
		}

		// no error, find out which VF index current device belongs to
		parentDBDF := parentLink[3:]
		vfIdx, err := deduceVfIdx(parentDBDF, i915file.Name())
		if err != nil {
			klog.Errorf("Ignoring device %v", i915file.Name())

			return
		}

		newDeviceInfo.uid = fmt.Sprintf("%s-%s-vf%s", parentDBDF, deviceID, vfIdx)
		newDeviceInfo.parentuid = fmt.Sprintf("%s-%s", parentDBDF, deviceID)
		newDeviceInfo.deviceType = intelcrd.VfDeviceType
		klog.V(5).Infof("physfn OK, device %v is a VF from %v", newDeviceInfo.uid, newDeviceInfo.parentuid)

		return
	}

	totalvfsInt, err := strconv.Atoi(strings.TrimSpace(string(totalvfsByte)))
	if err != nil {
		klog.Errorf("Could not convert string into int: %s", string(totalvfsByte))

		return
	}

	klog.V(5).Infof("Detected SR-IOV capacity, max VFs: %v", totalvfsInt)

	// check if driver will pick up new VFs as DRM devices for dynamic provisioning
	driversAutoprobeFile := path.Join(sysfsI915DriverDir, i915file.Name(), "sriov_drivers_autoprobe")
	driversAutoprobeByte, err := os.ReadFile(driversAutoprobeFile)
	if err != nil {
		klog.V(5).Infof("Could not read sriov_drivers_autoprobe file: %v. Not enabling SRIOV", err)

		return
	}

	if strings.TrimSpace(string(driversAutoprobeByte)) == "0" {
		klog.V(5).Infof("sriov_drivers_autoprobe disabled. Not enabling SR-IOV", err)

		return
	}
	klog.V(5).Info("Driver autoprobe is enabled, enabling SR-IOV")
	newDeviceInfo.maxvfs = totalvfsInt
}

func deduceVfIdx(parentDBDF string, vfDBDF string) (string, error) {
	filePath := filepath.Join(sysfsI915DriverDir, parentDBDF, "virtfn*")
	files, _ := filepath.Glob(filePath)
	for _, virtfn := range files {
		klog.V(5).Infof("Checking %v", virtfn)
		virtfnTarget, err := os.Readlink(virtfn)
		if err != nil {
			klog.Warningf("Failed reading virtfn symlink %v: %v. Skipping", virtfn, err)
		}

		// ../0000:00:02.1  # 15 chars
		if len(virtfnTarget) != 15 {
			klog.Warningf("Symlink target does not match expected length: %v", virtfnTarget)
			continue
		}

		vfBase := filepath.Base(virtfn)
		vfIdx := vfBase[6:]
		klog.V(5).Infof("symlink target: %v, VF Base: %v, VF Idx: %v", virtfnTarget, vfBase, vfIdx)
		if virtfnTarget[3:] == vfDBDF {
			return vfIdx, nil
		}
	}
	return "", fmt.Errorf("Could not find PF %v symlink to VF %v", parentDBDF, vfDBDF)
}
