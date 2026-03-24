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
	"k8s.io/utils/ptr"
	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"

	cdihelpers "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/cdihelpers"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/drm"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/helpers"
)

type nodeState struct {
	*helpers.NodeState
}

func newNodeState(detectedDevices map[string]*device.DeviceInfo, cdiRoot string, preparedClaimFilePath string, sysfsRoot string, nodeName string) (*nodeState, error) {
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

	return &state, nil
}

func (s *nodeState) GetResources() resourceslice.DriverResources {
	s.Lock()
	defer s.Unlock()

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
				if healthStatus == device.HealthUnhealthy {
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

		// If the GPU is neither DRM bound nor prepared, add a taint
		if !gpu.IsDRMBound() {
			if s.isDevicePrepared(gpuUID) {
				devices = append(devices, newDevice)
				continue
			}

			currentDriverInKey := gpu.CurrentDriver
			if currentDriverInKey == "" {
				currentDriverInKey = "unbound"
			}
			newDevice.Taints = append(newDevice.Taints, resourcev1.DeviceTaint{
				Key:    "NotDRMBound-" + currentDriverInKey,
				Effect: resourcev1.DeviceTaintEffectNoSchedule,
			})
		}

		devices = append(devices, newDevice)
	}

	return resourceslice.DriverResources{Pools: map[string]resourceslice.Pool{
		s.NodeName: {Slices: []resourceslice.Slice{{Devices: devices}}}}}
}

