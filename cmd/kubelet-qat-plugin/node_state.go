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
	"time"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/helpers"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/qat/cdihelpers"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/qat/device"

	resourcev1 "k8s.io/api/resource/v1"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
	"k8s.io/dynamic-resource-allocation/resourceslice"
	"k8s.io/klog/v2"
	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"
)

type nodeState struct {
	*helpers.NodeState
}

func newNodeState(detectedDevices device.VFDevices, cdiRoot string, preparedClaimFilePath string, nodeName string) (*nodeState, error) {
	for ddev := range detectedDevices {
		klog.V(3).Infof("new device: %+v", ddev)
	}

	klog.V(5).Info("Refreshing CDI registry")
	if err := cdiapi.Configure(cdiapi.WithSpecDirs(cdiRoot)); err != nil {
		return nil, fmt.Errorf("unable to refresh the CDI registry: %v", err)
	}

	cdiCache := cdiapi.GetDefaultCache()

	if err := cdihelpers.AddDetectedDevicesToCDIRegistry(cdiCache, detectedDevices); err != nil {
		return nil, fmt.Errorf("cannot sync CDI devices: %v", err)
	}

	// hack for tests on slow machines
	time.Sleep(250 * time.Millisecond)

	klog.V(5).Info("Allocatable devices after CDI registry refresh:")
	for duid, ddev := range detectedDevices {
		klog.V(5).Infof("CDI device: %v : %+v", duid, ddev)
	}

	preparedClaims, err := helpers.GetOrCreatePreparedClaims(preparedClaimFilePath)
	if err != nil {
		klog.Errorf("Error getting prepared claims: %v", err)
		return nil, fmt.Errorf("failed to get prepared claims: %v", err)
	}

	klog.V(5).Info("Creating NodeState")
	state := nodeState{
		NodeState: &helpers.NodeState{
			CdiCache:               cdiCache,
			Allocatable:            detectedDevices,
			Prepared:               preparedClaims,
			PreparedClaimsFilePath: preparedClaimFilePath,
			NodeName:               nodeName,
		},
	}

	//nolint:forcetypeassert
	allocatableDevices := state.Allocatable.(device.VFDevices)
	for duid, ddev := range allocatableDevices {
		klog.V(5).Infof("Allocatable device: %v : %+v", duid, ddev)
	}

	return &state, nil
}

func (s *nodeState) Prepare(ctx context.Context, claim *resourcev1.ResourceClaim) error {
	s.Lock()
	defer s.Unlock()

	preparedDevices := kubeletplugin.PrepareResult{}

	for _, allocatedDevice := range claim.Status.Allocation.Devices.Results {
		if allocatedDevice.Driver != device.DriverName || allocatedDevice.Pool != s.NodeName {
			klog.V(5).Infof("Driver/pool '%s/%s' not handled by driver (%s/%s)",
				allocatedDevice.Driver, allocatedDevice.Pool,
				device.DriverName, s.NodeName)

			continue
		}

		requestedDeviceUID := allocatedDevice.Device
		klog.V(5).Infof("Requested device UID '%s'", requestedDeviceUID)

		allocatableDevices, _ := s.Allocatable.(device.VFDevices)
		allocatableDevice, found := allocatableDevices[requestedDeviceUID]
		if !found {
			return fmt.Errorf("could not find allocatable device %v (pool %v)", allocatedDevice.Device, allocatedDevice.Pool)
		}

		if _, _, err := s.Allocate(requestedDeviceUID, device.Unset, string(claim.UID)); err != nil {
			for _, vf := range allocatableDevices {
				_, _ = vf.Free(string(claim.UID))
			}
			return fmt.Errorf("could not allocate device '%s' for claim '%s': %v", requestedDeviceUID, claim.UID, err)
		}

		cdiDeviceName := allocatableDevice.CDIName()
		controlDeviceNode, _ := device.GetControlNode()
		controlDeviceName := device.CDIKind + "=" + controlDeviceNode.UID()
		klog.V(5).Infof("Allocated CDI devices '%s' and '%s' for claim '%s'", cdiDeviceName, controlDeviceName, claim.GetUID())

		// add device
		newDevice := kubeletplugin.Device{
			Requests:     []string{allocatedDevice.Request},
			PoolName:     allocatedDevice.Pool,
			DeviceName:   requestedDeviceUID,
			CDIDeviceIDs: []string{cdiDeviceName, controlDeviceName},
		}
		preparedDevices.Devices = append(preparedDevices.Devices, newDevice)
	}

	s.Prepared[string(claim.UID)] = preparedDevices

	if err := helpers.WritePreparedClaimsToFile(s.PreparedClaimsFilePath, s.Prepared); err != nil {
		klog.Errorf("failed to write prepared claims to file: %v", err)
		return fmt.Errorf("failed to write prepared claims to file: %v", err)
	}

	klog.V(5).Infof("Created prepared claim %v allocation", claim.UID)
	return nil
}

func (s *nodeState) Allocate(requestedDeviceUID string, requestedService device.Services, requestedBy string) (*device.VFDevice, bool, error) {
	//nolint:forcetypeassert
	allocatableDevices := s.Allocatable.(device.VFDevices)
	allocatableDevice := allocatableDevices[requestedDeviceUID]

	if allocatableDevice.CheckAlreadyAllocated(requestedService, requestedBy) {
		return allocatableDevice, false, nil
	}

	if allocatableDevice.AllocateFromConfigured(requestedService, requestedBy) {
		return allocatableDevice, false, nil
	}

	if allocatableDevice.AllocateWithReconfiguration(requestedService, requestedBy) {
		return allocatableDevice, true, nil
	}

	return nil, false, fmt.Errorf("could not allocate device '%s', service '%s' from any device", requestedDeviceUID, requestedService.String())
}

func (s *nodeState) Unprepare(ctx context.Context, claim kubeletplugin.NamespacedObject) (bool, error) {

	for _, requestedDevice := range s.Prepared[string(claim.UID)].Devices {
		allocatableDevices, _ := s.Allocatable.(device.VFDevices)
		requestedDevice := allocatableDevices[requestedDevice.DeviceName]

		var updated bool
		var err error

		if err = s.NodeState.Unprepare(ctx, string(claim.UID)); err != nil {
			return false, fmt.Errorf("error unpreparing claim %s: %v", claim.UID, err)
		}

		if updated, err = requestedDevice.Free(string(claim.UID)); err != nil {
			klog.Warningf("Could not free device %s claim '%s': %v", requestedDevice.UID(), claim.UID, err)
		}
		klog.V(5).Infof("Claim with uid '%s' freed", claim.UID)

		if updated {
			return updated, nil
		}
	}
	return false, nil

}

func (s *nodeState) GetResources() resourceslice.DriverResources {
	//nolint:forcetypeassert // We want the code to panic if our assumption turns out to be wrong.
	allocatableDevices := s.Allocatable.(device.VFDevices)
	klog.V(5).Infof("allocatable devices in GetResources: %v", allocatableDevices)
	return resourceslice.DriverResources{
		Pools: map[string]resourceslice.Pool{
			s.NodeName: {
				Slices: []resourceslice.Slice{{
					Devices: *deviceResources(allocatableDevices),
				}}}},
	}
}
