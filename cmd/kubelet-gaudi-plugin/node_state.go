/*
 * Copyright (c) 2025-2026, Intel Corporation.  All Rights Reserved.
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
	"strings"
	"time"

	resourcev1 "k8s.io/api/resource/v1"
	"k8s.io/dynamic-resource-allocation/deviceattribute"
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
	gaudiHookPath string
	gaudiNetPath  string
}

func newNodeState(detectedDevices map[string]*device.DeviceInfo, cdiRoot, preparedClaimsFilePath, nodeName, gaudiHookPath string) (*nodeState, error) {
	for ddev := range detectedDevices {
		klog.V(3).Infof("new device: %+v", ddev)
	}

	klog.V(5).Info("Refreshing CDI registry")
	if err := cdiapi.Configure(cdiapi.WithSpecDirs(cdiRoot)); err != nil {
		return nil, fmt.Errorf("unable to refresh the CDI registry: %v", err)
	}

	cdiCache := cdiapi.GetDefaultCache()

	if err := cdihelpers.AddDetectedDevicesToCDIRegistry(cdiCache, detectedDevices); err != nil {
		return nil, fmt.Errorf("unable to add detected devices to CDI registry: %v", err)
	}

	time.Sleep(250 * time.Millisecond)

	klog.V(5).Info("Allocatable devices after CDI registry refresh:")
	for duid, ddev := range detectedDevices {
		klog.V(5).Infof("CDI device: %v : %+v", duid, ddev)
	}

	// TODO: should be only create prepared claims, discard old preparations. Do we even need the snapshot?
	preparedClaims, err := helpers.GetOrCreatePreparedClaims(preparedClaimsFilePath)
	if err != nil {
		klog.Errorf("failed to get prepared claims: %v", err)
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
		gaudiHookPath: gaudiHookPath,
	}

	allocatableDevices, ok := state.Allocatable.(map[string]*device.DeviceInfo)
	if !ok {
		return nil, fmt.Errorf("unexpected type for state.Allocatable")
	}

	klog.V(5).Infof("Synced state with CDI and GaudiAllocationState: %+v", state)
	for duid, ddev := range allocatableDevices {
		klog.V(5).Infof("Allocatable device: %v : %+v", duid, ddev)
	}

	return &state, nil
}

func (s *nodeState) GetResources() resourceslice.DriverResources {
	s.Lock()
	defer s.Unlock()

	devices := []resourcev1.Device{}

	allocatableDevices, _ := s.Allocatable.(map[string]*device.DeviceInfo)
	for gaudiUID, gaudi := range allocatableDevices {
		newDevice := resourcev1.Device{
			Name: gaudiUID,
			Attributes: map[resourcev1.QualifiedName]resourcev1.DeviceAttribute{
				"model": {
					StringValue: &gaudi.ModelName,
				},
				deviceattribute.StandardDeviceAttributePCIeRoot: {
					StringValue: &gaudi.PCIRoot,
				},
				"serial": {
					StringValue: &gaudi.Serial,
				},
				"healthy": {
					BoolValue: &gaudi.Healthy,
				},
			},
		}

		// pciRoot Device.DeviceAttribute is deprecated: will be removed in 1.0.0 release, use resource.kubernetes.io/pcieRoot'.
		// For backwards compatibility, strip domain, only bus was in the value.
		if len(gaudi.PCIRoot) > 0 {
			parts := strings.Split(gaudi.PCIRoot, ":")
			if len(parts) == 2 {
				newDevice.Attributes["pciRoot"] = resourcev1.DeviceAttribute{
					StringValue: &parts[1],
				}
			}
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
func (s *nodeState) cdiHabanaEnvVar(claimUID string, visibleDevices string, visibleModules string, hlVisibleDevices string) error {
	cdidev := s.CdiCache.GetDevice(claimUID)
	if cdidev != nil { // overwrite the contents
		cdidev.ContainerEdits = cdiSpecs.ContainerEdits{
			Env: []string{visibleDevices, visibleModules, hlVisibleDevices},
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
			Env: []string{visibleDevices, visibleModules, hlVisibleDevices},
		},
	}

	if err := cdihelpers.NewBlankDevice(s.CdiCache, newDevice, s.gaudiHookPath); err != nil {
		return fmt.Errorf("could not add CDI device into CDI registry: %v", err)
	}

	return nil
}

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
		klog.Errorf("failed to write prepared claims to file: %v", err)
		return fmt.Errorf("failed to write prepared claims to file: %v", err)
	}

	klog.V(5).Infof("Created prepared claim %v allocation", claim.UID)
	return nil
}

func (s *nodeState) prepareAllocatedDevices(ctx context.Context, claim *resourcev1.ResourceClaim) (allocatedDevices kubeletplugin.PrepareResult, err error) {
	allocatedDevices = kubeletplugin.PrepareResult{}
	visibleDeviceIndices := []string{}
	visibleModuleIndices := []string{}
	hlVisibleDevicePaths := []string{}
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

		visibleDeviceIndices = append(visibleDeviceIndices, fmt.Sprintf("%d", allocatableDevice.DeviceIdx))
		visibleModuleIndices = append(visibleModuleIndices, fmt.Sprintf("%d", allocatableDevice.ModuleIdx))
		hlVisibleDevicePaths = append(hlVisibleDevicePaths, fmt.Sprintf("/dev/accel/accel%d", allocatableDevice.DeviceIdx))
	}

	if len(allocatedDevices.Devices) > 0 {
		visibleDevicesEnvVar := fmt.Sprintf("%s=%s", device.VisibleDevicesEnvVarName, strings.Join(visibleDeviceIndices, ","))
		visibleModulesEnvVar := fmt.Sprintf("%s=%s", device.VisibleModulesEnvVarName, strings.Join(visibleModuleIndices, ","))
		hlVisibleDevicesEnvVar := fmt.Sprintf("%s=%s", device.HLVisibleDevicesEnvVarName, strings.Join(hlVisibleDevicePaths, ","))

		if err := s.cdiHabanaEnvVar(string(claim.UID), visibleDevicesEnvVar, visibleModulesEnvVar, hlVisibleDevicesEnvVar); err != nil {
			return allocatedDevices, fmt.Errorf("failed to ensure Habana Runtime specific CDI device: %v", err)
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
