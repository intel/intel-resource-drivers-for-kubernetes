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
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	intelcrd "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gpu/v1alpha2/api"
	sriovProfiles "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/sriov"

	"k8s.io/klog/v2"
)

const (
	initialMillicores = 1000
)

// Detect devices from sysfs drm directory (card id and renderD id).
func enumerateAllPossibleDevices(sysfsI915Dir string, sysfsDrmDir string) (map[string]*DeviceInfo, error) {

	devices := make(map[string]*DeviceInfo)

	cardregexp := regexp.MustCompile(cardRE)
	renderdregexp := regexp.MustCompile(renderdIdRE)
	pciregexp := regexp.MustCompile(pciAddressRE)
	files, err := os.ReadDir(sysfsI915Dir)

	if err != nil {
		if err == os.ErrNotExist {
			klog.V(5).Infof("No Intel GPU devices found on this host. %v does not exist", sysfsI915Dir)
			return devices, nil
		}
		klog.Errorf("could not read sysfs directory: %v", err)
		return devices, fmt.Errorf("reading sysfs dir: %v", err)
	}

	for _, pciDBDF := range files {
		deviceDBDF := pciDBDF.Name()
		// check if file is pci device
		if !pciregexp.MatchString(deviceDBDF) {
			continue
		}
		klog.V(5).Infof("Found GPU PCI device: " + deviceDBDF)

		deviceI915Dir := filepath.Join(sysfsI915Dir, deviceDBDF)
		deviceIdFile := filepath.Join(deviceI915Dir, "device")
		deviceIdBytes, err := os.ReadFile(deviceIdFile)
		if err != nil {
			klog.Errorf("Failed reading device file (%s): %+v", deviceIdFile, err)
			continue
		}
		deviceId := strings.TrimSpace(string(deviceIdBytes))
		uid := fmt.Sprintf("%v-%v", deviceDBDF, deviceId)
		klog.V(5).Infof("New gpu UID: %v", uid)
		newDeviceInfo := &DeviceInfo{
			uid:        uid,
			model:      deviceId,
			memoryMiB:  0,
			millicores: initialMillicores,
			deviceType: intelcrd.GpuDeviceType, // presume GPU, detect the physfn / parent lower
			cardidx:    0,
			renderdidx: 0,
		}

		// get card and renderD indexes
		drmDir := filepath.Join(deviceI915Dir, "drm")
		drmFiles, err := os.ReadDir(drmDir)

		if err != nil { // ignore this device
			klog.Errorf("Cannot read device folder %v: %v", drmDir, err)
			continue
		}

		for _, drmFile := range drmFiles {
			drmFileName := drmFile.Name()
			if cardregexp.MatchString(drmFileName) {
				cardIdx, err := strconv.ParseUint(drmFileName[4:], 10, 64)
				if err != nil {
					klog.Errorf("Failed to parse index of DRM card device '%v', skipping", drmFileName)
					continue
				}
				newDeviceInfo.cardidx = cardIdx
				drmGpuDir := filepath.Join(sysfsDrmDir, drmFileName)
				newDeviceInfo.memoryMiB = getLocalMemoryAmountMiB(drmGpuDir)
			} else if renderdregexp.MatchString(drmFileName) {
				renderDidx, err := strconv.ParseUint(drmFileName[7:], 10, 64)
				if err != nil {
					klog.Errorf("failed to parse renderDN device: %v, skipping", drmFileName)
					continue
				}
				newDeviceInfo.renderdidx = renderDidx
			}
		}

		detectSRIOV(newDeviceInfo, sysfsI915Dir, deviceDBDF, deviceId)
		// only GPU needs ECC information to provision VFs
		if newDeviceInfo.deviceType == intelcrd.GpuDeviceType {
			newDeviceInfo.eccOn = detectEcc(deviceId, newDeviceInfo.memoryMiB)
		}
		devices[newDeviceInfo.uid] = newDeviceInfo

	}
	return devices, nil
}

func detectEcc(deviceId string, detectedMemoryInMiB uint64) bool {
	vfMemMax, err := sriovProfiles.GetMaximumVFMemorySizeMiB(deviceId, false)
	if err != nil {
		klog.V(5).Infof("could not get maximum VF memory: %v", err)
		return false
	}

	// First profile should always be 1 VF with almost all available memory.
	// Usually VF lmem_quota is just a little less than the detected amount
	// of device total local memory, the difference is due to PF memory
	// reservation. In case if ECC is enabled, the detected memory will be
	// lower than non-ecc 1-VF profile's memory quota.
	if vfMemMax > detectedMemoryInMiB {
		klog.V(5).Infof("ECC is enabled, based on the total available local memory (%v / %v)", detectedMemoryInMiB, vfMemMax)
		return true
	}

	klog.V(5).Infof("ECC is disabled")
	return false
}

