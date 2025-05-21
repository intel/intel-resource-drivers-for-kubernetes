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

package main

import (
	"context"
	"fmt"
	"path"
	"time"

	resourcev1 "k8s.io/api/resource/v1beta1"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
	"k8s.io/dynamic-resource-allocation/resourceslice"
	"k8s.io/klog/v2"
	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"
	cdiparser "tags.cncf.io/container-device-interface/pkg/parser"
	cdiSpecs "tags.cncf.io/container-device-interface/specs-go"

	cdihelpers "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gaudi/cdihelpers"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gaudi/device"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/helpers"
)

type nodeState struct {
	*helpers.NodeState
}

func newNodeState(detectedDevices map[string]*device.DeviceInfo, cdiRoot string, preparedClaimsFilePath string, nodeName string) (*helpers.NodeState, error) {
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

	time.Sleep(250 * time.Millisecond)

	klog.V(5).Info("Allocatable devices after CDI registry refresh:")
	for duid, ddev := range detectedDevices {
		klog.V(5).Infof("CDI device: %v : %+v", duid, ddev)
	}

	// TODO: should be only create prepared claims, discard old preparations. Do we even need the snapshot?
	preparedClaims, err := helpers.GetOrCreatePreparedClaims(preparedClaimsFilePath)
	if err != nil {
		klog.Errorf("Error getting prepared claims: %v", err)
		return nil, fmt.Errorf("failed to get prepared claims: %v", err)
	}

	klog.V(5).Info("Creating NodeState")
	// TODO: allocatable should include cdi-described
	state := nodeState{
		NodeState: &helpers.NodeState{
			CdiCache:               cdiCache,
			Allocatable:            detectedDevices,
			Prepared:               preparedClaims,
			PreparedClaimsFilePath: preparedClaimsFilePath,
			NodeName:               nodeName,
		},
	}
	/*
		klog.V(5).Info("Syncing allocatable devices")
		err = state.syncPreparedDevicesFromFile(clientset, preparedClaims)
		if err != nil {
			return nil, fmt.Errorf("unable to sync allocated devices from GaudiAllocationState: %v", err)
		}
	*/

	allocatableDevices, ok := state.Allocatable.(map[string]*device.DeviceInfo)
	if !ok {
		return nil, fmt.Errorf("unexpected type for state.Allocatable")
	}

	klog.V(5).Infof("Synced state with CDI and GaudiAllocationState: %+v", state)
	for duid, ddev := range allocatableDevices {
		klog.V(5).Infof("Allocatable device: %v : %+v", duid, ddev)
	}

	return state.NodeState, nil
}

func (s *nodeState) GetResources() resourceslice.DriverResources {
	s.Lock()
	defer s.Unlock()

	devices := []resourcev1.Device{}

	allocatableDevices, _ := s.Allocatable.(map[string]*device.DeviceInfo)
	for gaudiUID, gaudi := range allocatableDevices {
		newDevice := resourcev1.Device{
			Name: gaudiUID,
			Basic: &resourcev1.BasicDevice{
				Attributes: map[resourcev1.QualifiedName]resourcev1.DeviceAttribute{
					"model": {
						StringValue: &gaudi.ModelName,
					},
					"pciRoot": {
						StringValue: &gaudi.PCIRoot,
					},
					"serial": {
						StringValue: &gaudi.Serial,
					},
					"healthy": {
						BoolValue: &gaudi.Healthy,
					},
				},
			},
		}

		devices = append(devices, newDevice)
	}

	driverResource := resourceslice.DriverResources{
		Pools: map[string]resourceslice.Pool{
			s.NodeName: {
				Slices: []resourceslice.Slice{{
					Devices: devices,
				}}}},
	}

	return driverResource
}

// cdiHabanaEnvVar ensures there is a CDI device with name == claimUID, that has
// only env vars for Habana Runtime, without device nodes.
func (s *nodeState) cdiHabanaEnvVar(claimUID string, visibleDevices string) error {
	cdidev := s.CdiCache.GetDevice(claimUID)
	if cdidev != nil { // overwrite the contents
		cdidev.ContainerEdits = cdiSpecs.ContainerEdits{
			Env: []string{visibleDevices},
		}

		// Save into the same spec where the device was found.
		deviceSpec := cdidev.GetSpec()
		specName := path.Base(deviceSpec.GetPath())
		if err := s.CdiCache.WriteSpec(deviceSpec.Spec, specName); err != nil {
			return err
		}

		return nil
	}

	// Create new CDI device and save into first vendor spec.
	newDevice := cdiSpecs.Device{
		Name: claimUID,
		ContainerEdits: cdiSpecs.ContainerEdits{
			Env: []string{visibleDevices},
		},
	}

	if err := cdihelpers.AddDeviceToAnySpec(s.CdiCache, device.CDIVendor, newDevice); err != nil {
		return fmt.Errorf("could not add CDI device into CDI registry: %v", err)
	}

	return nil
}

