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

	inf "gopkg.in/inf.v0"
	resourcev1 "k8s.io/api/resource/v1beta1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/dynamic-resource-allocation/resourceslice"
	"k8s.io/klog/v2"
	drav1 "k8s.io/kubelet/pkg/apis/dra/v1beta1"
	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"

	cdihelpers "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/cdihelpers"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/helpers"
)

type nodeState struct {
	*helpers.NodeState
}

func newNodeState(detectedDevices map[string]*device.DeviceInfo, cdiRoot string, preparedClaimFilePath string, sysfsRoot string, nodeName string) (*helpers.NodeState, error) {
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
			SysfsRoot:              sysfsRoot,
			NodeName:               nodeName,
		},
	}

	allocatableDevices, ok := state.Allocatable.(map[string]*device.DeviceInfo)
	if !ok {
		return nil, fmt.Errorf("unexpected type for state.Allocatable")
	}
	for duid, ddev := range allocatableDevices {
		klog.V(5).Infof("Allocatable device: %v : %+v", duid, ddev)
	}

	return state.NodeState, nil
}

func (s *nodeState) GetResources() resourceslice.DriverResources {
	devices := []resourcev1.Device{}

	allocatableDevices, _ := s.Allocatable.(map[string]*device.DeviceInfo)

	for gpuUID, gpu := range allocatableDevices {
		newDevice := resourcev1.Device{
			Name: gpuUID,
			Basic: &resourcev1.BasicDevice{
				Attributes: map[resourcev1.QualifiedName]resourcev1.DeviceAttribute{
					"model": {
						StringValue: &gpu.ModelName,
					},
					"family": {
						StringValue: &gpu.FamilyName,
					},
					"pciId": {
						StringValue: &gpu.Model,
					},
					"pciAddress": {
						StringValue: &gpu.PCIAddress,
					},
				},
				Capacity: map[resourcev1.QualifiedName]resourcev1.DeviceCapacity{
					"memory":     {Value: resource.MustParse(fmt.Sprintf("%vMi", gpu.MemoryMiB))},
					"millicores": {Value: *resource.NewDecimalQuantity(*inf.NewDec(int64(1000), inf.Scale(0)), resource.DecimalSI)},
				},
			},
		}

		devices = append(devices, newDevice)
	}

	return resourceslice.DriverResources{Pools: map[string]resourceslice.Pool{
		s.NodeName: {Slices: []resourceslice.Slice{{Devices: devices}}}}}
}

func (s *nodeState) Prepare(ctx context.Context, claim *resourcev1.ResourceClaim) error {
	if claim.Status.Allocation == nil {
		return fmt.Errorf("no allocation found in claim %v/%v status", claim.Namespace, claim.Name)
	}

	allocatedDevices := []*drav1.Device{}

	for _, allocatedDevice := range claim.Status.Allocation.Devices.Results {
		// ATM the only pool is cluster node's pool: all devices on current node.
		if allocatedDevice.Driver != device.DriverName || allocatedDevice.Pool != s.NodeName {
			klog.FromContext(ctx).Info("ignoring claim allocation device", "device pool", allocatedDevice.Pool, "device driver", allocatedDevice.Driver,
				"expected pool", s.NodeName, "expected driver", device.DriverName)
			continue
		}

		allocatableDevices, _ := s.Allocatable.(map[string]*device.DeviceInfo)

		allocatableDevice, found := allocatableDevices[allocatedDevice.Device]
		if !found {
			return fmt.Errorf("could not find allocatable device %v (pool %v)", allocatedDevice.Device, allocatedDevice.Pool)
		}

		newDevice := drav1.Device{
			RequestNames: []string{allocatedDevice.Request},
			PoolName:     allocatedDevice.Pool,
			DeviceName:   allocatedDevice.Device,
			CDIDeviceIDs: []string{allocatableDevice.CDIName()},
		}
		allocatedDevices = append(allocatedDevices, &newDevice)
	}

	s.Prepared[string(claim.UID)] = allocatedDevices

	err := helpers.WritePreparedClaimsToFile(s.PreparedClaimsFilePath, s.Prepared)
	if err != nil {
		klog.Errorf("Error writing prepared claims to file: %v", err)
		return fmt.Errorf("failed to write prepared claims to file: %v", err)
	}

	klog.V(5).Infof("Created prepared claim %v allocation", claim.UID)
	return nil
}
