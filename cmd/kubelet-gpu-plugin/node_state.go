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
	"sort"
	"strings"
	"time"

	inf "gopkg.in/inf.v0"
	resourcev1 "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/dynamic-resource-allocation/deviceattribute"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
	"k8s.io/dynamic-resource-allocation/resourceslice"
	"k8s.io/klog/v2"
	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"

	cdihelpers "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/cdihelpers"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/helpers"
)

type nodeState struct {
	*helpers.NodeState
	ignoreHealthWarning bool // true if Warning status means healthy, false otherwise.
}

func newNodeState(detectedDevices map[string]*device.DeviceInfo, cdiRoot string, preparedClaimFilePath string, sysfsRoot string, nodeName string, ignoreHealthWarning bool) (*nodeState, error) {
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
		ignoreHealthWarning: ignoreHealthWarning,
	}

	allocatableDevices, ok := state.Allocatable.(map[string]*device.DeviceInfo)
	if !ok {
		return nil, fmt.Errorf("unexpected type for state.Allocatable")
	}
	for duid, ddev := range allocatableDevices {
		klog.V(5).Infof("Allocatable device: %v : %+v", duid, ddev)
	}

	return &state, nil
}

func (s *nodeState) GetResources() resourceslice.DriverResources {
	devices := []resourcev1.Device{}

	allocatableDevices, _ := s.Allocatable.(map[string]*device.DeviceInfo)

	for gpuUID, gpu := range allocatableDevices {
		sriovSupported := gpu.MaxVFs > 0
		newDevice := resourcev1.Device{
			Name: gpuUID,
			Attributes: map[resourcev1.QualifiedName]resourcev1.DeviceAttribute{
				"model": {
					StringValue: &gpu.ModelName,
				},
				"family": {
					StringValue: &gpu.FamilyName,
				},
				"driver": {
					StringValue: &gpu.Driver,
				},
				"sriov": {
					BoolValue: &sriovSupported,
				},
				"pciId": {
					StringValue: &gpu.Model,
				},
				// Deprecated: will be removed in 1.0.0 release, use 'resource.kubernetes.io/pciBusID'.
				"pciAddress": {
					StringValue: &gpu.PCIAddress,
				},
				"health": {
					StringValue: &gpu.Health,
				},
				deviceattribute.StandardDeviceAttributePCIeRoot: {
					StringValue: &gpu.PCIRoot,
				},
				deviceattribute.StandardDeviceAttributePrefix + helpers.DRADeviceAttributePCIBusIDSuffix: {
					StringValue: &gpu.PCIAddress,
				},
			},
			Capacity: map[resourcev1.QualifiedName]resourcev1.DeviceCapacity{
				"memory":     {Value: resource.MustParse(fmt.Sprintf("%vMi", gpu.MemoryMiB))},
				"millicores": {Value: *resource.NewDecimalQuantity(*inf.NewDec(int64(1000), inf.Scale(0)), resource.DecimalSI)},
			},
		}

		// pciRoot Device.DeviceAttribute is deprecated: will be removed in 1.0.0 release, use resource.kubernetes.io/pcieRoot'.
		// For backwards compatibility, strip domain, only bus was in the value.
		if len(gpu.PCIRoot) > 0 {
			parts := strings.Split(gpu.PCIRoot, ":")
			if len(parts) == 2 {
				newDevice.Attributes["pciRoot"] = resourcev1.DeviceAttribute{
					StringValue: &parts[1],
				}
			}
		}

		// FIXME: TODO: K8s 1.33-1.34 only supports plain taint without description.
		// See https://github.com/kubernetes/enhancements/issues/5055 .
		if gpu.Health == device.HealthUnhealthy {
			// e.g. HealthIssues-memorytemperature_coretemperature:NoExecute
			// The format will change in K8s 1.35+.
			unhealthyTypes := []string{}
			for healthType, healthStatus := range gpu.HealthStatus {
				if !s.StatusHealth(healthStatus) {
					unhealthyTypes = append(unhealthyTypes, healthType)
				}
			}
			sort.Strings(unhealthyTypes)
			key := "HealthIssues-" + strings.Join(unhealthyTypes, "_")
			key = strings.ReplaceAll(key, "[", "")
			key = strings.ReplaceAll(key, "]", "")
			key = strings.ReplaceAll(key, ",", "_")
			newDevice.Taints = []resourcev1.DeviceTaint{{
				Key:    key,
				Effect: resourcev1.DeviceTaintEffectNoExecute,
			}}
		}

		devices = append(devices, newDevice)
	}

	return resourceslice.DriverResources{Pools: map[string]resourceslice.Pool{
		s.NodeName: {Slices: []resourceslice.Slice{{Devices: devices}}}}}
}

func (s *nodeState) StatusHealth(status string) (health bool) {
	switch status {
	case "Critical":
		return false
	case "Warning":
		return s.ignoreHealthWarning
	case "OK":
		return true
	case "Unknown":
		return true
	default:
		// This is unexpected, we should never get here.
		klog.Error("Unsupported health status value: ", status)
		panic("invalid status value")
	}
}

func (s *nodeState) Prepare(ctx context.Context, claim *resourcev1.ResourceClaim) error {
	if claim.Status.Allocation == nil {
		return fmt.Errorf("no allocation found in claim %v/%v status", claim.Namespace, claim.Name)
	}

	preparedDevices := kubeletplugin.PrepareResult{}

	for _, allocatedDevice := range claim.Status.Allocation.Devices.Results {
		// ATM the only pool is cluster node's pool: all devices on current node.
		if allocatedDevice.Driver != device.DriverName || allocatedDevice.Pool != s.NodeName {
			klog.FromContext(ctx).Info("ignoring claim allocation device", "device", allocatedDevice, "expected pool", s.NodeName, "expected driver", device.DriverName)
			continue
		}

		allocatableDevices, _ := s.Allocatable.(map[string]*device.DeviceInfo)

		allocatableDevice, found := allocatableDevices[allocatedDevice.Device]
		if !found {
			return fmt.Errorf("could not find allocatable device %v (pool %v)", allocatedDevice.Device, allocatedDevice.Pool)
		}

		newDevice := kubeletplugin.Device{
			Requests:     []string{allocatedDevice.Request},
			PoolName:     allocatedDevice.Pool,
			DeviceName:   allocatedDevice.Device,
			CDIDeviceIDs: []string{allocatableDevice.CDIName()},
		}
		preparedDevices.Devices = append(preparedDevices.Devices, newDevice)
	}

	s.Prepared[string(claim.UID)] = preparedDevices

	err := helpers.WritePreparedClaimsToFile(s.PreparedClaimsFilePath, s.Prepared)
	if err != nil {
		klog.Errorf("Error writing prepared claims to file: %v", err)
		return fmt.Errorf("failed to write prepared claims to file: %v", err)
	}

	klog.V(5).Infof("Created prepared claim %v allocation", claim.UID)
	return nil
}
