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
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	intelcrd "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gpu/v1alpha2/api"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/sriov"
	"k8s.io/klog/v2"
)

const (
	vfMemConfigFile = "/defaults/vf-memory.config"
)

// waitUntilNoVFs waits until timeout for all virtfnX symlinks to be gone from parent pci device.
func (d *driver) waitUntilNoVFs(pciDBDF string) error {
	filePath := filepath.Join(d.sysfsI915Dir, pciDBDF, "virtfn*")
	attemptsLimit := 10
	attemptDelay := 1000 * time.Millisecond

	for attempt := 0; attempt < attemptsLimit+1; attempt++ {
		klog.V(5).Infof("waiting until VFs %v are gone, attempt %v", filePath, attempt)
		time.Sleep(attemptDelay)
		files, _ := filepath.Glob(filePath)
		if len(files) != 0 {
			klog.V(5).Infof("found %v VFs still present", len(files))
		} else {
			klog.V(5).Infof("all VFs are gone after %v attempts", attempt)
			return nil
		}
	}

	klog.Errorf("timeout waiting for VFs to be disabled on device %v", pciDBDF)
	return fmt.Errorf("timeout waiting for VFs to be disabled on device %v", pciDBDF)
}

// removeAllVFsFromParents loops through devices with respective UIDs and
// removes all VFs from them.
func (d *driver) removeAllVFsFromParents(parentDevices []string) error {
	allErrorsStr := ""
	for _, parentUID := range parentDevices {
		err := d.removeAllVFs(d.state.allocatable[parentUID])
		if err != nil {
			allErrorsStr = fmt.Sprintf("%v; %v", allErrorsStr, err)
		}
	}
	if allErrorsStr != "" {
		return fmt.Errorf(allErrorsStr)
	}

	return nil
}

// removeAllVFs - calls sysfs to disable all VFs on a GPU.
// When called, no containers is supposed to be using VFs
// and VFs can be removed.
func (d *driver) removeAllVFs(parentDevice *DeviceInfo) error {
	pciDBDF := parentDevice.uid[:12]
	klog.V(5).Infof("removing all VFs for %v", pciDBDF)
	sriovNumvfsFile := path.Join(d.sysfsI915Dir, pciDBDF, "sriov_numvfs")

	numvfsInt := parentDevice.maxvfs
	numvfsBytes, err := os.ReadFile(sriovNumvfsFile)

	if err != nil { // Not critical, we will just have to unconfigure all VFs present.
		klog.Warning("could not read %v file: %v", sriovNumvfsFile, err)
	} else {
		numvfsInt, err = strconv.ParseUint(strings.TrimSpace(string(numvfsBytes)), 10, 64)
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
		klog.Error("(ignored) could not close file %v", sriovNumvfsFile)
		// Do not fail here, main job is done by now.
	}

	if err = d.waitUntilNoVFs(pciDBDF); err != nil {
		klog.Error("timed out waiting for VFs to be removed")
		return fmt.Errorf("failed removing VFs")
	}

	klog.V(5).Infof("cleaning up profiles for %v VFs", numvfsInt)

	sysfsVFsDir := fmt.Sprintf("%v/card%d/prelim_iov", d.sysfsDRMDir, parentDevice.cardidx)
	if err2 := sriov.CleanupManualConfigurationMaybe(sysfsVFsDir, numvfsInt); err2 != nil {
		klog.Error("failed to cleanup manually provisioned VFs: %v", err2)
		return fmt.Errorf("failed cleaning up VFs configuration: %v", err2)
	}

	klog.V(5).Infof("Successfully cleaned up all VFs for %v", parentDevice.uid)
	return nil
}

