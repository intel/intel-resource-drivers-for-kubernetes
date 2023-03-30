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
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	intelcrd "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/crd/intel/v1alpha/api"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/sriov"
	"k8s.io/klog/v2"
)

const (
	sysfsDRMDir = "/sys/class/drm/"
)

// Wait until timeout and monitor for virtfnX symlinks to be gone from parent pci device.
func waitUntilNoVFs(pciDBDF string) error {
	filePath := filepath.Join(sysfsI915DriverDir, pciDBDF, "virtfn*")
	attemptsLimit := 10
	attemptDelay := 1000 * time.Millisecond

	for attempt := 0; attempt < attemptsLimit+1; attempt++ {
		klog.V(5).Infof("waiting until vfs %v are gone, attempt %v", filePath, attempt)
		time.Sleep(attemptDelay)
		files, _ := filepath.Glob(filePath)
		if len(files) != 0 {
			klog.V(5).Infof("found %v vfs still present", len(files))
		} else {
			klog.V(5).Infof("all vfs are gone on after %v attempts", attempt)
			return nil
		}
	}

	klog.Errorf("timeout waiting for vfs to be disabled on deviec %v", pciDBDF)
	return fmt.Errorf("timeout waiting for vfs to be disabled on deviec %v", pciDBDF)
}

// When removeAllVFs is called - no containers is supposed to be using VFs and VFs can be removed
// in case they ever need to be different memory size.
func removeAllVFs(parentDevice *DeviceInfo) error {
	pciDBDF := parentDevice.uid[:12]
	klog.V(5).Infof("removing all vfs for %v", pciDBDF)
	sriovNumvfsFile := path.Join(sysfsI915DriverDir, pciDBDF, "sriov_numvfs")

	numvfsInt := parentDevice.maxvfs
	numvfsBytes, err := os.ReadFile(sriovNumvfsFile)

	if err != nil { // not cricital, we will just have to unconfigure all VFs present
		klog.Warning("could not read %v file: %v", sriovNumvfsFile, err)
	} else {
		numvfsInt, err = strconv.Atoi(strings.TrimSpace(string(numvfsBytes)))
		if err != nil {
			klog.Errorf("could not convert numvfs to int: %v", err)
		}
		klog.V(5).Infof("num_vfs had value %v", numvfsInt)
	}

	fhandle, err := os.OpenFile(sriovNumvfsFile, os.O_APPEND|os.O_WRONLY, os.ModeAppend)

	if err != nil {
		klog.Errorf("failed to open %v file", sriovNumvfsFile)
		return fmt.Errorf("failed to open %v file", sriovNumvfsFile)
	}

	_, err = fhandle.WriteString("0")
	if err != nil {
		klog.Error("could not write to file %v", sriovNumvfsFile)
		return fmt.Errorf("could not write to file %v", sriovNumvfsFile)
	}

	if err = fhandle.Close(); err != nil {
		klog.Error("could not close file %v", sriovNumvfsFile)
		// do not fail here, main job is done by now
	}

	if err = waitUntilNoVFs(pciDBDF); err == nil {
		klog.V(5).Infof("cleaning up profiles for %v VFs", numvfsInt)
		if err2 := cleanupManualConfigurationMaybe(pciDBDF, parentDevice.cardidx, numvfsInt); err2 != nil {
			klog.Error("failed to cleanup manually provisioned VFs: %v", err2)
		}

	} else {
		klog.Error("Timed out waiting for VFs to be removed")
	}

	klog.V(5).Infof("Successfully cleaned up all VFs for %v", parentDevice.uid)
	return nil
}

// Ensure single VF is in place and DRM files for it are present.
func validateVF(drmDir string, cardregexp *regexp.Regexp) error {
	klog.V(5).Infof("checking DRM files in vf %v", drmDir)
	files, err := os.ReadDir(drmDir)
	if err != nil {
		return fmt.Errorf("cannot read sysfs folder %v: %v", drmDir, err)
	}

	cardFound := false
	for _, filename := range files {
		klog.V(5).Infof("checking file %v", filename.Name())
		if cardregexp.MatchString(filename.Name()) {
			klog.V(5).Info("found cardX file")
			cardFound = true
		}
	}
	if !cardFound {
		klog.Errorf("could not find DRM cardX device in %v", drmDir)
		return fmt.Errorf("could not find DRM cardX device in %v", drmDir)
	}
	return nil
}

