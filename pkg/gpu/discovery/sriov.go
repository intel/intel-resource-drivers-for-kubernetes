/*
 * Copyright (c) 2026, Intel Corporation.  All Rights Reserved.
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

package discovery

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/helpers"

	"k8s.io/klog/v2"
)

// Detects if the GPU is a VF or PF. For PF check if SR-IOV is enabled, and the maximum
// number of VFs. For VF detects parent PF.
func detectSRIOV(newDeviceInfo *device.DeviceInfo, sysfsDevicesDir string, devicePCIAddress string, deviceID string) {
	sysfsDeviceDir := path.Join(sysfsDevicesDir, devicePCIAddress)
	totalvfsFile := path.Join(sysfsDeviceDir, "sriov_totalvfs")
	totalvfsByte, err := os.ReadFile(totalvfsFile)
	if err != nil {
		klog.V(5).Infof("Could not read totalvfs file (%s): %+v. Checking for physfn.", totalvfsFile, err)
		// Detect parent if device this is a VF
		physfnLink := path.Join(sysfsDeviceDir, "physfn")
		parentLink, err := os.Readlink(physfnLink)
		if err != nil {
			klog.V(5).Infof("Could not read %v: %v. Ignoring SR-IOV for device %v", physfnLink, err, devicePCIAddress)

			return
		}

		// no error, find out which VF index current device belongs to
		parentPCIAddress := parentLink[3:]
		vfIdx, err := deduceVfIdx(sysfsDevicesDir, parentPCIAddress, devicePCIAddress)
		if err != nil {
			klog.Errorf("Ignoring device %v. Error: %v", devicePCIAddress, err)

			return
		}

		parentUID := helpers.DeviceUIDFromPCIinfo(parentPCIAddress, deviceID)

		newDeviceInfo.VFIndex = vfIdx
		newDeviceInfo.Millicores = initialMillicores
		newDeviceInfo.ParentUID = parentUID
		newDeviceInfo.DeviceType = device.VfDeviceType
		klog.V(5).Infof("physfn OK, device %v is a VF from %v", newDeviceInfo.UID, newDeviceInfo.ParentUID)

		return
	}

	totalvfsInt, err := strconv.ParseUint(strings.TrimSpace(string(totalvfsByte)), 10, 64)
	if err != nil {
		klog.Errorf("Could not convert string into int: %s", string(totalvfsByte))

		return
	}

	klog.V(5).Infof("Detected SR-IOV capacity, max VFs: %v", totalvfsInt)

	// check if driver will pick up new VFs as DRM devices for dynamic provisioning
	driversAutoprobeFile := path.Join(sysfsDevicesDir, devicePCIAddress, "sriov_drivers_autoprobe")
	driversAutoprobeByte, err := os.ReadFile(driversAutoprobeFile)
	if err != nil {
		klog.V(5).Infof("Could not read sriov_drivers_autoprobe file: %v. Not enabling SR-IOV", err)
		return
	}

	if strings.TrimSpace(string(driversAutoprobeByte)) == "0" {
		klog.Infof("%v: sriov_drivers_autoprobe disabled", devicePCIAddress)
		return
	}
	newDeviceInfo.MaxVFs = totalvfsInt
}

func deduceVfIdx(sysfsDevicesDir string, parentDBDF string, vfDBDF string) (uint64, error) {
	filePath := path.Join(sysfsDevicesDir, parentDBDF, "virtfn*")
	files, _ := filepath.Glob(filePath)

	for _, virtfn := range files {
		klog.V(5).Infof("Checking %v", virtfn)
		virtfnTarget, err := os.Readlink(virtfn)
		if err != nil {
			klog.Warningf("Failed reading virtfn symlink %v: %v. Skipping", virtfn, err)
			continue
		}

		// ../0000:00:02.1  # 15 chars
		if len(virtfnTarget) != 15 {
			klog.Warningf("Symlink target does not match expected length: %v", virtfnTarget)
			continue
		}

		vfBase := path.Base(virtfn)
		vfIdxStr := vfBase[6:]
		klog.V(5).Infof("symlink target: %v, VF Base: %v, VF Idx: %s", virtfnTarget, vfBase, vfIdxStr)
		if virtfnTarget[3:] != vfDBDF {
			continue
		}

		vfIdx, err := strconv.ParseUint(vfIdxStr, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("could not parse VF index (%v): %v", vfIdxStr, err)
		}

		return vfIdx, nil
	}
	return 0, fmt.Errorf("could not find PF %v symlink to VF %v", parentDBDF, vfDBDF)
}
