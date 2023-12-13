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
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/sriov"
)

// DeviceInfo is an internal structure type to store info about discovered device.
type DeviceInfo struct {
	uid        string // unique identifier, pci_DBDF-pci_device_id
	model      string // pci_device_id
	cardidx    uint64 // card device number (e.g. 0 for /dev/dri/card0)
	renderdidx uint64 // renderD device number (e.g. 128 for /dev/dri/renderD128)
	memoryMiB  uint64 // in MiB
	millicores uint64 // [0-1000] where 1000 means whole GPU.
	deviceType string // gpu, vf, any
	maxvfs     uint64 // if enabled, non-zero maximum amount of VFs
	parentuid  string // uid of gpu device where VF is
	vfprofile  string // name of the SR-IOV profile
	vfindex    uint64 // 0-based PCI index of the VF on the GPU, DRM indexing starts with 1
	eccOn      bool   // true of ECC is enabled, false otherwise
}

func (g *DeviceInfo) DeepCopy() *DeviceInfo {
	di := *g
	return &di
}

func (g *DeviceInfo) pciVFIndex() uint64 {
	return g.vfindex
}

func (g *DeviceInfo) drmVFIndex() uint64 {
	return g.vfindex + 1
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

type nodeState struct {
	sync.Mutex
	cdi         cdiapi.Registry
	allocatable DevicesInfo
	prepared    ClaimAllocations
}

func (g DeviceInfo) CDIName() string {
	return fmt.Sprintf("%s=%s", cdiKind, g.uid)
}

func newNodeState(gas *intelcrd.GpuAllocationState, detectedDevices map[string]*DeviceInfo, cdiRoot string) (*nodeState, error) {
	klog.V(3).Infof("Enumerating all devices")

	for ddev := range detectedDevices {
		klog.V(3).Infof("new device: %+v", ddev)
	}

	klog.V(5).Infof("Getting CDI registry")
	cdi := cdiapi.GetRegistry(
		cdiapi.WithSpecDirs(cdiRoot),
	)

	klog.V(5).Infof("Got CDI registry, refreshing it")
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

	for duid, ddev := range detectedDevices {
		klog.V(3).Infof("Allocatable after CDI refresh device: %v : %+v", duid, ddev)
	}

	klog.V(5).Infof("Creating NodeState")
	// TODO: allocatable should include cdi-described
	state := &nodeState{
		cdi:         cdi,
		allocatable: detectedDevices,
		prepared:    make(ClaimAllocations),
	}

	klog.V(5).Infof("Syncing allocatable devices")
	err = state.syncPreparedGpusFromGASSpec(&gas.Spec)
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

	cardregexp := regexp.MustCompile(cardRE)
	renderdregexp := regexp.MustCompile(renderdIdRE)

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

				if syncDeviceNodes(specDevice, detectedDevice, cardregexp, renderdregexp) {
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
			if cardIdx != detectedDevice.cardidx {
				klog.V(5).Infof("Fixing card index for CDI device %v", detectedDevice.uid)
				deviceNode.Path = filepath.Join(dridevpath, fmt.Sprintf("card%d", detectedDevice.cardidx))
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
			if renderdIdx != detectedDevice.renderdidx {
				klog.V(5).Infof("Fixing renderD index for CDI device %v", detectedDevice.uid)
				deviceNode.Path = filepath.Join(dridevpath, fmt.Sprintf("renderD%d", detectedDevice.renderdidx))
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
		newDevice := specs.Device{
			Name: device.uid,
			ContainerEdits: specs.ContainerEdits{
				DeviceNodes: []*specs.DeviceNode{
					{Path: filepath.Join(dridevpath, fmt.Sprintf("card%d", device.cardidx)), Type: "c"},
				},
			},
		}
		// renderD DRM devices are absent on non-Display controllers
		if device.renderdidx != 0 {
			newDevice.ContainerEdits.DeviceNodes = append(
				newDevice.ContainerEdits.DeviceNodes,
				&specs.DeviceNode{
					Path: filepath.Join(dridevpath, fmt.Sprintf("renderD%d", device.renderdidx)),
					Type: "c",
				},
			)
		}
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

// FreeClaimDevices returns slice of gpu IDs where all VFs can be removed.
func (s *nodeState) FreeClaimDevices(claimUID string, gasSpec *intelcrd.GpuAllocationStateSpec) ([]string, error) {
	s.Lock()
	defer s.Unlock()

	parentsToCleanup := []string{}

	if s.prepared[claimUID] == nil {
		return parentsToCleanup, nil
	}

	parentUIDs := []string{}
	for _, device := range s.prepared[claimUID] {
		var err error
		switch device.deviceType {
		case intelcrd.GpuDeviceType:
			klog.V(5).Info("Freeing GPU, nothing to do")
		case intelcrd.VfDeviceType:
			parentUIDs = append(parentUIDs, device.parentuid)
		default:
			klog.Errorf("unsupported device type: %v", device.deviceType)
			err = fmt.Errorf("unsupported device type: %v", device.deviceType)
		}
		if err != nil {
			return nil, fmt.Errorf("freeClaimDevices failed: %v", err)
		}
	}

	parentsToCleanup, err := s.freeVFs(claimUID, parentUIDs, gasSpec)
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
	return parentsToCleanup, nil
}

func (s *nodeState) GetUpdatedSpec(inspec *intelcrd.GpuAllocationStateSpec) *intelcrd.GpuAllocationStateSpec {
	s.Lock()
	defer s.Unlock()

	outspec := inspec.DeepCopy()
	s.syncAllocatableDevicesToGASSpec(outspec)
	s.syncPreparedToGASSpec(outspec)
	return outspec
}

func (s *nodeState) DeviceInfoFromAllocated(allocatedGpu intelcrd.AllocatedGpu) *DeviceInfo {
	device := DeviceInfo{
		uid:        allocatedGpu.UID,
		deviceType: string(allocatedGpu.Type),
		parentuid:  allocatedGpu.ParentUID,
		memoryMiB:  uint64(allocatedGpu.Memory),
		millicores: uint64(allocatedGpu.Millicores),
		vfprofile:  allocatedGpu.Profile,
	}
	if vfIndex, err := sriov.VFIndexFromUID(device.uid); err == nil {
		device.vfindex = vfIndex
	}
	return &device
}

func (s *nodeState) GetAllocatedCDINames(claimUID string) []string {
	devs := []string{}
	klog.V(5).Infof("getAllocatedCDINames is called")

	klog.V(5).Infof("Refreshing CDI registry")
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
	klog.V(5).Infof("getMonitorCDINames is called")

	klog.V(5).Infof("Refreshing CDI registry")
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
func (s *nodeState) freeVFs(
	claimUIDBeingDeleted string,
	parentUIDs []string,
	gasSpec *intelcrd.GpuAllocationStateSpec) ([]string, error) {
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
		for claimUID, usedGpus := range gasSpec.PreparedClaims {
			// ignore devices in the claim being deleted, they all are being unprepared
			if claimUID == claimUIDBeingDeleted {
				continue
			}
			for _, usedGpu := range usedGpus {
				if usedGpu.Type == intelcrd.VfDeviceType && usedGpu.ParentUID == parentDevice.uid {
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
		gpus[device.uid] = intelcrd.AllocatableGpu{
			Memory:     device.memoryMiB,
			Millicores: device.millicores,
			Model:      device.model,
			Type:       v1alpha2.GpuType(device.deviceType),
			UID:        device.uid,
			Maxvfs:     device.maxvfs,
			ParentUID:  device.parentuid,
			Ecc:        device.eccOn,
		}
	}

	spec.AllocatableDevices = gpus
}

// On startup read what was previously prepared where we left off.
func (s *nodeState) syncPreparedGpusFromGASSpec(spec *intelcrd.GpuAllocationStateSpec) error {
	klog.V(5).Infof("Syncing %d Prepared allocations from GpuAllocationState to internal state", len(spec.PreparedClaims))

	if s.prepared == nil {
		s.prepared = make(ClaimAllocations)
	}

	for claimuid, preparedDevices := range spec.PreparedClaims {
		klog.V(5).Infof("claim %v has %v gpus", claimuid, len(preparedDevices))
		skipClaimAllocation := false
		prepared := []*DeviceInfo{}
		for _, preparedDevice := range preparedDevices {
			klog.V(5).Infof("Device: %+v", preparedDevice)
			switch preparedDevice.Type {
			case intelcrd.GpuDeviceType:
				klog.V(5).Info("Matched GPU type in sync")
				if _, exists := s.allocatable[preparedDevice.UID]; !exists {
					klog.Errorf("allocated device %v no longer available for claim %v", preparedDevice.UID, claimuid)

					return fmt.Errorf("could not find allocated device %v for claimAllocation %v",
						preparedDevice.UID, claimuid)
				}
				newdevice := s.allocatable[preparedDevice.UID].DeepCopy()
				newdevice.memoryMiB = preparedDevice.Memory
				newdevice.millicores = preparedDevice.Millicores
				prepared = append(prepared, newdevice)
			case intelcrd.VfDeviceType:
				if _, exists := s.allocatable[preparedDevice.UID]; !exists {
					klog.Errorf("allocated device %v does not exist in allocatable", preparedDevice.UID)
					if _, parentExists := spec.AllocatableDevices[preparedDevice.ParentUID]; !parentExists {
						klog.Errorf("parent %v does not exist in allocatable", preparedDevice.ParentUID)
					}
					skipClaimAllocation = true

					break
				}
				newdevice := s.allocatable[preparedDevice.UID].DeepCopy()
				prepared = append(prepared, newdevice)
			default:
				klog.Errorf("unsupported device type: %v", preparedDevice.Type)
			}
		}
		if !skipClaimAllocation {
			s.prepared[claimuid] = prepared
		}
	}

	return nil
}

func (s *nodeState) syncPreparedToGASSpec(gasspec *intelcrd.GpuAllocationStateSpec) {
	out := make(intelcrd.PreparedClaims)
	for claimuid, devices := range s.prepared {
		claimGpus := intelcrd.PreparedClaim{}
		for _, device := range devices {
			switch device.deviceType {
			case intelcrd.GpuDeviceType, intelcrd.VfDeviceType:
				outdevice := intelcrd.AllocatedGpu{
					UID:        device.uid,
					Memory:     device.memoryMiB,
					Millicores: device.millicores,
					Type:       v1alpha2.GpuType(device.deviceType),
					Profile:    device.vfprofile,
					ParentUID:  device.parentuid,
				}
				claimGpus = append(claimGpus, outdevice)
			default:
				klog.Errorf("unsupported device type: %v", device.deviceType)
			}
		}
		out[claimuid] = claimGpus
	}
	gasspec.PreparedClaims = out
}

// addNewVFs adds new VFs into CDI registries and into internal
// NodeState.allocatable list.
func (s *nodeState) addNewVFs(newVFs DevicesInfo) error {
	klog.V(5).Infof("Announcing new devices: %+v", newVFs)

	s.Lock()
	defer s.Unlock()

	klog.V(5).Infof("Refreshing CDI registry")
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
	// GAS spec will be updated with s.allocatable in NodeUnprepareResource call to getUpdatedSpec
	for _, availDev := range s.allocatable {
		if availDev.parentuid == parentUID {
			delete(s.allocatable, availDev.uid)
		}
	}

	// remove from CDI registry
	klog.V(5).Infof("Refreshing CDI registry")
	err := s.cdi.Refresh()
	if err != nil {
		return fmt.Errorf("unable to refresh the CDI registry: %v", err)
	}

	for _, spec := range s.cdi.SpecDB().GetVendorSpecs(cdiVendor) {
		klog.V(5).Infof("Checking for VFs in CDI spec: %+v", spec)

		filteredDevices := []specs.Device{}
		for _, device := range spec.Spec.Devices {
			if detectedParentUID, err := sriov.PfUIDFromVfUID(device.Name); err == nil &&
				parentUID == detectedParentUID {
				klog.V(5).Infof("Found matching VF: %v", device.Name)
				continue
			}
			filteredDevices = append(filteredDevices, device)
		}
		if len(filteredDevices) < len(spec.Spec.Devices) {
			klog.V(5).Info("Replacing devices in spec with VFs filtered out")
			spec.Spec.Devices = filteredDevices

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

func (s *nodeState) makePreparedClaimAllocation(claimUID string, allocatedClaim intelcrd.AllocatedClaim) error {
	preparedGpus := []*DeviceInfo{}

	for _, device := range allocatedClaim.Gpus {
		sourceDevice, provisioned := s.allocatable[device.UID]
		if !provisioned {
			klog.Errorf("could not find allocated device %v for claim %v while making prepared claim allocation",
				device.UID, claimUID)
			return fmt.Errorf("could not find allocated device %v", claimUID)
		}
		preparedGpus = append(preparedGpus, sourceDevice.DeepCopy())
	}

	s.prepared[claimUID] = preparedGpus
	klog.V(5).Infof("Created prepared claim allocation %v: %+v", claimUID, preparedGpus)
	return nil
}