// Ensure the new VSs were created and DRM devices are created, wait if needed.
func validateVFs(pciDBDF string, vfs map[string]*DeviceInfo) error {
	klog.V(5).Infof("validationg %v vfs creation on %v, ignoring profiles (NOT IMPLEMENTED)", len(vfs), pciDBDF)
	cardregexp := regexp.MustCompile(cardRE)
	attemptsLimit := 10
	attemptDelay := 1000 * time.Millisecond
	// loop through sysfsI915DriverDir/pciDBDF/virtfn* symlinks, check drm/[cardX, renderDY]
	for _, vf := range vfs {
		klog.V(5).Infof("Validating vf %+v", vf)
		drmDir := path.Join(sysfsI915DriverDir, pciDBDF, fmt.Sprintf("virtfn%d/drm/", vf.pciVFIndex()))
		vfOK := false
		for attempt := 0; attempt < attemptsLimit; attempt++ {
			if err := validateVF(drmDir, cardregexp); err == nil {
				klog.V(5).Infof("vf %v of gpu %v is OK", vf.vfindex, pciDBDF)
				vfOK = true
				break
			}
			time.Sleep(attemptDelay)
		}
		if !vfOK {
			klog.Errorf("vf %d of GPU %s is NOT OK, did not check the rest of new vfs", vf.vfindex, pciDBDF)
			return fmt.Errorf("vf %d of GPU %s is NOT OK, did not check the rest of new vfs", vf.vfindex, pciDBDF)
		}
	}
	return nil
}

// Iterate through the lists and check if sequence is broken, e.g. pciDBDFs, vfIdx.
func validateVFsToBeProvisioned(toProvision map[string]map[string]*DeviceInfo) error {
	for parentUID, vfs := range toProvision {
		// either all fairShare or all non-fairShare
		fairShared := 0
		// sequence should not be broken
		for vfIdx := 0; vfIdx < len(vfs); vfIdx++ {
			vfUID := fmt.Sprintf("%s-vf%d", parentUID, vfIdx)
			if _, found := vfs[vfUID]; !found {
				return fmt.Errorf("vf with index %d not found", vfIdx)
			}
			if vfs[vfUID].vfprofile == sriov.FairShareProfile {
				fairShared++
			}
		}
		if fairShared != len(vfs) && fairShared != 0 {
			return fmt.Errorf(
				"vfs with fairshare profile cannot be provisioned together with specific profile vfs")
		}

	}
	return nil
}

