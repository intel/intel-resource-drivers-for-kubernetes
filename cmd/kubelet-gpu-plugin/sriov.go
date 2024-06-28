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

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/discovery"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/sriov"
	intelcrd "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gpu/v1alpha2/api"
	"k8s.io/klog/v2"
)

const (
	attemptsLimitBase = 10
	attemptDelay      = 1000 * time.Millisecond
	vfMemConfigFile   = "/defaults/vf-memory.config"
)

// waitUntilNoVFs waits until timeout for all virtfnX symlinks to be gone from parent pci device.
func (d *driver) waitUntilNoVFs(pciAddress string) error {
	filePath := path.Join(d.sysfsDir, device.SysfsI915path, pciAddress, "virtfn*")

	for attempt := 0; attempt < attemptsLimitBase; attempt++ {
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

	return fmt.Errorf("timeout waiting for VFs to be disabled on device")
}

// removeAllVFsFromParents loops through devices with respective UIDs and
// removes all VFs from them.
func (d *driver) removeAllVFsFromParents(parentDevices []string) error {
	allErrorsStr := ""
	for _, parentUID := range parentDevices {
		err := d.removeAllVFs(d.state.allocatable[parentUID])
		if err != nil {
			if allErrorsStr != "" {
				allErrorsStr = fmt.Sprintf("%v; ", allErrorsStr)
			}
			allErrorsStr = fmt.Sprintf("%v%v: %v", allErrorsStr, parentUID, err)
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
func (d *driver) removeAllVFs(parentDevice *device.DeviceInfo) error {
	klog.V(5).Infof("removing all VFs for %v", parentDevice.PCIAddress)
	sriovNumvfsFile := path.Join(d.sysfsDir, device.SysfsI915path, parentDevice.PCIAddress, "sriov_numvfs")

	numvfsInt := parentDevice.MaxVFs
	numvfsBytes, err := os.ReadFile(sriovNumvfsFile)

	if err != nil { // Not critical, we will just have to unconfigure all VFs present.
		klog.Warningf("could not read %v file: %v", sriovNumvfsFile, err)
	} else {
		numvfsInt, err = strconv.ParseUint(strings.TrimSpace(string(numvfsBytes)), 10, 64)
		if err != nil {
			klog.Errorf("could not convert numvfs to int: %v", err)
		}
		klog.V(5).Infof("num_vfs had value %v", numvfsInt)
	}

	fhandle, err := os.OpenFile(sriovNumvfsFile, os.O_APPEND|os.O_WRONLY, os.ModeAppend)

	if err != nil {
		return fmt.Errorf("failed to open %v file", sriovNumvfsFile)
	}

	_, err = fhandle.WriteString("0")
	if err != nil {
		return fmt.Errorf("could not write to file %v", sriovNumvfsFile)
	}

	if err = fhandle.Close(); err != nil {
		klog.Errorf("(ignored) could not close file %v", sriovNumvfsFile)
		// Do not fail here, main job is done by now.
	}

	if err = d.waitUntilNoVFs(parentDevice.PCIAddress); err != nil {
		return fmt.Errorf("failed removing VFs: %v", err)
	}

	klog.V(5).Infof("cleaning up profiles for %v VFs", numvfsInt)

	sysfsVFsDir := path.Join(d.sysfsDir, device.SysfsDRMpath, fmt.Sprintf("card%d/prelim_iov", parentDevice.CardIdx))
	if err2 := sriov.CleanupManualConfigurationMaybe(sysfsVFsDir, numvfsInt); err2 != nil {

		return fmt.Errorf("failed cleaning up VFs configuration: %v", err2)
	}

	klog.V(5).Infof("Successfully cleaned up all VFs for %v", parentDevice.UID)
	return nil
}

// validateVF ensures single VF is in place and DRM files for it are present.
func validateVF(drmDir string) error {
	klog.V(5).Infof("checking DRM files in vf %v", drmDir)
	files, err := os.ReadDir(drmDir)
	if err != nil {
		klog.V(5).Infof("cannot read sysfs folder %v: %v", drmDir, err)
		return fmt.Errorf("cannot read sysfs folder %v: %v", drmDir, err)
	}

	cardFound := false
	for _, filename := range files {
		klog.V(5).Infof("checking file %v", filename.Name())
		if device.CardRegexp.MatchString(filename.Name()) {
			klog.V(5).Info("found cardX file")
			cardFound = true
		}
	}
	if !cardFound {
		klog.Errorf("could not find DRM cardX device in %v", drmDir)
		return fmt.Errorf("could not find DRM cardX device in %v", drmDir)
	}

	return nil // TODO += deviceId
}

// validateVFs ensures that all new VFs were created and DRM devices are created, waits if needed.
// vf.model has to be set for new UID to be set.
func (d *driver) validateVFs(pciAddress string, vfs []*device.DeviceInfo) error {
	klog.V(5).Infof("validationg %v VFs creation on %v, ignoring profiles (NOT IMPLEMENTED)", len(vfs), pciAddress)

	attemptsLimit := attemptsLimitBase + len(vfs)
	attempt := 0
	// Loop through sysfsI915Dir/<pciAddress>/virtfn* symlinks, check drm/[cardX, renderDY].
	for _, vf := range vfs {
		klog.V(5).Infof("Validating vf %v on device %v", vf.VFIndex, vf.ParentUID)
		virtfnLinkPath := path.Join(d.sysfsDir, device.SysfsI915path, pciAddress, fmt.Sprintf("virtfn%d", vf.VFIndex))
		drmDir := path.Join(virtfnLinkPath, "drm")
		vfOK := false
		for ; attempt < attemptsLimit; attempt++ {
			if err := validateVF(drmDir); err == nil {
				klog.V(5).Infof("vf %v on GPU %v is OK", vf.VFIndex, pciAddress)

				newPciAddress, err := newVFpciAddress(virtfnLinkPath)
				if err != nil {
					return fmt.Errorf("cannot get new VF PCI address: %v", err)
				}

				vf.UID = device.DeviceUIDFromPCIinfo(newPciAddress, vf.Model)
				klog.V(5).Infof("New UID for vf %v on GPU %v is %v", vf.VFIndex, vf.ParentUID, vf.UID)
				vfOK = true
				break
			} else {
				klog.V(5).Infof("vf %v of GPU %v is NOT OK on attempt %v. ERR: %v", vf.VFIndex, pciAddress, attempt, err)
			}
			time.Sleep(attemptDelay)
		}
		if !vfOK {
			klog.Errorf("vf %d of GPU %s is NOT OK, did not check the rest of new VFs", vf.VFIndex, pciAddress)
			return fmt.Errorf("vf %d of GPU %s is NOT OK, did not check the rest of new VFs", vf.VFIndex, pciAddress)
		}
	}
	return nil
}

func newVFpciAddress(virtfnPath string) (string, error) {
	virtfnTarget, err := os.Readlink(virtfnPath)
	if err != nil {
		return "", fmt.Errorf("failed reading virtfn symlink %v: %v", virtfnPath, err)
	}

	// ../0000-00-02-1  # 15 chars
	if len(virtfnTarget) != 15 {
		return "", fmt.Errorf("symlink target does not match expected length: %v", virtfnTarget)
	}

	targetPciAddress := virtfnTarget[3:]
	if !device.PciRegexp.MatchString(targetPciAddress) {
		return "", fmt.Errorf("symlink target does not match PCI address pattern: %v", virtfnTarget)
	}

	return targetPciAddress, nil
}

// validateVFsToBeProvisioned iterates through the per-GPU lists of VFs to be provisioned
// and checks for profiles to be either all FairShare, or none to be FairShare.
func (d *driver) validateVFsToBeProvisioned(toProvision map[string][]*device.DeviceInfo) error {
	for _, vfsList := range toProvision {
		// Either all fairShare or all non-fairShare.
		fairShared := 0

		vfIndexes := map[uint64]bool{}
		for _, vf := range vfsList {
			if vf.VFProfile == sriov.FairShareProfile {
				fairShared++
			}
			if _, found := vfIndexes[vf.VFIndex]; found {
				return fmt.Errorf("one or more allocated VFs have same VF index")
			}
			vfIndexes[vf.VFIndex] = true
		}

		if fairShared != len(vfsList) && fairShared != 0 {
			return fmt.Errorf("VFs with fairshare profile cannot be provisioned together with specific profile VFs")
		}
	}

	if !d.state.parentCanHaveVFs(toProvision) {
		return fmt.Errorf("one or more parent devices cannot have VFs")
	}

	return nil
}

// provisionVFs calls sysfs to tell KMD to create VFs.
// Should only work if no VFs exist at the moment on given GPU.
// Returns newly provisioned VFs as map of DeviceInfo.
func (d *driver) provisionVFs(toProvision map[string][]*device.DeviceInfo) (device.DevicesInfo, error) {
	klog.V(5).Infof("provisionVFs is called for %v GPUs.", len(toProvision))

	provisionedVFs := device.DevicesInfo{}

	for parentUID, vfs := range toProvision {
		pciAddress, _ := device.PciInfoFromDeviceUID(parentUID)
		// At least as many VFs as requested has to be provisioned.
		// It could be possible to create more based on the maximum requested memory for VF.
		numvfs := len(vfs)
		klog.V(5).Infof("provisioning %v VFs for GPU %v ", numvfs, parentUID)

		if needToPreconfigureVFs(vfs) {
			sysfsVFsDir := path.Join(d.sysfsDir, device.SysfsDRMpath, fmt.Sprintf("card%d/prelim_iov", d.state.allocatable[parentUID].CardIdx))
			err := preConfigureVFs(sysfsVFsDir, vfs, d.state.allocatable[parentUID].EccOn)
			if err != nil {
				klog.Error("failed preconfiguring VFs, attempting to unconfigure them")

				// try to unconfigure all needed VFs, in case there was unexpected configuration in them
				cleanupErr := sriov.CleanupManualConfigurationMaybe(sysfsVFsDir, uint64(numvfs))
				if cleanupErr != nil {
					return nil, fmt.Errorf("failed to unconfigure VFs for GPU %v. Err: %v", parentUID, cleanupErr)
				}

				return nil, fmt.Errorf("failed preconfiguring VFs: %v", err)
			}
		}

		sriovNumvfsFile := path.Join(d.sysfsDir, device.SysfsI915path, pciAddress, "sriov_numvfs")

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
		if err2 := d.validateVFs(pciAddress, vfs); err2 != nil {
			cleanupErr := d.removeAllVFs(d.state.allocatable[parentUID])
			if cleanupErr != nil {
				klog.Errorf("VFs cleanup failed for %v: %v", pciAddress, cleanupErr)
				return nil, fmt.Errorf("failed to clean up VFs after failed provisioning: %v", cleanupErr)
			}
			return nil, fmt.Errorf("failed to validate provisioned VFs: %v, cleaned up successfully", err2)
		}
	}

	// If no errors - discover all new VFs.
	allDevices := discovery.DiscoverDevices(d.sysfsDir)

	// Amount of provisioned VFs on device might be more than requested, announce all VFs, not only
	// requested.
	for duid, device := range allDevices {
		if device.DeviceType == intelcrd.VfDeviceType {
			if _, wasRequested := toProvision[device.ParentUID]; wasRequested {
				provisionedVFs[duid] = device
			}
		}
	}

	return provisionedVFs, nil
}

func needToPreconfigureVFs(vfs []*device.DeviceInfo) bool {
	for _, vf := range vfs {
		if vf.VFProfile != sriov.FairShareProfile {
			return true
		}
	}
	return false
}

// pickupMoreClaims searches for more VFs in other resourceClaimAllocation that potentially need
// provisioning on same GPU together with current claim, because SR-IOV config can only be
// set once.
func (d *driver) pickupMoreClaims(currentClaimUID string, toProvision map[string][]*device.DeviceInfo, perClaimDevices map[string][]*device.DeviceInfo) {
	for claimUID, ca := range d.gas.Spec.AllocatedClaims {
		if claimUID == currentClaimUID {
			continue
		}
		for _, gpu := range ca.Gpus {
			if gpu.Type == intelcrd.VfDeviceType {
				_, vfExists := d.state.allocatable[gpu.UID]
				_, affectedParent := toProvision[gpu.ParentUID]
				if !vfExists && affectedParent {
					klog.V(5).Infof("Picking VF %v for claim %v to be provisioned (was not requested yet)",
						gpu.UID,
						claimUID)
					newDevice := d.state.DeviceInfoFromAllocated(gpu)
					newDevice.VFIndex = uint64(len(toProvision[gpu.ParentUID]))
					toProvision[gpu.ParentUID] = append(toProvision[gpu.ParentUID], newDevice)
					if perClaimDevices[claimUID] == nil {
						perClaimDevices[claimUID] = []*device.DeviceInfo{}
					}
					perClaimDevices[claimUID] = append(perClaimDevices[claimUID], newDevice)
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
func (d *driver) reuseLeftoverSRIOVResources(toProvision map[string][]*device.DeviceInfo) {
	klog.V(5).Info("ReuseLeftoverSRIOVResources is called")
	for gpuUID, gpuVFs := range toProvision {
		klog.V(5).Infof("trying to reuse leftovers of GPU %v", gpuUID)
		memoryLeftMiB := d.state.allocatable[gpuUID].MemoryMiB

		// If all profiles are fair-share, controller has decided
		// to split all resources to specified VFs
		fairShareProfilesOnly := true
		// If all VFs have same profile - the amount of VFs can be
		// safely increased up to profile.numvfs.
		sameProfiles := true
		firstProfile := gpuVFs[0].VFProfile

		for _, vf := range gpuVFs {
			memoryLeftMiB -= vf.MemoryMiB
			if vf.VFProfile != sriov.FairShareProfile {
				fairShareProfilesOnly = false
			}

			if vf.VFProfile != firstProfile {
				sameProfiles = false
			}
		}

		// With all VFs being fairShare profile, no leftovers are expected on this GPU;
		// KMD will split the GPU resources equally among the requested VFs.
		if fairShareProfilesOnly {
			klog.V(5).Info("only fairShare profiles present, nothing to do")

			continue
		}

		// Only add more of same-profile VFs up to profile.numvfs if GPU has same maxvfs configured.
		// In case the GPU has lower limit of sriov_totalvfs configured, not all VFs of same profile
		// will be provisioned which will result in resources idling. For this case to maximize
		// resources utilization the VFs have to be of different profile.
		if sameProfiles && d.state.allocatable[gpuUID].MaxVFs >= sriov.Profiles[firstProfile]["numvfs"] {
			klog.V(5).Info("all the requested VFs have the same profile, adding more same-profile VFs to reuse remaining resources")
			for uint64(len(gpuVFs)) < sriov.Profiles[firstProfile]["numvfs"] {
				newVFIndex := smallestFreeVFIndex(gpuVFs)
				newVF := newVFWithProfile(newVFIndex, gpuUID, firstProfile, d.state.allocatable[gpuUID].Model)
				gpuVFs = append(gpuVFs, newVF)
			}
			toProvision[gpuUID] = gpuVFs
			continue
		}

		klog.V(5).Info("trying to add VFs with different profiles")
		newVFs := newCustomVFs(gpuVFs, d.state.allocatable[gpuUID], memoryLeftMiB)
		toProvision[gpuUID] = append(gpuVFs, newVFs...)
	}
}

// newCustomVFs tries to find the default memory amount the VF should have. It splits leftover
// resources into VFs that can be utilized by future workloads, and adds these VFs into the map for
// provisioning.
func newCustomVFs(vfs []*device.DeviceInfo, parentDeviceInfo *device.DeviceInfo, memoryLeftMiB uint64) []*device.DeviceInfo {
	newVFs := []*device.DeviceInfo{}
	doorbellsLeft := sriov.GetMaximumVFDoorbells(parentDeviceInfo.Model)
	klog.V(5).Infof("device %v total doorbells: %v", parentDeviceInfo.Model, doorbellsLeft)
	for _, vf := range vfs {
		doorbellsLeft -= sriov.GetProfileDoorbells(vf.VFProfile)
	}
	klog.V(5).Infof("device %v doorbells left: %v", parentDeviceInfo.Model, doorbellsLeft)

	// The set of VFs is heterogeneous, need to find out the best split of leftover resources.

	deviceId := parentDeviceInfo.Model
	minVFMemoryMiB, _ := sriov.GetMimimumVFMemorySizeMiB(deviceId, parentDeviceInfo.EccOn)
	minVFDoorbells := sriov.GetMimimumVFDoorbells(deviceId)

	modelProfileNames := sriov.PerDeviceIdProfiles[deviceId]

	// If the leftover resource is too small even for smallest VF profile,
	// then we have nothing to do.
	if memoryLeftMiB <= minVFMemoryMiB {
		return newVFs
	}

	// Get current GPU's default VF memory amount and profile for this cluster from config map
	// or fallback to driver defaults.
	vfMemMiB, _, vfProfileName, err := getGpuVFDefaults(deviceId, parentDeviceInfo.EccOn)
	if err != nil {
		klog.Errorf("could not get default VF memory size: %v", err)
		return newVFs
	}
	vfDoorbells := sriov.GetProfileDoorbells(vfProfileName)

	klog.V(5).Infof("adding VFs until leftover memory is less than %v and doorbells less than %v", minVFMemoryMiB, minVFDoorbells)

	allVFs := vfs
	// Generate new VFs until even the smallest VF won't fit any longer.
	for memoryLeftMiB > minVFMemoryMiB && doorbellsLeft > minVFDoorbells {
		klog.V(5).Infof("gpu %v has %v memory left, %v doorbells", parentDeviceInfo.UID, memoryLeftMiB, doorbellsLeft)
		for memoryLeftMiB > vfMemMiB && doorbellsLeft > vfDoorbells {
			newVFIndex := smallestFreeVFIndex(allVFs)
			newVF := newVFWithProfile(newVFIndex, parentDeviceInfo.UID, vfProfileName, parentDeviceInfo.Model)
			newVFs = append(newVFs, newVF)
			allVFs = append(allVFs, newVF) // just to track VF indexes
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
					profileMemoryMiB, _ := sriov.ProfileMemoryMiB(nextProfileName, parentDeviceInfo.EccOn)
					profileDoorbells := sriov.GetProfileDoorbells(nextProfileName)
					klog.V(5).Infof("checking profile %v", nextProfileName)
					if memoryLeftMiB > profileMemoryMiB && doorbellsLeft > profileDoorbells {
						vfProfileName = nextProfileName
						vfMemMiB = profileMemoryMiB
						vfDoorbells = profileDoorbells
						klog.V(5).Infof("picking profile %v", nextProfileName)
						break
					}
				}
				break
			}
		}
	}

	return newVFs
}

// smallestFreeVFIndex checks vfindex values in newVFs and returns smallest unused Vf index.
// newVFs are not guaranteed to be ordered or with sequential Vf indexes.
func smallestFreeVFIndex(newVFs []*device.DeviceInfo) int {
	for newIdx := 0; ; newIdx++ {
		found := false
		for _, vf := range newVFs {
			if vf.VFIndex == uint64(newIdx) {
				found = true
				break
			}
		}
		if !found {
			return newIdx
		}
	}
}

func newVFWithProfile(vfIndex int, gpuUID string, vfProfileName string, model string) *device.DeviceInfo {
	klog.V(5).Infof("Adding new VF #%v with profile %v on device %v", vfIndex, vfProfileName, gpuUID)

	newVF := &device.DeviceInfo{
		DeviceType: intelcrd.VfDeviceType,
		VFProfile:  vfProfileName,
		ParentUID:  gpuUID,
		VFIndex:    uint64(vfIndex),
		Model:      model,
	}

	return newVF
}

// getGpuVFDefaults tries to read default amount of local memory the VF
// should have mounted from config-map. If it fails, then it tries to get
// the hardcoded default from the SR-IOV profiles package.
func getGpuVFDefaults(deviceId string, eccOn bool) (uint64, uint64, string, error) {
	var vfMemMiB uint64

	vfMemMiB, err := getDefaultVFMemoryFromConfigMap(vfMemConfigFile, deviceId, eccOn)
	if err != nil {
		klog.V(5).Infof("could not get VF default memory size from config map: %v", err)

		vfMemMiB, vfMillicores, profileName, err := sriov.GetVFDefaults(deviceId, eccOn)
		if err != nil {
			klog.V(5).Infof("could not get default profile VF memory: %v", err)
			return 0, 0, "", fmt.Errorf("could not get default profile VF memory: %v", err)
		}

		return vfMemMiB, vfMillicores, profileName, nil
	}

	// PickVFProfile has to be called in case defaultVFSize got the memory amount
	// from config map and not from hardcoded driver defaults.
	vfMemMiB, vfMillicores, profileName, err := sriov.PickVFProfile(deviceId, vfMemMiB, 0, eccOn)
	if err != nil {
		klog.Errorf("failed getting suitable profile for device %v with %d MiB memory. Err: %v", deviceId, vfMemMiB, err)
		return 0, 0, "", fmt.Errorf("failed picking VF profile: %v", err)
	}

	return vfMemMiB, vfMillicores, profileName, nil
}

func getDefaultVFMemoryFromConfigMap(vfMemConfigFile string, deviceId string, eccOn bool) (uint64, error) {
	model, found := device.SRIOVDeviceToModelMap[deviceId]
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
func preConfigureVFs(sysfsVFsDir string, vfs []*device.DeviceInfo, eccOn bool) error {
	for _, vf := range vfs {

		klog.V(5).Infof("preconfiguring VF %v on GPU %v", vf.VFIndex, vf.ParentUID)
		if err := sriov.PreConfigureVF(sysfsVFsDir, vf.DrmVFIndex(), vf.VFProfile, eccOn); err != nil {
			return fmt.Errorf("failed preconfiguring vf #%v: %v", vf.VFIndex, err)
		}
	}

	return nil
}
