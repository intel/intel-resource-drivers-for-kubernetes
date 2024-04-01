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
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"k8s.io/klog/v2"

	cdiapi "github.com/container-orchestrated-devices/container-device-interface/pkg/cdi"
	specs "github.com/container-orchestrated-devices/container-device-interface/specs-go"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gpu/v1alpha2"
	intelcrd "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gpu/v1alpha2/api"
)

// DeviceInfo is an internal structure type to store info about discovered device.
type DeviceInfo struct {
	UID        string `json:"uid"`        // unique identifier, pci_DBDF-pci_device_id
	Model      string `json:"model"`      // pci_device_id
	CardIdx    uint64 `json:"cardidx"`    // card device number (e.g. 0 for /dev/dri/card0)
	RenderdIdx uint64 `json:"renderdidx"` // renderD device number (e.g. 128 for /dev/dri/renderD128)
	MemoryMiB  uint64 `json:"memorymib"`  // in MiB
	Millicores uint64 `json:"millicores"` // [0-1000] where 1000 means whole GPU.
	DeviceType string `json:"devicetype"` // gpu, vf, any
	MaxVFs     uint64 `json:"maxvfs"`     // if enabled, non-zero maximum amount of VFs
	ParentUID  string `json:"parentuid"`  // uid of gpu device where VF is
	VFProfile  string `json:"vfprofile"`  // name of the SR-IOV profile
	VFIndex    uint64 `json:"vfindex"`    // 0-based PCI index of the VF on the GPU, DRM indexing starts with 1
	EccOn      bool   `json:"eccon"`      // true of ECC is enabled, false otherwise
}

func (g *DeviceInfo) DeepCopy() *DeviceInfo {
	di := *g
	return &di
}