// Detects if the GPU is a VF or PF. For PF check if SR-IOV is enabled, and the maximum
// number of VFs. For VF detects parent PR.
func detectSRIOV(newDeviceInfo *DeviceInfo, sysfsI915Dir string, deviceDBDF string, deviceID string) {
	deviceI915Dir := filepath.Join(sysfsI915Dir, deviceDBDF)
	totalvfsFile := filepath.Join(deviceI915Dir, "sriov_totalvfs")
	totalvfsByte, err := os.ReadFile(totalvfsFile)
	if err != nil {
		klog.V(5).Infof("Could not read totalvfs file (%s): %+v. Checking for physfn.", totalvfsFile, err)
		// Detect parent if device this is a VF
		physfnLink := filepath.Join(deviceI915Dir, "physfn")
		parentLink, err := os.Readlink(physfnLink)
		if err != nil {
			klog.Errorf("Failed reading %v: %v. Ignoring SR-IOV for device %v", physfnLink, err, deviceDBDF)

			return
		}

		// no error, find out which VF index current device belongs to
		parentDBDF := parentLink[3:]
		vfIdx, err := deduceVfIdx(sysfsI915Dir, parentDBDF, deviceDBDF)
		if err != nil {
			klog.Errorf("Ignoring device %v", deviceDBDF)

			return
		}

		newDeviceInfo.uid = fmt.Sprintf("%s-%s-vf%s", parentDBDF, deviceID, vfIdx)
		newDeviceInfo.parentuid = fmt.Sprintf("%s-%s", parentDBDF, deviceID)
		newDeviceInfo.deviceType = intelcrd.VfDeviceType
		klog.V(5).Infof("physfn OK, device %v is a VF from %v", newDeviceInfo.uid, newDeviceInfo.parentuid)

		return
	}

	totalvfsInt, err := strconv.ParseUint(strings.TrimSpace(string(totalvfsByte)), 10, 64)
	if err != nil {
		klog.Errorf("Could not convert string into int: %s", string(totalvfsByte))

		return
	}

	klog.V(5).Infof("Detected SR-IOV capacity, max VFs: %v", totalvfsInt)

	// check if driver will pick up new VFs as DRM devices for dynamic provisioning
	driversAutoprobeFile := filepath.Join(sysfsI915Dir, deviceDBDF, "sriov_drivers_autoprobe")
	driversAutoprobeByte, err := os.ReadFile(driversAutoprobeFile)
	if err != nil {
		klog.V(5).Infof("Could not read sriov_drivers_autoprobe file: %v. Not enabling SR-IOV", err)

		return
	}

	if strings.TrimSpace(string(driversAutoprobeByte)) == "0" {
		klog.V(5).Infof("sriov_drivers_autoprobe disabled. Not enabling SR-IOV", err)

		return
	}
	klog.V(5).Info("Driver autoprobe is enabled, enabling SR-IOV")
	newDeviceInfo.maxvfs = totalvfsInt
}

func deduceVfIdx(sysfsI915Dir string, parentDBDF string, vfDBDF string) (string, error) {
	filePath := filepath.Join(sysfsI915Dir, parentDBDF, "virtfn*")
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
	return "", fmt.Errorf("could not find PF %v symlink to VF %v", parentDBDF, vfDBDF)
}

// getTileCount reads the tile count.
func getTileCount(drmGpuDir string) (numTiles uint64) {
	filePath := filepath.Join(drmGpuDir, "gt/gt*")
	files, _ := filepath.Glob(filePath)

	if len(files) == 0 {
		return 1
	}
	return uint64(len(files))
}

// Return the amount of local memory GPU has, if any, otherwise shared memory presumed.
func getLocalMemoryAmountMiB(drmGpuDir string) uint64 {
	numTiles := getTileCount(drmGpuDir)
	filePath := filepath.Join(drmGpuDir, "lmem_total_bytes")

	klog.V(5).Infof("probing local memory at %v", filePath)
	dat, err := os.ReadFile(filePath)
	if err != nil {
		klog.Warning("no local memory detected, could not read file: %v", err)
		return 0
	}

	totalPerTile, err := strconv.ParseUint(strings.TrimSpace(string(dat)), 10, 64)
	if err != nil {
		klog.Errorf("could not convert lmem_total_bytes: %v", err)
		return 0
	}

	totalPerTileMiB := totalPerTile / (1024 * 1024)
	klog.V(5).Infof("detected %d MiB local memory (per tile, tiles: %v)", totalPerTileMiB, numTiles)

	return totalPerTileMiB * numTiles
}