// validateVF ensures single VF is in place and DRM files for it are present.
func validateVF(drmDir string, cardregexp *regexp.Regexp) error {
	klog.V(5).Infof("checking DRM files in vf %v", drmDir)
	files, err := os.ReadDir(drmDir)
	if err != nil {
		klog.V(5).Infof("cannot read sysfs folder %v: %v", drmDir, err)
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

// validateVFs ensures that all new VFs were created and DRM devices are created, waits if needed.
func (d *driver) validateVFs(pciDBDF string, vfs map[string]*DeviceInfo) error {
	klog.V(5).Infof("validationg %v VFs creation on %v, ignoring profiles (NOT IMPLEMENTED)", len(vfs), pciDBDF)
	cardregexp := regexp.MustCompile(cardRE)
	attemptsLimit := 10
	attemptDelay := 1000 * time.Millisecond
	// Loop through sysfsI915Dir/pciDBDF/virtfn* symlinks, check drm/[cardX, renderDY].
	for _, vf := range vfs {
		klog.V(5).Infof("Validating vf %+v", vf)
		drmDir := path.Join(d.sysfsI915Dir, pciDBDF, fmt.Sprintf("virtfn%d/drm/", vf.pciVFIndex()))
		vfOK := false
		for attempt := 0; attempt < attemptsLimit; attempt++ {
			if err := validateVF(drmDir, cardregexp); err == nil {
				klog.V(5).Infof("vf %v of gpu %v is OK", vf.vfindex, pciDBDF)
				vfOK = true
				break
			} else {
				klog.V(5).Infof("vf %v of GPU %v is NOT OK. ERR: %v", vf.vfindex, pciDBDF, err)
			}
			time.Sleep(attemptDelay)
		}
		if !vfOK {
			klog.Errorf("vf %d of GPU %s is NOT OK, did not check the rest of new VFs", vf.vfindex, pciDBDF)
			return fmt.Errorf("vf %d of GPU %s is NOT OK, did not check the rest of new VFs", vf.vfindex, pciDBDF)
		}
	}
	return nil
}

// validateVFsToBeProvisioned iterates through the per-GPU lists of VFs to be provisioned and check
// if any list's VF indexing sequence is broken, e.g. pciDBDFs, vfIdx.
func validateVFsToBeProvisioned(toProvision map[string]map[string]*DeviceInfo) error {
	for parentUID, vfs := range toProvision {
		// Either all fairShare or all non-fairShare.
		fairShared := 0
		// Sequence should not be broken.
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
			return fmt.Errorf("VFs with fairshare profile cannot be provisioned together with specific profile VFs")
		}

	}
	return nil
}

// provisionVFs calls sysfs to tell KMD to create VFs.
// Should only work if no VFs exist at the moment on given GPU.
func (d *driver) provisionVFs(toProvision map[string]map[string]*DeviceInfo) (DevicesInfo, error) {
	klog.V(5).Infof("provisionVFs is called for %v GPUs.", len(toProvision))

	provisionedVFs := DevicesInfo{}

	for parentUID, vfs := range toProvision {
		pciDBDF := parentUID[:12]
		// At least as many VFs as requested has to be provisioned.
		// It could be possible to create more based on the maximum requested memory for VF.
		numvfs := len(vfs)
		klog.V(5).Infof("provisioning %v VFs for GPU %v ", numvfs, parentUID)

		if needToPreconfigureVFs(vfs) {
			sysfsVFsDir := fmt.Sprintf("%v/card%d/prelim_iov", d.sysfsDRMDir, d.state.allocatable[parentUID].cardidx)
			err := preConfigureVFs(sysfsVFsDir, vfs, d.state.allocatable[parentUID].eccOn)
			if err != nil {
				klog.Error("failed preconfiguring VFs, attempting to unconfigure them")
				// try to unconfigure VFs
				if !sriov.UnConfigureAllVFs(sysfsVFsDir) {
					klog.Error("failed to unconfigure VFs for GPU %v", parentUID)
				}

				return nil, fmt.Errorf("failed preconfiguring VFs")
			}
		}

		sriovNumvfsFile := path.Join(d.sysfsI915Dir, pciDBDF, "sriov_numvfs")

		fhandle, err := os.OpenFile(sriovNumvfsFile, os.O_APPEND|os.O_WRONLY, os.ModeAppend)
		if err != nil {
			klog.Errorf("failed to open %v file: %v", sriovNumvfsFile, err)
			return nil, fmt.Errorf("failed to open %v file: %v", sriovNumvfsFile, err)
		}

		_, err = fhandle.WriteString(fmt.Sprint(numvfs))
		if err != nil {
			klog.Errorf("could not write to file %v: %v", sriovNumvfsFile, err)
			return nil, fmt.Errorf("could not write to file %v: %v", sriovNumvfsFile, err)
		}

		if err = fhandle.Close(); err != nil {
			klog.Errorf("could not close file %v: %v", sriovNumvfsFile, err)
			// Do not fail here, main job is done by now.
		}

		// Wait for VFs and attempt to dismantle the VFs if DRM devices did not come up properly.
		if err2 := d.validateVFs(pciDBDF, vfs); err2 != nil {
			cleanupErr := d.removeAllVFs(d.state.allocatable[parentUID])
			if cleanupErr != nil {
				klog.Errorf("VFs cleanup failed for %v: %v", pciDBDF, cleanupErr)
				return nil, fmt.Errorf("failed to clean up VFs after failed provisioning: %v", cleanupErr)
			}
			return nil, fmt.Errorf("failed to validate provisioned VFs: %v, cleaned up successfully", err2)
		}
	}

	// If no errors - discover all new VFs.
	allDevices, err := enumerateAllPossibleDevices(d.sysfsI915Dir, d.sysfsDRMDir)
	if err != nil {
		return nil, fmt.Errorf("failed detecting supported devices: %v", err)
	}

	// Amount of provisioned VFs on device might be more than requested, announce all VFs, not only
	// requested.
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

// pickupMoreClaims searches for more VFs in other resourceClaimAllocation that potentially need
// provisioning on same GPU together with current claim, because SR-IOV config can only be
// set once.
func (d *driver) pickupMoreClaims(currentClaimUID string, toProvision map[string]map[string]*DeviceInfo) {
	for claimUID, ca := range d.gas.Spec.AllocatedClaims {
		if claimUID == currentClaimUID {
			continue
		}
		for _, device := range ca.Gpus {
			if device.Type == intelcrd.VfDeviceType {
				_, vfExists := d.state.allocatable[device.UID]
				_, affectedParent := toProvision[device.ParentUID]
				if !vfExists && affectedParent {
					klog.V(5).Infof("Picking VF %v for claim %v to be provisioned (was not requested yet)",
						device.UID,
						claimUID)
					toProvision[device.ParentUID][device.UID] = d.state.DeviceInfoFromAllocated(device)
				}
			}
		}
	}
}

// reuseLeftoverSRIOVResources checks whether there are any GPU resources left after the requested
// VFs would be provisioned, if so - attempts to make use of them as free, unallocated VFs
// to maximize GPU utilization.
// Added VFs will be provisioned and announced, but not immediately used. Instead,
// the driver will be able to allocate them, once suitable resource claim request arrives.
func (d *driver) reuseLeftoverSRIOVResources(toProvision map[string]map[string]*DeviceInfo) {
	klog.V(5).Infof("ReuseLeftoverSRIOVResources is called")
	for gpuUID, vfs := range toProvision {
		klog.V(5).Infof("trying to reuse leftovers of GPU %v", gpuUID)
		memoryLeftMiB := d.state.allocatable[gpuUID].memoryMiB

		// If all profiles are fair-share, controller has decided
		// to split all resources to specified VFs
		fairShareProfilesOnly := true
		// If all VFs have same profile - the amount of VFs can be
		// safely increased up to profile.numvfs.
		sameProfiles := true
		firstProfile := ""

		for _, vf := range vfs {
			memoryLeftMiB -= vf.memoryMiB
			if vf.vfprofile != sriov.FairShareProfile {
				fairShareProfilesOnly = false
			}
			if firstProfile == "" {
				firstProfile = vf.vfprofile
			} else if vf.vfprofile != firstProfile {
				sameProfiles = false
			}
		}

		// With all VFs being fairShare profile, no leftovers are expected on this GPU;
		// KMD will split the GPU resources equally among the requested VFs.
		if fairShareProfilesOnly {
			klog.V(5).Infof("only fairShare profiles present, nothing to do")

			continue
		}

		// Only add more of same-profile VFs up to profile.numvfs if GPU has same maxvfs configured.
		// In case the GPU has lower limit of sriov_totalvfs configured, not all VFs of same profile
		// will be provisioned which will result in resources idling. For this case to maximize
		// resources utilization the VFs have to be of different profile.
		if sameProfiles && d.state.allocatable[gpuUID].maxvfs >= sriov.Profiles[firstProfile]["numvfs"] {
			klog.V(5).Infof("all the requested VFs have the same profile, adding more same-profile VFs to reuse remaining resources")
			for uint64(len(vfs)) < sriov.Profiles[firstProfile]["numvfs"] {
				addVFWithProfile(vfs, gpuUID, firstProfile)
			}

			continue
		}

		klog.V(5).Infof("trying to add VFs with different profiles")
		addCustomVFs(vfs, d.state.allocatable[gpuUID], memoryLeftMiB)
	}
}

// addCustomVFs tries to find the default memory amount the VF should have.  It splits leftover
// resources into VFs that can be utilized by future workloads, and adds these VFs into the map for
// provisioning.
func addCustomVFs(vfs map[string]*DeviceInfo, parentDeviceInfo *DeviceInfo, memoryLeftMiB uint64) {
	doorbellsLeft := sriov.GetMaximumVFDoorbells(parentDeviceInfo.model)
	klog.V(5).Infof("device %v total doorbells: %v", parentDeviceInfo.model, doorbellsLeft)
	for _, vf := range vfs {
		doorbellsLeft -= sriov.GetProfileDoorbells(vf.vfprofile)
	}
	klog.V(5).Infof("device %v doorbells left: %v", parentDeviceInfo.model, doorbellsLeft)

	// The set of VFs is heterogeneous, need to find out the best split of leftover resources.

	deviceId := parentDeviceInfo.model
	minVFMemoryMiB, _ := sriov.GetMimimumVFMemorySizeMiB(deviceId, parentDeviceInfo.eccOn)
	minVFDoorbells := sriov.GetMimimumVFDoorbells(deviceId)

	modelProfileNames := sriov.PerDeviceIdProfiles[deviceId]

	// If the leftover resource is too small even for smallest VF profile,
	// then we have nothing to do.
	if memoryLeftMiB <= minVFMemoryMiB {
		return
	}

	// Get current GPU's default VF memory amount and profile for this cluster from config map
	// or fallback to driver defaults.
	vfMemMiB, vfProfileName, err := getGpuVFDefaults(deviceId, parentDeviceInfo.eccOn)
	if err != nil {
		klog.Errorf("could not get default VF memory size: %v", err)
		return
	}
	vfDoorbells := sriov.GetProfileDoorbells(vfProfileName)

	klog.V(5).Infof("adding VFs until leftover memory is less than %v and doorbells less than %v", minVFMemoryMiB, minVFDoorbells)

	// Generate new VFs until even the smallest VF won't fit any longer.
	for memoryLeftMiB > minVFMemoryMiB && doorbellsLeft > minVFDoorbells {
		klog.V(5).Infof("gpu %v has %v memory left, %v doorbells", parentDeviceInfo.uid, memoryLeftMiB, doorbellsLeft)
		for memoryLeftMiB > vfMemMiB && doorbellsLeft > vfDoorbells {
			addVFWithProfile(vfs, parentDeviceInfo.uid, vfProfileName)
			memoryLeftMiB -= vfMemMiB
			doorbellsLeft -= vfDoorbells
		}

		// Find next suitable profile.
		for profileIdx, profileName := range modelProfileNames {
			if profileName == vfProfileName {
				klog.V(5).Infof("found current profile %v", profileName)
				// Now iterate over smaller VF profiles to see if any of them still fit.
				for _, nextProfileName := range modelProfileNames[profileIdx:] {
					// err does not need to be checked because the loop is through unit-tested
					// list of profiles that are guaranteed to exist.
					profileLmemQuotaMiB, _ := sriov.GetProfileLmemQuotaMiB(nextProfileName, parentDeviceInfo.eccOn)
					profileDoorbells := sriov.GetProfileDoorbells(nextProfileName)
					klog.V(5).Infof("checking profile %v", nextProfileName)
					if memoryLeftMiB > profileLmemQuotaMiB && doorbellsLeft > profileDoorbells {
						vfProfileName = nextProfileName
						vfMemMiB = profileLmemQuotaMiB
						vfDoorbells = profileDoorbells
						klog.V(5).Infof("picking profile %v", nextProfileName)
						break
					}
				}
				break
			}
		}
	}
}

func addVFWithProfile(vfs map[string]*DeviceInfo, gpuUID string, vfProfileName string) {
	vfIndex := uint64(len(vfs))
	newVFUID := fmt.Sprintf("%s-vf%d", gpuUID, vfIndex)
	klog.V(5).Infof("Adding new VF %v with profile %v on device %v", newVFUID, vfProfileName, gpuUID)

	newVF := &DeviceInfo{
		deviceType: intelcrd.VfDeviceType,
		uid:        newVFUID,
		vfprofile:  vfProfileName,
		parentuid:  gpuUID,
		vfindex:    vfIndex,
	}
	vfs[newVFUID] = newVF
}

var deviceToModelMap = map[string]string{
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

// getGpuVFDefaults tries to read default amount of local memory the VF
// should have mounted from config-map. If it fails, then it tries to get
// the hardcoded default from the SR-IOV profiles package.
func getGpuVFDefaults(deviceId string, eccOn bool) (uint64, string, error) {
	var vfMemMiB uint64

	vfMemMiB, err := getDefaultVFMemoryFromConfigMap(vfMemConfigFile, deviceId, eccOn)
	if err != nil {
		klog.V(5).Infof("could not get VF default memory size from config map: %v", err)

		vfMemMiB, profileName, err := sriov.GetVFDefaults(deviceId, eccOn)
		if err != nil {
			klog.V(5).Infof("could not get default profile VF memory: %v", err)
			return 0, "", fmt.Errorf("could not get default profile VF memory: %v", err)
		}

		return vfMemMiB, profileName, nil
	}

	// PickVFProfile has to be called in case defaultVFSize got the memory amount
	// from config map and not from hardcoded driver defaults.
	vfMemMiB, profileName, err := sriov.PickVFProfile(deviceId, vfMemMiB, eccOn)
	if err != nil {
		klog.Error("failed getting suitable profile for device %d with %d MiB memory. Err: %v", deviceId, vfMemMiB, err)
		return 0, "", fmt.Errorf("failed picking VF profile: %v", err)
	}

	return vfMemMiB, profileName, nil
}

func getDefaultVFMemoryFromConfigMap(vfMemConfigFile string, deviceId string, eccOn bool) (uint64, error) {
	model, found := deviceToModelMap[deviceId]
	if !found {
		klog.V(5).Infof("could not find device model by PCI ID %v", deviceId)
		return 0, fmt.Errorf("unsupported device %v", deviceId)
	}

	vfMemConfigBytes, err := os.ReadFile(vfMemConfigFile)
	if err != nil {
		klog.V(5).Infof("could not read default VF memory configuration from file %v. Err: %v", vfMemConfigFile, err)
		return 0, fmt.Errorf("failed reading file %v. Err: %v", vfMemConfigFile, err)
	}

	// Try to parse the config map contents into map.
	var perDeviceDefaultVFSize map[string]uint64
	if err := json.Unmarshal(vfMemConfigBytes, &perDeviceDefaultVFSize); err != nil {
		klog.V(5).Infof("Could not parse default VF memory configuration from file %v. Err: %v", vfMemConfigFile, err)
		return 0, fmt.Errorf("failed parsing file %v. Err: %v", vfMemConfigFile, err)
	}

	// Finally, get VF memory amount from cluster configuration (config-map).
	vfMemMiB, found := perDeviceDefaultVFSize[model]
	if !found {
		klog.V(5).Infof("could not find default VF memory configuration for model %v (%v)", model, deviceId)
		return 0, fmt.Errorf("no data for model %v (device ID %v)", model, deviceId)
	}

	// Sanitize the value before returning it
	if !sriov.SanitizeLmemQuotaMiB(deviceId, eccOn, vfMemMiB) {
		return 0, fmt.Errorf("misconfigured vf-memory config")
	}

	return vfMemMiB, nil
}

// preConfigureVFs loops through vfs map and calls preconfiguration of given profile
// for manual provisioning mode, in case fair share is not suitable.
// pf/auto_provisioning will be automatically set to 0 in this case.
func preConfigureVFs(sysfsVFsDir string, vfs map[string]*DeviceInfo, eccOn bool) error {
	for _, vf := range vfs {
		klog.V(5).Infof("preconfiguring VF %v on GPU %v", vf.vfindex, vf.parentuid)
		if err := sriov.PreConfigureVF(sysfsVFsDir, vf.drmVFIndex(), vf.vfprofile, eccOn); err != nil {
			return fmt.Errorf("failed preconfiguring vf %v: %v", vf.uid, err)
		}
	}

	return nil
}