/*
func (s *nodeState) syncPreparedDevicesFromFile(preparedClaims ClaimPreparations) error {
	klog.V(5).Infof("Syncing %d Prepared allocations from GaudiAllocationState to internal state", len(preparedClaims))

	if s.prepared == nil {
		s.prepared = make(ClaimPreparations)
	}

	for claimuid, preparedDevices := range preparedClaims {
		skipPreparedClaim := false
		prepared := []*device.DeviceInfo{}
		for _, preparedDevice := range preparedDevices {
			klog.V(5).Infof("claim %v had device %+v", claimuid, preparedDevice)

			if _, exists := s.allocatable[preparedDevice.UID]; !exists {
				klog.Errorf("prepared device %v no longer available for claim %v, dropping claim preparation", preparedDevice.UID, claimuid)
				skipPreparedClaim = true
				break
			}

			newdevice := s.allocatable[preparedDevice.UID].DeepCopy()
			prepared = append(prepared, newdevice)
		}

		if !skipPreparedClaim {
			s.prepared[claimuid] = prepared
		}
	}

	return nil
}
*/

func (s *nodeState) Prepare(ctx context.Context, claim *resourcev1.ResourceClaim) error {
	// To prevent concurrent writing of prepared claims file and potential data loss.
	s.Lock()
	defer s.Unlock()

	if claim.Status.Allocation == nil {
		return fmt.Errorf("no allocation found in claim %v/%v status", claim.Namespace, claim.Name)
	}

	allocatedDevices, err := s.prepareAllocatedDevices(ctx, claim)
	if err != nil {
		return err
	}

	s.Prepared[string(claim.UID)] = allocatedDevices

	if err = helpers.WritePreparedClaimsToFile(s.PreparedClaimsFilePath, s.Prepared); err != nil {
		klog.Errorf("Error writing prepared claims to file: %v", err)
		return fmt.Errorf("failed to write prepared claims to file: %v", err)
	}

	klog.V(5).Infof("Created prepared claim %v allocation", claim.UID)
	return nil
}

func (s *nodeState) prepareAllocatedDevices(ctx context.Context, claim *resourcev1.ResourceClaim) (allocatedDevices kubeletplugin.PrepareResult, err error) {
	allocatedDevices = kubeletplugin.PrepareResult{}
	visibleDevices := device.VisibleDevicesEnvVarName + "="

	for _, allocatedDevice := range claim.Status.Allocation.Devices.Results {
		// ATM the only pool is cluster node's pool: all devices on current node.
		if allocatedDevice.Driver != device.DriverName || allocatedDevice.Pool != s.NodeName {
			klog.Infof("ignoring claim allocation device %+v", allocatedDevice)
			continue
		}

		allocatableDevices, _ := s.Allocatable.(map[string]*device.DeviceInfo)

		allocatableDevice, found := allocatableDevices[allocatedDevice.Device]
		if !found {
			return allocatedDevices, fmt.Errorf("could not find allocatable device %v (pool %v)", allocatedDevice.Device, allocatedDevice.Pool)
		}

		newDevice := kubeletplugin.Device{
			Requests:     []string{allocatedDevice.Request},
			PoolName:     allocatedDevice.Pool,
			DeviceName:   allocatedDevice.Device,
			CDIDeviceIDs: []string{allocatableDevice.CDIName()},
		}
		allocatedDevices.Devices = append(allocatedDevices.Devices, newDevice)

		if len(allocatedDevices.Devices) > 1 {
			visibleDevices += ","
		}
		visibleDevices += fmt.Sprintf("%v", allocatableDevice.DeviceIdx)
	}

	if len(allocatedDevices.Devices) > 0 {
		if err := s.cdiHabanaEnvVar(string(claim.UID), visibleDevices); err != nil {
			return allocatedDevices, fmt.Errorf("failed ensuring Habana Runtime specific CDI device: %v", err)
		}

		cdiName := cdiparser.QualifiedName(device.CDIVendor, device.CDIClass, string(claim.UID))
		allocatedDevices.Devices[0].CDIDeviceIDs = append(allocatedDevices.Devices[0].CDIDeviceIDs, cdiName)
	}

	return allocatedDevices, nil
}

func (s *nodeState) AllocatableByPCIAddress(pciAddress string) *device.DeviceInfo {
	allocatableDevices, _ := s.Allocatable.(map[string]*device.DeviceInfo)
	for _, device := range allocatableDevices {
		if device.PCIAddress == pciAddress {
			return device
		}
	}

	return nil
}