// Tell driver to create VFs. Should only work if no VFs exist at the moment on given GPU.
func (d *driver) provisionVFs(toProvision map[string]map[string]*DeviceInfo) (DevicesInfo, error) {
	klog.V(5).Infof("provisionVFs is called for %v GPUs.", len(toProvision))

	provisionedVFs := DevicesInfo{}

	if err := validateVFsToBeProvisioned(toProvision); err != nil {
		return provisionedVFs, err
	}

	for parentUID, vfs := range toProvision {
		pciDBDF := parentUID[:12]
		// At least as many VFs as requested has to be provisioned.
		// It could be possible to create more based on the maximum requested memory for VF
		numvfs := len(vfs)
		klog.V(5).Infof("provisioning %v VFs for GPU %v ", numvfs, parentUID)
		// move to controller
		/*
			memoryRequests := []int{}
			for _, vf := range vfs {
				memoryRequests = append(memoryRequests, vf.memory)
			}

			fairNumvfs, fairShareErr := sriov_profiles.MaxFairVfs(d.state.allocatable[parentUID].model, memoryRequests)
			if fairShareErr == nil {
				numvfs = fairNumvfs // override with bigger or same number
			} else { // find per VF profile to configure manually
				for _, vf := range vfs {
					profileName, err := sriov_profiles.PickVFProfile(d.state.allocatable[parentUID].model, vf.memory)
					if err != nil {
						return provisionedVFs, err
					}

					profileNames = append(profileNames, profileName)
				}
				// configure VFs before enabling them
				preConfigureVFs(d.state.allocatable[parentUID].cardidx, profileNames, numvfs)
			}
		*/

		if needToPreconfigureVFs(vfs) {
			err := preConfigureVFs(d.state.allocatable[parentUID].cardidx, vfs)
			if err != nil {
				klog.Error("failed preconfiguring vfs")
				return provisionedVFs, fmt.Errorf("failed preconfiguring vfs")
			}
		}

		sriovNumvfsFile := path.Join(sysfsI915DriverDir, pciDBDF, "sriov_numvfs")

		fhandle, err := os.OpenFile(sriovNumvfsFile, os.O_APPEND|os.O_WRONLY, os.ModeAppend)
		if err != nil {
			klog.Errorf("failed to open %v file: %v", sriovNumvfsFile, err)
			return provisionedVFs, fmt.Errorf("failed to open %v file: %v", sriovNumvfsFile, err)
		}

		_, err = fhandle.WriteString(fmt.Sprint(numvfs))
		if err != nil {
			klog.Errorf("could not write to file %v: %v", sriovNumvfsFile, err)
			return provisionedVFs, fmt.Errorf("could not write to file %v: %v", sriovNumvfsFile, err)
		}

		if err = fhandle.Close(); err != nil {
			klog.Errorf("could not close file %v: %v", sriovNumvfsFile, err)
			// do not fail here, main job is done by now
		}

		// wait for VFs and attempt to dismantle the VFs if DRM devices did not come up properly
		if err2 := validateVFs(pciDBDF, vfs); err2 != nil {
			cleanupErr := removeAllVFs(d.state.allocatable[parentUID])
			if cleanupErr != nil {
				klog.Errorf("vfs cleanup failed for %v: %v", pciDBDF, cleanupErr)
				return provisionedVFs, fmt.Errorf("failed to cleaned up vfs after failed provisioning: %v.", cleanupErr)
			}
			return provisionedVFs, fmt.Errorf("failed to validate provisioned VFs: %v, cleaned up successfully.", err2)
		}
	}

	// if no errors - discover all new VFs
	allDevices := enumerateAllPossibleDevices()

	// amount of provisioned VFs on device might be more than requested, announce all VFs, not only requested
	for duid, device := range allDevices {
		if device.deviceType == intelcrd.VfDeviceType {
			if _, wasRequested := toProvision[device.parentuid]; wasRequested {
				provisionedVFs[duid] = device
			}
		}
	}

	return provisionedVFs, nil
}

func needToPreconfigureVFs(vfs map[string]*DeviceInfo) bool {
	needToPreconfigureVFs := false
	for _, vf := range vfs {
		if vf.vfprofile != sriov.FairShareProfile {
			needToPreconfigureVFs = true
		}
		break
	}
	return needToPreconfigureVFs
}

// getTileCount reads the tile count.
func getTileCount(gpuName string) (numTiles int) {
	filePath := filepath.Join(sysfsDRMDir, gpuName, "gt/gt*")
	files, _ := filepath.Glob(filePath)

	if len(files) == 0 {
		return 1
	}
	return len(files)
}

// Return the amount of local memory GPU has, if any, otherwise shared memory presumed.
func getLocalMemoryAmountMiB(gpuName string) int {
	numTiles := getTileCount(gpuName)
	filePath := filepath.Join(sysfsDRMDir, gpuName, "lmem_total_bytes")

	klog.V(5).Infof("probing local memory at %v", filePath)
	dat, err := os.ReadFile(filePath)
	if err != nil {
		klog.Warning("no local memory detected, could not read file: %v", err)
		return 0
	}

	totalPerTile, err := strconv.Atoi(strings.TrimSpace(string(dat)))
	if err != nil {
		klog.Errorf("could not convert lmem_total_bytes: %v", err)
		return 0
	}

	totalPerTileMiB := totalPerTile / bytesInMB // MiB is is that many bytes
	klog.V(5).Infof("detected %d MiB local memory (per tile, tiles: %v)", totalPerTileMiB, numTiles)

	return totalPerTileMiB * numTiles
}