func (g *DeviceInfo) drmVFIndex() uint64 {
	return g.VFIndex + 1
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

// ClaimAllocations maps a slice of allocated DeviceInfos to Claim.Uid.
type ClaimAllocations map[string][]*DeviceInfo
type ClaimPreparations map[string][]*DeviceInfo

type nodeState struct {
	sync.Mutex
	cdi         cdiapi.Registry
	allocatable DevicesInfo
	prepared    ClaimPreparations
}

func (g DeviceInfo) CDIName() string {
	return fmt.Sprintf("%s=%s", cdiKind, g.UID)
}

func newNodeState(gas *intelcrd.GpuAllocationState, detectedDevices map[string]*DeviceInfo, cdiRoot string, preparedClaimFilePath string) (*nodeState, error) {
	for ddev := range detectedDevices {
		klog.V(3).Infof("new device: %+v", ddev)
	}

	klog.V(5).Info("Getting CDI registry")
	cdi := cdiapi.GetRegistry(
		cdiapi.WithSpecDirs(cdiRoot),
	)

	klog.V(5).Info("Got CDI registry, refreshing it")
	err := cdi.Refresh()
	if err != nil {
		return nil, fmt.Errorf("unable to refresh the CDI registry: %v", err)
	}

	// syncDetectedDevicesWithCdiRegistry overrides uid in detecteddevices from existing cdi spec
	err = syncDetectedDevicesWithCdiRegistry(cdi, detectedDevices, true)
	if err != nil {
		return nil, fmt.Errorf("unable to sync detected devices to CDI registry: %v", err)
	}
	err = cdi.Refresh()
	if err != nil {
		return nil, fmt.Errorf("unable to refresh the CDI registry after populating it: %v", err)
	}

	klog.V(5).Info("Allocatable after CDI refresh device:")
	for duid, ddev := range detectedDevices {
		klog.V(5).Infof("CDI device: %v : %+v", duid, ddev)
	}

	klog.V(5).Info("Creating NodeState")
	// TODO: allocatable should include cdi-described
	state := &nodeState{
		cdi:         cdi,
		allocatable: detectedDevices,
		prepared:    make(ClaimPreparations),
	}

	preparedClaims, err := state.getOrCreatePreparedClaims(preparedClaimFilePath)
	if err != nil {
		klog.Errorf("Error getting prepared claims: %v", err)
		return nil, fmt.Errorf("failed to get prepared claims: %v", err)
	}

	klog.V(5).Info("Syncing allocatable devices")
	err = state.syncPreparedGpusFromFile(preparedClaims)
	if err != nil {
		return nil, fmt.Errorf("unable to sync allocated devices from GpuAllocationState: %v", err)
	}
	klog.V(5).Infof("Synced state with CDI and GpuAllocationState: %+v", state)
	for duid, ddev := range state.allocatable {
		klog.V(5).Infof("Allocatable device: %v : %+v", duid, ddev)
	}

	return state, nil
}

// Add detected devices into cdi registry if they are not yet there.
// Update existing registry devices with detected.
// Remove absent registry devices.
func syncDetectedDevicesWithCdiRegistry(registry cdiapi.Registry, detectedDevices DevicesInfo, doCleanup bool) error {

	vendorSpecs := registry.SpecDB().GetVendorSpecs(cdiVendor)
	devicesToAdd := detectedDevices.DeepCopy()

	if len(vendorSpecs) == 0 {
		klog.V(5).Infof("No existing specs found for vendor %v, creating new", cdiVendor)
		if err := addNewDevicesToNewRegistry(devicesToAdd); err != nil {
			klog.V(5).Infof("Failed adding card to cdi registry: %v", err)
			return err
		}
		return nil
	}

	// loop through spec devices
	// - remove from CDI those not detected
	// - update with card and renderD indexes
	//   - delete from detected so they are not added as duplicates
	// - write spec
	// add rest of detected devices to first vendor spec
	for specidx, vendorSpec := range vendorSpecs {
		klog.V(5).Infof("checking vendorspec %v", specidx)

		specChanged := false // if devices were updated or deleted
		filteredDevices := []specs.Device{}

		for specDeviceIdx, specDevice := range vendorSpec.Devices {
			klog.V(5).Infof("checking device %v: %v", specDeviceIdx, specDevice)

			// if matched detected - check and update cardIdx and renderDIdx if needed - add to filtered Devices
			if detectedDevice, found := devicesToAdd[specDevice.Name]; found {

				if syncDeviceNodes(specDevice, detectedDevice, cardRegexp, renderdRegexp) {
					specChanged = true
				}

				filteredDevices = append(filteredDevices, specDevice)
				// Regardless if we needed to update the existing device or not,
				// it is in CDI registry so no need to add it again later.
				delete(devicesToAdd, specDevice.Name)
			} else if doCleanup {
				// skip CDI devices that were not detected
				klog.V(5).Infof("Removing device %v from CDI registry", specDevice.Name)
				specChanged = true
			} else {
				filteredDevices = append(filteredDevices, specDevice)
			}
		}
		// update spec if it was changed
		if specChanged {
			klog.V(5).Info("Replacing devices in spec with VFs filtered out")
			vendorSpec.Spec.Devices = filteredDevices
			specName := filepath.Base(vendorSpec.GetPath())
			klog.V(5).Infof("Overwriting spec %v", specName)
			err := registry.SpecDB().WriteSpec(vendorSpec.Spec, specName)
			if err != nil {
				klog.Errorf("failed writing CDI spec %v: %v", vendorSpec.GetPath(), err)
				return fmt.Errorf("failed writing CDI spec %v: %v", vendorSpec.GetPath(), err)
			}
		}
	}

	if len(devicesToAdd) > 0 {
		// add devices that were not found in registry to the first existing vendor spec
		apispec := vendorSpecs[0]
		klog.V(5).Infof("Adding %d devices to CDI spec", len(devicesToAdd))
		addDevicesToCDISpec(devicesToAdd, apispec.Spec)
		specName := filepath.Base(apispec.GetPath())

		cdiVersion, err := cdiapi.MinimumRequiredVersion(apispec.Spec)
		if err != nil {
			klog.Errorf("failed to get minimum CDI version for spec %v: %v", apispec.GetPath(), err)
			return fmt.Errorf("failed to get minimum CDI version for spec %v: %v", apispec.GetPath(), err)
		}
		if apispec.Version != cdiVersion {
			apispec.Version = cdiVersion
		}

		klog.V(5).Infof("Overwriting spec %v", specName)
		err = registry.SpecDB().WriteSpec(apispec.Spec, specName)
		if err != nil {
			klog.Errorf("failed to write CDI spec %v: %v", apispec.GetPath(), err)
			return fmt.Errorf("failed write CDI spec %v: %v", apispec.GetPath(), err)
		}
	}

	return nil
}

func syncDeviceNodes(
	specDevice specs.Device, detectedDevice *DeviceInfo,
	cardregexp *regexp.Regexp, renderdregexp *regexp.Regexp) bool {
	specChanged := false
	dridevpath := getDevfsDriDir()

	for deviceNodeIdx, deviceNode := range specDevice.ContainerEdits.DeviceNodes {
		driFileName := filepath.Base(deviceNode.Path) // e.g. card1 or renderD129
		switch {
		case cardregexp.MatchString(driFileName):
			klog.V(5).Infof("CDI device node %v is a card device: %v", deviceNodeIdx, driFileName)
			cardIdx, err := strconv.ParseUint(strings.Split(driFileName, "card")[1], 10, 64)
			if err != nil {
				klog.Errorf("Failed to parse index of DRI card device '%v', skipping", driFileName)
				continue // deviceNode loop
			}
			if cardIdx != detectedDevice.CardIdx {
				klog.V(5).Infof("Fixing card index for CDI device %v", detectedDevice.UID)
				deviceNode.Path = filepath.Join(dridevpath, fmt.Sprintf("card%d", detectedDevice.CardIdx))
				specChanged = true
			} else {
				klog.V(5).Info("card index for CDI device is correct")
			}
		case renderdregexp.MatchString(driFileName):
			klog.V(5).Infof("CDI device node %v is a renderD device: %v", deviceNodeIdx, driFileName)
			renderdIdx, err := strconv.ParseUint(strings.Split(driFileName, "renderD")[1], 10, 64)
			if err != nil {
				klog.Errorf("Failed to parse index of DRI renderD device '%v', skipping", driFileName)
				continue // deviceNode loop
			}
			if renderdIdx != detectedDevice.RenderdIdx {
				klog.V(5).Infof("Fixing renderD index for CDI device %v", detectedDevice.UID)
				deviceNode.Path = filepath.Join(dridevpath, fmt.Sprintf("renderD%d", detectedDevice.RenderdIdx))
				specChanged = true
			} else {
				klog.V(5).Info("renderD index for CDI device is correct")
			}
		default:
			klog.Warningf("Unexpected device node %v in CDI device %v", deviceNode.Path)
		}
	}
	return specChanged
}

func addDevicesToCDISpec(devices DevicesInfo, spec *specs.Spec) {
	dridevpath := getDevfsDriDir()

	for _, device := range devices {
		// primary / control node (for modesetting)
		newDevice := specs.Device{
			Name: device.UID,
			ContainerEdits: specs.ContainerEdits{
				DeviceNodes: []*specs.DeviceNode{
					{Path: filepath.Join(dridevpath, fmt.Sprintf("card%d", device.CardIdx)), Type: "c"},
				},
			},
		}
		// render nodes can be optional: https://www.kernel.org/doc/html/latest/gpu/drm-uapi.html#render-nodes
		if device.RenderdIdx != 0 {
			newDevice.ContainerEdits.DeviceNodes = append(
				newDevice.ContainerEdits.DeviceNodes,
				&specs.DeviceNode{
					Path: filepath.Join(dridevpath, fmt.Sprintf("renderD%d", device.RenderdIdx)),
					Type: "c",
				},
			)
		}
		// TODO: add /dev/dri/by-path entries
		spec.Devices = append(spec.Devices, newDevice)
	}
}

// Write devices into new vendor-specific CDI spec, should only be called if such spec does not exist.
func addNewDevicesToNewRegistry(devices DevicesInfo) error {
	klog.V(5).Infof("Adding %v devices to new spec", len(devices))
	registry := cdiapi.GetRegistry()

	spec := &specs.Spec{
		Kind: cdiKind,
	}

	addDevicesToCDISpec(devices, spec)
	klog.V(5).Infof("spec devices length: %v", len(spec.Devices))

	cdiVersion, err := cdiapi.MinimumRequiredVersion(spec)
	if err != nil {
		return fmt.Errorf("failed to get minimum required CDI spec version: %v", err)
	}
	klog.V(5).Infof("CDI version required for new spec: %v", cdiVersion)
	spec.Version = cdiVersion

	specname, err := cdiapi.GenerateNameForSpec(spec)
	if err != nil {
		return fmt.Errorf("failed to generate name for cdi device spec: %+v", err)
	}
	klog.V(5).Infof("new name for new CDI spec: %v", specname)

	err = registry.SpecDB().WriteSpec(spec, specname)
	if err != nil {
		return fmt.Errorf("failed to write CDI spec %v: %v", specname, err)
	}

	return nil
}

// Check if any prepared claim already uses VFs from given parent UIDs.
func (s *nodeState) parentCanHaveVFs(toProvision map[string][]*DeviceInfo) bool {
	for _, preparedClaim := range s.prepared {
		for _, device := range preparedClaim {
			if _, found := toProvision[device.ParentUID]; found {
				return false
			}
		}
	}
	return true
}

// FreeClaimDevices returns slice of gpu IDs where all VFs can be removed.
func (s *nodeState) FreeClaimDevices(preparedClaimFilePath string, claimUID string) ([]string, error) {
	s.Lock()
	defer s.Unlock()

	parentsToCleanup := []string{}

	if s.prepared[claimUID] == nil {
		return parentsToCleanup, nil
	}

	parentUIDs := []string{}
	for _, device := range s.prepared[claimUID] {
		var err error
		switch device.DeviceType {
		case intelcrd.GpuDeviceType:
			klog.V(5).Info("Freeing GPU, nothing to do")
		case intelcrd.VfDeviceType:
			parentUIDs = append(parentUIDs, device.ParentUID)
		default:
			klog.Errorf("unsupported device type: %v", device.DeviceType)
			err = fmt.Errorf("unsupported device type: %v", device.DeviceType)
		}
		if err != nil {
			return nil, fmt.Errorf("freeClaimDevices failed: %v", err)
		}
	}

	parentsToCleanup, err := s.freeVFs(claimUID, parentUIDs)
	if err != nil {
		return nil, fmt.Errorf("freeClaimDevices failed: %v", err)
	}

	for _, parentUID := range parentsToCleanup {
		if err := s.removeVFs(parentUID); err != nil {
			klog.Errorf("failed to free VFs for %v: %+v", parentUID, err)
			// Only way it could fail is CDI registry being unavailable.
			// Rest of parentsToCleanup won't succeed, no point proceeding.
			return nil, fmt.Errorf("failed to free VFs for %v: %+v", parentUID, err)
		}
	}

	delete(s.prepared, claimUID)
	// write prepared claims to file
	err = s.writePreparedClaimToFile(preparedClaimFilePath, s.prepared)
	if err != nil {
		klog.Errorf("Error writing prepared claims to file: %v", err)
		return nil, fmt.Errorf("failed to write prepared claims to file: %v", err)
	}
	return parentsToCleanup, nil
}

func (s *nodeState) GetUpdatedSpec(inspec *intelcrd.GpuAllocationStateSpec) *intelcrd.GpuAllocationStateSpec {
	s.Lock()
	defer s.Unlock()

	outspec := inspec.DeepCopy()
	s.syncAllocatableDevicesToGASSpec(outspec)
	return outspec
}

func (s *nodeState) DeviceInfoFromAllocated(allocatedGpu intelcrd.AllocatedGpu) *DeviceInfo {
	device := DeviceInfo{
		UID:        allocatedGpu.UID,
		DeviceType: string(allocatedGpu.Type),
		ParentUID:  allocatedGpu.ParentUID,
		MemoryMiB:  uint64(allocatedGpu.Memory),
		Millicores: uint64(allocatedGpu.Millicores),
		VFProfile:  allocatedGpu.Profile,
		VFIndex:    uint64(allocatedGpu.VFIndex),
	}
	// model will be needed to construct uid for new VF device in validateVFs()
	if allocatedGpu.ParentUID != "" {
		device.Model = s.allocatable[device.ParentUID].Model
	}
	return &device
}

func (s *nodeState) GetAllocatedCDINames(claimUID string) []string {
	devs := []string{}
	klog.V(5).Info("getAllocatedCDINames is called")

	klog.V(5).Info("Refreshing CDI registry")
	err := s.cdi.Refresh()
	if err != nil {
		klog.Errorf("Unable to refresh the CDI registry: %v", err)
		return []string{}
	}

	for _, device := range s.prepared[claimUID] {
		cdidev := s.cdi.DeviceDB().GetDevice(device.CDIName())
		if cdidev == nil {
			klog.Errorf("CDI Device %v from claim %v not found in cdi DB", device.CDIName(), claimUID)
			return []string{}
		}
		klog.V(5).Infof("Found cdi device %v", cdidev.GetQualifiedName())
		devs = append(devs, cdidev.GetQualifiedName())
	}
	return devs
}

func (s *nodeState) getMonitorCDINames(claimUID string) []string {
	klog.V(5).Info("getMonitorCDINames is called")

	klog.V(5).Info("Refreshing CDI registry")
	err := s.cdi.Refresh()
	if err != nil {
		klog.Errorf("Unable to refresh the CDI registry: %v", err)
		return []string{}
	}

	devs := []string{}
	for _, device := range s.allocatable {
		cdidev := s.cdi.DeviceDB().GetDevice(device.CDIName())
		if cdidev == nil {
			klog.Errorf("CDI Device %v for monitor claim %v not found in cdi DB", device.CDIName(), claimUID)
			return []string{}
		}
		klog.V(5).Infof("Found cdi device %v", cdidev.GetQualifiedName())
		devs = append(devs, cdidev.GetQualifiedName())
	}
	return devs
}

// Check every device from parenUIDs if all VFs on it can be removed.
func (s *nodeState) freeVFs(claimUIDBeingDeleted string, parentUIDs []string) ([]string, error) {
	klog.V(5).Info("freeVFs is called")

	parentsToCleanup := []string{}
	for _, parentUID := range parentUIDs {
		parentDevice, found := s.allocatable[parentUID]
		if !found {
			klog.Errorf("device %v has disappeared from allocatable", parentUID)
			continue
		}
		klog.V(5).Infof("Checking if VFs on parent device %v can be dismantled", parentUID)

		// TODO: only dismantle VFs if driver parameter or resource class permits changing HW layout / configuration
		// parent is a physical function with index 0

		vfsCanBeRemoved := true
		// Loop through prepared and search if VFs with same parent are used by any other allocation:
		// - do nothing if found - VFs are still needed
		// - if no VFs of parent found to be used - proceed to dismantling VFs
		for claimUID, usedGpus := range s.prepared {
			// ignore devices in the claim being deleted, they all are being unprepared
			if claimUID == claimUIDBeingDeleted {
				continue
			}
			for _, usedGpu := range usedGpus {
				if usedGpu.DeviceType == intelcrd.VfDeviceType && usedGpu.ParentUID == parentDevice.UID {
					klog.V(5).Infof(
						"Parent device %v is still used by %v (claim %v)",
						usedGpu.ParentUID,
						usedGpu.UID,
						claimUID)
					vfsCanBeRemoved = false
				}
			}
		}

		if vfsCanBeRemoved {
			parentsToCleanup = append(parentsToCleanup, parentUID)
		}
	}
	return parentsToCleanup, nil
}

func (s *nodeState) syncAllocatableDevicesToGASSpec(spec *intelcrd.GpuAllocationStateSpec) {
	gpus := make(map[string]intelcrd.AllocatableGpu)
	for _, device := range s.allocatable {
		gpus[device.UID] = intelcrd.AllocatableGpu{
			Memory:     device.MemoryMiB,
			Millicores: device.Millicores,
			Model:      device.Model,
			Type:       v1alpha2.GpuType(device.DeviceType),
			UID:        device.UID,
			Maxvfs:     device.MaxVFs,
			VFIndex:    device.VFIndex,
			ParentUID:  device.ParentUID,
			Ecc:        device.EccOn,
		}
	}

	spec.AllocatableDevices = gpus
}

// On startup read what was previously prepared where we left off.
func (s *nodeState) syncPreparedGpusFromFile(preparedClaims map[string][]*DeviceInfo) error {
	klog.V(5).Infof("Syncing %d Prepared allocations from GpuAllocationState to internal state", len(preparedClaims))

	if s.prepared == nil {
		s.prepared = make(ClaimPreparations)
	}

	for claimuid, preparedDevices := range preparedClaims {
		klog.V(5).Infof("claim %v has %v gpus", claimuid, len(preparedDevices))
		skipClaimAllocation := false
		prepared := []*DeviceInfo{}
		for _, preparedDevice := range preparedDevices {
			klog.V(5).Infof("Device: %+v", preparedDevice)
			switch preparedDevice.DeviceType {
			case intelcrd.GpuDeviceType:
				klog.V(5).Info("Matched GPU type in sync")
				if _, exists := s.allocatable[preparedDevice.UID]; !exists {
					klog.Errorf("allocated device %v no longer available for claim %v", preparedDevice.UID, claimuid)

					return fmt.Errorf("could not find allocated device %v for claimAllocation %v",
						preparedDevice.UID, claimuid)
				}
				newdevice := s.allocatable[preparedDevice.UID].DeepCopy()
				newdevice.MemoryMiB = preparedDevice.MemoryMiB
				newdevice.Millicores = preparedDevice.Millicores
				prepared = append(prepared, newdevice)
			case intelcrd.VfDeviceType:
				if _, exists := s.allocatable[preparedDevice.UID]; !exists {
					klog.Errorf("allocated device %v does not exist in allocatable", preparedDevice.UID)
					if _, parentExists := s.allocatable[preparedDevice.ParentUID]; !parentExists {
						klog.Errorf("parent %v does not exist in allocatable", preparedDevice.ParentUID)
					}
					skipClaimAllocation = true

					break
				}
				newdevice := s.allocatable[preparedDevice.UID].DeepCopy()
				prepared = append(prepared, newdevice)
			default:
				klog.Errorf("unsupported device type: %v", preparedDevice.DeviceType)
			}
		}
		if !skipClaimAllocation {
			s.prepared[claimuid] = prepared
		}
	}

	return nil
}

// addNewVFs adds new VFs into CDI registries and into internal
// NodeState.allocatable list.
func (s *nodeState) addNewVFs(newVFs DevicesInfo) error {
	klog.V(5).Infof("Announcing new devices: %+v", newVFs)

	s.Lock()
	defer s.Unlock()

	klog.V(5).Info("Refreshing CDI registry")
	err := s.cdi.Refresh()
	if err != nil {
		return fmt.Errorf("unable to refresh the CDI registry: %v", err)
	}

	klog.V(5).Infof("Adding %v new VFs to CDI", len(newVFs))
	err = syncDetectedDevicesWithCdiRegistry(s.cdi, newVFs, false)
	if err != nil {
		klog.Errorf("failed announcing new VFs: %v", err)
		return fmt.Errorf("failed announcing new VFs: %v", err)
	}

	// New VF devices added to s.allocatable will be announced to GAS
	// when getUpdatedSpec will be called in NodePrepareResource.
	for duid, device := range newVFs {
		s.allocatable[duid] = device
	}

	return nil
}

// removeVFs removes all VFs that belong to parentUID from CDI registries
// and from internal nodeState.allocatable list.
func (s *nodeState) removeVFs(parentUID string) error {
	klog.V(5).Infof("unannounceVFs called for parentUID: %v", parentUID)

	cdiDevicesToDelete := map[string]bool{}
	// GAS spec will be updated with s.allocatable in NodeUnprepareResource call to getUpdatedSpec
	for devUID, dev := range s.allocatable {
		if dev.ParentUID == parentUID {
			cdiDevicesToDelete[devUID] = true
			delete(s.allocatable, devUID)
		}
	}

	// remove from CDI registry
	klog.V(5).Info("Refreshing CDI registry")
	err := s.cdi.Refresh()
	if err != nil {
		return fmt.Errorf("unable to refresh the CDI registry: %v", err)
	}

	for _, spec := range s.cdi.SpecDB().GetVendorSpecs(cdiVendor) {
		klog.V(5).Infof("Checking for VFs in CDI spec: %+v", spec)

		remainingDevices := []specs.Device{} // list of devices to be saved back to CDI Spec
		for _, device := range spec.Spec.Devices {
			if _, found := cdiDevicesToDelete[device.Name]; found {
				klog.V(5).Infof("Found matching VF: %v", device.Name)
				continue // filter this device out
			}
			remainingDevices = append(remainingDevices, device)
		}
		if len(remainingDevices) < len(spec.Spec.Devices) {
			klog.V(5).Info("Replacing devices in spec with VFs filtered out")
			spec.Spec.Devices = remainingDevices

			klog.V(5).Info("Overwriting spec")
			specName := filepath.Base(spec.GetPath())
			err = s.cdi.SpecDB().WriteSpec(spec.Spec, specName)
			if err != nil {
				klog.Errorf("failed writing CDI spec %v: %v", spec.GetPath(), err)
			}
		}
	}

	return nil
}

func (s *nodeState) makePreparedClaimAllocation(preparedClaimFilePath string, perClaimDevices map[string][]*DeviceInfo) error {

	for claimUID, devices := range perClaimDevices {
		for _, device := range devices {
			_, provisioned := s.allocatable[device.UID]
			if !provisioned {
				klog.Errorf("could not find allocated device %v for claim %v while making prepared claim allocation",
					device.UID, claimUID)
				return fmt.Errorf("could not find allocated device %v", claimUID)
			}
		}

		s.prepared[claimUID] = devices
		klog.V(5).Infof("Created prepared claim allocation %v: %+v", claimUID, devices)
	}

	// write prepared claims to file
	err := s.writePreparedClaimToFile(preparedClaimFilePath, s.prepared)
	if err != nil {
		klog.Errorf("Error writing prepared claims to file: %v", err)
		return fmt.Errorf("failed to write prepared claims to file: %v", err)
	}

	return nil
}

// getOrCreatePreparedClaims reads a PreparedClaim from a file and deserializes it or creates the file.
func (s *nodeState) getOrCreatePreparedClaims(preparedClaimFilePath string) (ClaimPreparations, error) {
	preparedClaims := make(ClaimPreparations)

	if _, err := os.Stat(preparedClaimFilePath); os.IsNotExist(err) {
		klog.V(5).Infof("could not find file %v. Creating file", preparedClaimFilePath)
		f, err := os.OpenFile(preparedClaimFilePath, os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			return nil, fmt.Errorf("failed creating file %v. Err: %v", preparedClaimFilePath, err)
		}
		defer f.Close()

		if _, err := f.WriteString("{}"); err != nil {
			return nil, fmt.Errorf("failed writing to file %v. Err: %v", preparedClaimFilePath, err)
		}

		klog.V(5).Infof("empty prepared claims file created %v", preparedClaimFilePath)

		return preparedClaims, nil
	}

	preparedClaimsConfigBytes, err := os.ReadFile(preparedClaimFilePath)
	if err != nil {
		klog.V(5).Infof("could not read prepared claims configuration from file %v. Err: %v", preparedClaimFilePath, err)
		return nil, fmt.Errorf("failed reading file %v. Err: %v", preparedClaimFilePath, err)
	}

	if err := json.Unmarshal(preparedClaimsConfigBytes, &preparedClaims); err != nil {
		klog.V(5).Infof("Could not parse default prepared claims configuration from file %v. Err: %v", preparedClaimFilePath, err)
		return nil, fmt.Errorf("failed parsing file %v. Err: %v", preparedClaimFilePath, err)
	}

	return preparedClaims, nil
}

// writePreparedClaimToFile serializes PreparedClaims and writes it to a file.
func (s *nodeState) writePreparedClaimToFile(preparedClaimFilePath string, preparedClaims ClaimPreparations) error {
	encodedPreparedClaims, err := json.MarshalIndent(preparedClaims, "", "  ")
	if err != nil {
		return fmt.Errorf("failed encoding json. Err: %v", err)
	}
	return os.WriteFile(preparedClaimFilePath, encodedPreparedClaims, 0600)
}