func (s *nodeState) Prepare(ctx context.Context, claim *resourcev1.ResourceClaim) error {
	s.Lock()
	defer s.Unlock()

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

		adminAccess := ptr.Deref(allocatedDevice.AdminAccess, false)
		if !adminAccess && s.isDeviceUsedExclusivelyAlready(allocatedDevice.Device, allocatedDevice.Pool, string(claim.UID)) {
			return fmt.Errorf(
				"device %v (pool %v) is already allocated to another claim and cannot be prepared without adminAccess flag",
				allocatedDevice.Device, allocatedDevice.Pool)
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

// isDeviceUsedExclusivelyAlready returns true if the device is already in use in some other claim and
// adminAccess flag is not set.
// TODO: FIXME: shareID needs to be checked as well but it is not in kubeletplugin.PrepareResult,
// and therefore it is not currently stored in cached preparedClaims file or in s.Prepared.
func (s *nodeState) isDeviceUsedExclusivelyAlready(deviceName, poolName, claimUID string) bool {
	for preparedClaimUID, claim := range s.Prepared {
		// Ignore currently processed claim if it was prepared before.
		if preparedClaimUID == claimUID {
			continue
		}

		for _, preparedDevice := range claim.Devices {
			if preparedDevice.DeviceName == deviceName && preparedDevice.PoolName == poolName {
				// TODO: FIXME: check for shareID when consumableCapacity is supported.
				return true
			}
		}
	}
	return false
}

func (s *nodeState) IsDeviceDRMBound(deviceUID string) bool {
	s.Lock()
	defer s.Unlock()

	allocatableDevices, _ := s.Allocatable.(map[string]*device.DeviceInfo)
	gpu := allocatableDevices[deviceUID]

	return gpu.IsDRMBound()
}

func (s *nodeState) RefreshDeviceOnDriverEvent(deviceUID, currentDriver string) error {
	s.Lock()
	defer s.Unlock()

	// nolint:forcetypeassert
	allocatable := s.Allocatable.(map[string]*device.DeviceInfo)
	gpu := allocatable[deviceUID]
	gpu.CurrentDriver = currentDriver
	if gpu.CurrentDriver == "" {
		return nil
	}

	sysfsDriverDeviceDir := path.Join(s.SysfsRoot, device.SysfsPCIBuspath, gpu.Driver, gpu.PCIAddress)
	cardIdx, renderIdx, err := drm.DeduceCardAndRenderdIndexes(sysfsDriverDeviceDir)
	if err != nil {
		return fmt.Errorf("could not deduce card/render indexes for PCI address %s: %v", gpu.PCIAddress, err)
	}

	// If the GPU is already DRM bound and the indexes haven't changed, no need to refresh the CDI registry.
	if gpu.CardIdx == cardIdx && gpu.RenderdIdx == renderIdx {
		return nil
	}

	gpu.CardIdx = cardIdx
	gpu.RenderdIdx = renderIdx

	// Refreshing the CDI registry with updated device information
	cdiCache := cdiapi.GetDefaultCache()
	if err := cdihelpers.AddDetectedDevicesToCDIRegistry(cdiCache, allocatable); err != nil {
		return fmt.Errorf("failed to add detected devices to CDI registry: %v", err)
	}

	return nil
}

func (s *nodeState) IsDevicePrepared(deviceUID string) bool {
	s.Lock()
	defer s.Unlock()

	return s.isDevicePrepared(deviceUID)
}

func (s *nodeState) isDevicePrepared(deviceUID string) bool {

	for _, preparedClaim := range s.Prepared {
		for _, preparedDevice := range preparedClaim.Devices {
			if preparedDevice.DeviceName == deviceUID {
				return true
			}
		}
	}

	return false
}

func (s *nodeState) getDeviceUIDFromPCIAddress(pciAddress string) (string, error) {
	s.Lock()
	defer s.Unlock()
	// nolint:forcetypeassert
	allocatable := s.Allocatable.(map[string]*device.DeviceInfo)

	for deviceUID, deviceInfo := range allocatable {
		if deviceInfo.PCIAddress == pciAddress {
			return deviceUID, nil
		}
	}

	return "", fmt.Errorf("no device found with PCI address %s", pciAddress)
}

func (s *nodeState) devpathContainsGPUPCIAddress(devpath string) bool {
	s.Lock()
	defer s.Unlock()

	allocatableDevices, _ := s.Allocatable.(map[string]*device.DeviceInfo)

	for _, gpu := range allocatableDevices {
		if strings.Contains(devpath, gpu.PCIAddress) {
			return true
		}
	}
	return false
}

// applyDeviceUpdates processes XPUMD-supplied device details and health, and
// returns a bool of whether ResourceSlice update and publication is needed,
// and a possible error.
func (s *nodeState) applyDeviceUpdates(newDevicesInfo device.DevicesInfo) (bool, error) {
	s.Lock()
	defer s.Unlock()

	needToPublish := false

	//nolint:forcetypeassert // We want the code to panic if our assumption turns out to be wrong.
	allocatable := s.Allocatable.(map[string]*device.DeviceInfo)

	for deviceUID, newDeviceInfo := range newDevicesInfo {
		klog.V(5).Infof("Checking device %v info", deviceUID)
		foundDevice, found := allocatable[deviceUID]
		if !found {
			// TODO: re-discover to check if new device was hot-plugged.
			return false, fmt.Errorf("could not find allocatable device with UID %v", deviceUID)
		}

		// Apply memory change if any:
		// - if DRA driver runs in non-privileged mode, XPUMD info can provide memory info.
		// - PF can change it's memory amount when VFs are enabled or disabled.
		if foundDevice.MemoryMiB != newDeviceInfo.MemoryMiB {
			klog.Infof("Device %v memory changed from %v MiB to %v MiB", deviceUID, foundDevice.MemoryMiB, newDeviceInfo.MemoryMiB)
			foundDevice.MemoryMiB = newDeviceInfo.MemoryMiB
			needToPublish = true
		}

		// Only overall foundDevice.Health is exposed in the ResourceSlice Device, and not foundDevice.HealshStatus.
		// Overall health is a logical AND of all HealthStatus elements. If the overall health changes - the new
		// ResourceSlice needs to be published.
		for newHealthType, newHealthStatus := range newDeviceInfo.HealthStatus {
			oldHealthValue, oldHealthFound := foundDevice.HealthStatus[newHealthType]
			// If
			// - the health was known before and has changed
			// - health was not known before and new status is not healthy
			if (oldHealthFound && oldHealthValue != newHealthStatus) || (!oldHealthFound && newHealthStatus == device.HealthUnhealthy) {
				klog.Infof("Device %v health status for %v changed from %v to %v", deviceUID, newHealthType, oldHealthValue, newHealthStatus)
				needToPublish = true
			}
		}

		// Check if some previously known health status is no longer reported. If it was known to be
		// unhealthy last time - consider its absence as healthy and indicate ResourceSlice
		// update is needed.
		for oldHealthType, oldHealthValue := range foundDevice.HealthStatus {
			if _, healthReported := newDeviceInfo.HealthStatus[oldHealthType]; !healthReported && oldHealthValue == device.HealthUnhealthy {
				klog.Infof("Device %v health status for %v is no longer reported, considered healthy", deviceUID, oldHealthType)
				needToPublish = true
			}
		}

		// Finally, overwrite the health status with the new one as a whole.
		foundDevice.HealthStatus = newDeviceInfo.HealthStatus
		foundDevice.Health = newDeviceInfo.Health

		klog.V(5).Infof("Updated health status for device: %v to: overall: %v; details: %v", deviceUID, foundDevice.Health, foundDevice.HealthStatus)
	}

	return needToPublish, nil
}
