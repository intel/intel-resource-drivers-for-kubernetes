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
	"strings"
	"sync"
	"time"

	resourcev1 "k8s.io/api/resource/v1alpha2"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"

	cdihelpers "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/cdihelpers"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gpu/v1alpha2"
	intelcrd "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gpu/v1alpha2/api"
	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"
	specs "tags.cncf.io/container-device-interface/specs-go"
)

const (
	bytesInMiB = 1024 * 1024
)

type ClaimPreparations map[string][]*device.DeviceInfo

type nodeState struct {
	sync.Mutex
	cdiCache               *cdiapi.Cache
	allocatable            device.DevicesInfo
	prepared               ClaimPreparations
	preparedClaimsFilePath string
}

func newNodeState(detectedDevices map[string]*device.DeviceInfo, cdiRoot string, preparedClaimFilePath string) (*nodeState, error) {
	for ddev := range detectedDevices {
		klog.V(3).Infof("new device: %+v", ddev)
	}

	klog.V(5).Info("Refreshing CDI registry")
	if err := cdiapi.Configure(cdiapi.WithSpecDirs(cdiRoot)); err != nil {
		return nil, fmt.Errorf("unable to refresh the CDI registry: %v", err)
	}

	cdiCache := cdiapi.GetDefaultCache()

	// syncDetectedDevicesWithRegistry overrides uid in detecteddevices from existing cdi spec
	if err := cdihelpers.SyncDetectedDevicesWithRegistry(cdiCache, detectedDevices, true); err != nil {
		return nil, fmt.Errorf("unable to sync detected devices to CDI registry: %v", err)
	}

	// hack for tests on slow machines
	time.Sleep(250 * time.Millisecond)

	klog.V(5).Info("Allocatable devices after CDI registry refresh:")
	for duid, ddev := range detectedDevices {
		klog.V(5).Infof("CDI device: %v : %+v", duid, ddev)
	}

	klog.V(5).Info("Creating NodeState")
	// TODO: allocatable should include cdi-described
	state := &nodeState{
		cdiCache:               cdiCache,
		allocatable:            detectedDevices,
		prepared:               make(ClaimPreparations),
		preparedClaimsFilePath: preparedClaimFilePath,
	}

	preparedClaims, err := getOrCreatePreparedClaims(preparedClaimFilePath)
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

// Check if any prepared claim already uses VFs from given parent UIDs.
func (s *nodeState) parentCanHaveVFs(toProvision map[string][]*device.DeviceInfo) bool {
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
func (s *nodeState) FreeClaimDevices(claimUID string) ([]string, error) {
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
	err = writePreparedClaimsToFile(s.preparedClaimsFilePath, s.prepared)
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

func (s *nodeState) DeviceInfoFromAllocated(allocatedGpu intelcrd.AllocatedGpu) *device.DeviceInfo {
	device := device.DeviceInfo{
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

	for _, device := range s.prepared[claimUID] {
		cdidev := s.cdiCache.GetDevice(device.CDIName())
		if cdidev == nil {
			klog.Errorf("CDI Device %v from claim %v not found in CDI DB", device.CDIName(), claimUID)
			return []string{}
		}
		klog.V(5).Infof("Found CDI device %v", cdidev.GetQualifiedName())
		devs = append(devs, cdidev.GetQualifiedName())
	}
	return devs
}

func (s *nodeState) getMonitorCDINames(claimUID string) []string {
	klog.V(5).Info("getMonitorCDINames is called")

	klog.V(5).Info("Refreshing CDI registry")
	err := s.cdiCache.Refresh()
	if err != nil {
		klog.Errorf("Unable to refresh the CDI registry: %v", err)
		return []string{}
	}

	devs := []string{}
	for _, device := range s.allocatable {
		cdidev := s.cdiCache.GetDevice(device.CDIName())
		if cdidev == nil {
			klog.Errorf("CDI Device %v for monitor claim %v not found in CDI DB", device.CDIName(), claimUID)
			return []string{}
		}
		klog.V(5).Infof("Found CDI device %v", cdidev.GetQualifiedName())
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
func (s *nodeState) syncPreparedGpusFromFile(preparedClaims map[string][]*device.DeviceInfo) error {
	klog.V(5).Infof("Syncing %d Prepared allocations from GpuAllocationState to internal state", len(preparedClaims))

	if s.prepared == nil {
		s.prepared = make(ClaimPreparations)
	}

	for claimuid, preparedDevices := range preparedClaims {
		klog.V(5).Infof("claim %v has %v gpus", claimuid, len(preparedDevices))
		skipClaimAllocation := false
		prepared := []*device.DeviceInfo{}
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
func (s *nodeState) addNewVFs(newVFs device.DevicesInfo) error {
	klog.V(5).Infof("Announcing new devices: %+v", newVFs)

	s.Lock()
	defer s.Unlock()

	klog.V(5).Info("Refreshing CDI registry")
	err := s.cdiCache.Refresh()
	if err != nil {
		return fmt.Errorf("unable to refresh the CDI registry: %v", err)
	}

	klog.V(5).Infof("Adding %v new VFs to CDI", len(newVFs))
	err = cdihelpers.SyncDetectedDevicesWithRegistry(s.cdiCache, newVFs, false)
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
	err := s.cdiCache.Refresh()
	if err != nil {
		return fmt.Errorf("unable to refresh the CDI registry: %v", err)
	}

	for _, spec := range s.cdiCache.GetVendorSpecs(cdiVendor) {
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
			specName := path.Base(spec.GetPath())
			err = s.cdiCache.WriteSpec(spec.Spec, specName)
			if err != nil {
				klog.Errorf("failed writing CDI spec %v: %v", spec.GetPath(), err)
			}
		}
	}

	return nil
}

func (s *nodeState) makePreparedClaimAllocation(perClaimDevices map[string][]*device.DeviceInfo) error {

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
	err := writePreparedClaimsToFile(s.preparedClaimsFilePath, s.prepared)
	if err != nil {
		klog.Errorf("Error writing prepared claims to file: %v", err)
		return fmt.Errorf("failed to write prepared claims to file: %v", err)
	}

	return nil
}

func (s *nodeState) getResourceModel() resourcev1.ResourceModel {
	var devices []resourcev1.NamedResourcesInstance

	for _, device := range s.allocatable {
		instance := resourcev1.NamedResourcesInstance{
			Name: strings.ToLower(device.UID),
			Attributes: []resourcev1.NamedResourcesAttribute{
				{
					Name: "uid",
					NamedResourcesAttributeValue: resourcev1.NamedResourcesAttributeValue{
						StringValue: &device.UID,
					},
				},
				{
					Name: "sr-iov",
					NamedResourcesAttributeValue: resourcev1.NamedResourcesAttributeValue{
						BoolValue: ptr.To(device.SriovEnabled()),
					},
				},
				{
					Name: "memory",
					NamedResourcesAttributeValue: resourcev1.NamedResourcesAttributeValue{
						QuantityValue: resource.NewQuantity(int64(device.MemoryMiB)*bytesInMiB, resource.BinarySI),
					},
				},
				{
					Name: "model",
					NamedResourcesAttributeValue: resourcev1.NamedResourcesAttributeValue{
						StringValue: ptr.To(device.ModelName()),
					},
				},
			},
		}
		devices = append(devices, instance)
	}

	model := resourcev1.ResourceModel{
		NamedResources: &resourcev1.NamedResourcesResources{Instances: devices},
	}

	return model
}

// getOrCreatePreparedClaims reads a PreparedClaim from a file and deserializes it or creates the file.
func getOrCreatePreparedClaims(preparedClaimFilePath string) (ClaimPreparations, error) {
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

		return make(ClaimPreparations), nil
	}

	return readPreparedClaimsFromFile(preparedClaimFilePath)
}

// readPreparedClaimToFile returns unmarshaled content for given prepared claims JSON file.
func readPreparedClaimsFromFile(preparedClaimFilePath string) (ClaimPreparations, error) {

	preparedClaims := make(ClaimPreparations)

	preparedClaimsBytes, err := os.ReadFile(preparedClaimFilePath)
	if err != nil {
		klog.V(5).Infof("could not read prepared claims configuration from file %v. Err: %v", preparedClaimFilePath, err)
		return nil, fmt.Errorf("failed reading file %v. Err: %v", preparedClaimFilePath, err)
	}

	if err := json.Unmarshal(preparedClaimsBytes, &preparedClaims); err != nil {
		klog.V(5).Infof("Could not parse default prepared claims configuration from file %v. Err: %v", preparedClaimFilePath, err)
		return nil, fmt.Errorf("failed parsing file %v. Err: %v", preparedClaimFilePath, err)
	}

	return preparedClaims, nil
}

// writePreparedClaimsToFile serializes PreparedClaims and writes it to a file.
func writePreparedClaimsToFile(preparedClaimFilePath string, preparedClaims ClaimPreparations) error {
	if preparedClaims == nil {
		preparedClaims = ClaimPreparations{}
	}
	encodedPreparedClaims, err := json.MarshalIndent(preparedClaims, "", "  ")
	if err != nil {
		return fmt.Errorf("prepared claims JSON encoding failed. Err: %v", err)
	}
	return os.WriteFile(preparedClaimFilePath, encodedPreparedClaims, 0600)
}
