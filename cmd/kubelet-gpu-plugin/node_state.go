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
	"sync"
	"time"

	inf "gopkg.in/inf.v0"
	resourcev1 "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/dynamic-resource-allocation/deviceattribute"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
	"k8s.io/dynamic-resource-allocation/resourceslice"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"

	cdihelpers "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/cdihelpers"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/discovery"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/drm"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/vfio"
)

type nodeState struct {
	sync.Mutex
	CdiCache               *cdiapi.Cache
	Allocatable            interface{}
	Prepared               ClaimPreparations
	PreparedClaimsFilePath string
	NodeName               string
	SysfsRoot              string
	ManageBinding          bool
}

func newNodeState(detectedDevices map[string]*device.DeviceInfo, cdiRoot string, preparedClaimFilePath string, sysfsRoot string, nodeName string, manageBinding bool) (*nodeState, error) {
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

	preparedClaims, err := GetOrCreatePreparedClaims(preparedClaimFilePath)
	if err != nil {
		klog.Errorf("Error getting prepared claims: %v", err)
		return nil, fmt.Errorf("failed to get prepared claims: %v", err)
	}

	klog.V(5).Info("Creating NodeState")
	state := nodeState{
		CdiCache:               cdiCache,
		Allocatable:            detectedDevices,
		Prepared:               preparedClaims,
		PreparedClaimsFilePath: preparedClaimFilePath,
		SysfsRoot:              sysfsRoot,
		NodeName:               nodeName,
		ManageBinding:          manageBinding,
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

// GetResources returns resourceslice.DriverResources based on the current node state.
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
					StringValue: &gpu.CurrentDriver,
				},
				"sriov": {
					BoolValue: &sriovSupported,
				},
				"type": {
					StringValue: &gpu.DeviceType,
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
				deviceattribute.StandardDeviceAttributePCIBusID: {
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

		// Taint the device if it is not bound to any kernel driver and binding
		// management is disabled.
		if gpu.CurrentDriver == "" && !s.ManageBinding {
			newDevice.Taints = append(newDevice.Taints, resourcev1.DeviceTaint{
				Key:    device.UnboundUnmanagedTaintKey,
				Effect: resourcev1.DeviceTaintEffectNoSchedule,
			})
		}

		devices = append(devices, newDevice)
	}

	return resourceslice.DriverResources{Pools: map[string]resourceslice.Pool{
		s.NodeName: {Slices: []resourceslice.Slice{{Devices: devices}}}}}
}

// Prepare handles single ResourceClaim devices preparation, including changing
// kernel driver if needed. Returns bool indicating if new ResourceSlice needs
// to be published, and the PrepareResult that will be forwarded to the kubelet.
func (s *nodeState) Prepare(ctx context.Context, claim *resourcev1.ResourceClaim) (needToPublishSlice bool, prepareResult kubeletplugin.PrepareResult) {
	s.Lock()
	defer s.Unlock()

	if claim.Status.Allocation == nil {
		prepareResult.Err = fmt.Errorf("no allocation found in claim %v/%v status", claim.Namespace, claim.Name)
		return
	}

	var err error
	preparedDevices := []PreparedDevice{}
	allocatableDevices, _ := s.Allocatable.(map[string]*device.DeviceInfo)

	for _, allocatedDevice := range claim.Status.Allocation.Devices.Results {
		// ATM the only pool is cluster node's pool: all devices on current node.
		if allocatedDevice.Driver != device.DriverName || allocatedDevice.Pool != s.NodeName {
			klog.FromContext(ctx).Info("ignoring claim allocation device", "device", allocatedDevice, "expected pool", s.NodeName, "expected driver", device.DriverName)
			continue
		}

		// Protection against force-deleted claims making cluster think the device is free.
		adminAccess := ptr.Deref(allocatedDevice.AdminAccess, false)
		if !adminAccess && s.isDeviceUsedExclusivelyAlready(allocatedDevice.Device, allocatedDevice.Pool, claim.UID) {
			prepareResult.Err = fmt.Errorf(
				"device %v (pool %v) is already allocated to another claim and cannot be prepared without adminAccess flag",
				allocatedDevice.Device, allocatedDevice.Pool)
			return
		}

		allocatableDevice, found := allocatableDevices[allocatedDevice.Device]
		if !found {
			prepareResult.Err = fmt.Errorf("could not find allocatable device %v (pool %v)", allocatedDevice.Device, allocatedDevice.Pool)
			return
		}

		deviceClassName := s.getRequestDeviceClassNameFromClaim(allocatedDevice.Request, claim)
		klog.V(5).Infof("Device class name for request %v: %v", allocatedDevice.Request, deviceClassName)
		if deviceClassName == device.VFIODeviceClassName {
			klog.V(5).Infof("Device %v is requested as VFIO device, preparing with VFIO driver", allocatableDevice.PCIAddress)
			needToPublishSlice, err = s.prepareVFIODevice(allocatableDevice)
			if err != nil {
				prepareResult.Err = fmt.Errorf("failed to prepare VFIO device %v: %v", allocatableDevice.PCIAddress, err)
				return
			}
		} else {
			klog.V(5).Infof("Device %v is requested as regular GPU device, preparing with DRM driver", allocatableDevice.PCIAddress)
			needToPublishSlice, err = s.prepareDRMDevice(allocatableDevice)
			if err != nil {
				prepareResult.Err = fmt.Errorf("failed to prepare DRM device %v: %v", allocatableDevice.PCIAddress, err)
				return
			}
		}

		newDevice := PreparedDevice{
			KubeletpluginDevice: kubeletplugin.Device{
				Requests:     []string{allocatedDevice.Request},
				PoolName:     allocatedDevice.Pool,
				DeviceName:   allocatedDevice.Device,
				CDIDeviceIDs: []string{allocatableDevice.CDIName()},
				Metadata: &kubeletplugin.DeviceMetadata{
					Attributes: map[string]resourcev1.DeviceAttribute{
						string(deviceattribute.StandardDeviceAttributePCIBusID): {
							StringValue: &allocatableDevice.PCIAddress,
						},
					},
				},
			},
			AdminAccess: adminAccess,
		}

		if adminAccess && allocatableDevice.MEIName != "" {
			klog.V(5).Infof("Adding MEI CDI device for device %v with MEI name %v", allocatedDevice.Device, allocatableDevice.MEIName)
			newDevice.KubeletpluginDevice.CDIDeviceIDs = append(newDevice.KubeletpluginDevice.CDIDeviceIDs, allocatableDevice.MEICDIName())
		}

		preparedDevices = append(preparedDevices, newDevice)
	}

	s.Prepared[claim.UID] = ClaimPreparation{PreparedDevices: preparedDevices}

	if err = WritePreparedClaimsToFile(s.PreparedClaimsFilePath, s.Prepared); err != nil {
		klog.Errorf("Error writing prepared claims to file: %v", err)
		prepareResult.Err = fmt.Errorf("failed to write prepared claims to file: %v", err)

		return
	}

	klog.V(5).Infof("Created prepared claim %v allocation", claim.UID)
	prepareResult = s.Prepared[claim.UID].PrepareResult()

	return
}

// isDeviceUsedExclusivelyAlready returns true if the device is already in use in some other claim and
// adminAccess flag is not set.
// TODO: FIXME: shareID needs to be checked as well but it is not in kubeletplugin.PrepareResult,
// and therefore it is not currently stored in cached preparedClaims file or in s.Prepared.
func (s *nodeState) isDeviceUsedExclusivelyAlready(deviceName, poolName string, claimUID types.UID) bool {
	for preparedClaimUID, claimPreparation := range s.Prepared {
		// Ignore currently processed claim if it was prepared before.
		if preparedClaimUID == claimUID {
			continue
		}

		for _, preparedDevice := range claimPreparation.PreparedDevices {
			if preparedDevice.AdminAccess {
				continue
			}
			if preparedDevice.KubeletpluginDevice.DeviceName == deviceName && preparedDevice.KubeletpluginDevice.PoolName == poolName {
				// TODO: FIXME: check for shareID when consumableCapacity is supported.
				return true
			}
		}
	}
	return false
}

// RefreshDeviceOnDriverEvent rediscovers the device information and returns bool indicating
// if the device was updated and an updated ResourceSlice needs to be published.
func (s *nodeState) RefreshDeviceOnDriverEvent(pciAddress, expectedDriver string) (bool, error) {
	s.Lock()
	defer s.Unlock()
	needToPublish := false
	needToUpdateCDI := false

	discoveredDeviceInfo, discoveredDeviceInfoErr := discovery.DiscoverPCIDevice(path.Join(s.SysfsRoot, device.SysfsPCIDevicesPath, pciAddress), s.SysfsRoot)
	cachedDeviceInfo, cachedDeviceInfoErr := s.getAllocatableByPCIAddress(pciAddress)
	// cachedDeviceInfoErr, discoveredDeviceInfoErr
	// 0 - 0 : compare fields
	// 0 - 1 : device disappeared?
	// 1 - 0 : new device appeared (e.g. a VF)
	// 1 - 1 : what is happening in udev?!!1!
	//         do we lack permission to discover or sysfs mismounted / mistargeted via env var ?! Just log the error.
	switch {
	case cachedDeviceInfoErr != nil && discoveredDeviceInfoErr != nil:
		klog.Errorf("Device with PCI address %s is not discoverable and was not in allocatable devices: %v. Is sysfs mounted correctly at %v?", pciAddress, discoveredDeviceInfoErr, s.SysfsRoot)
	case cachedDeviceInfoErr == nil && discoveredDeviceInfoErr != nil:
		// TODO: check if it was a VF and remove from slice.
		klog.Warningf("Previously discoverable device with PCI address %s is no longer discoverable, tainting it.", pciAddress)
		cachedDeviceInfo.HealthStatus[device.HealthStatusDeviceAbsent] = device.HealthUnhealthy
		needToPublish = true
	case cachedDeviceInfoErr != nil && discoveredDeviceInfoErr == nil:
		klog.Infof("Previously undiscoverable device with PCI address %s found. Adding to allocatable devices. Device info: %+v", pciAddress, discoveredDeviceInfo)
		// Add to allocatable, CDI, indicate need to publish ResourceSlice.
		needToUpdateCDI = true
		needToPublish = true
		allocatableDevices, _ := s.Allocatable.(map[string]*device.DeviceInfo)
		allocatableDevices[discovery.DetermineDeviceName(discoveredDeviceInfo, device.DefaultNamingStyle)] = discoveredDeviceInfo
	default: // if cachedDeviceInfoErr == nil && discoveredDeviceInfoErr == nil
		// Untaint if the device was observed as absent before.
		if healthStatus, found := cachedDeviceInfo.HealthStatus[device.HealthStatusDeviceAbsent]; found && healthStatus == device.HealthUnhealthy {
			cachedDeviceInfo.HealthStatus[device.HealthStatusDeviceAbsent] = device.HealthHealthy
			needToPublish = true
		}

		if discoveredDeviceInfo.CurrentDriver != expectedDriver {
			// TODO: FIXME: expectedDriver is from udev event. Was there too much lag / lock wait that the next udev already too place and is in queue,
			// and we're processing old event? Ignore the change.
			klog.Warningf("Device %s has unexpected driver after udev event. Expected: %s, actual: %s.", pciAddress, expectedDriver, discoveredDeviceInfo.CurrentDriver)
		} else if cachedDeviceInfo.CurrentDriver != discoveredDeviceInfo.CurrentDriver {
			// Something else than the DRA driver has changed the driver.
			// - If the device was not prepared for workload Pod:
			//   - just update the cached info, no need to taint it
			// - If the device was prepared for workload Pod:
			//   - update cached info
			//   - taint the device
			cachedDeviceInfo.CurrentDriver = discoveredDeviceInfo.CurrentDriver
			cachedDeviceInfo.CardName = discoveredDeviceInfo.CardName
			cachedDeviceInfo.RenderDName = discoveredDeviceInfo.RenderDName
			cachedDeviceInfo.MemoryMiB = discoveredDeviceInfo.MemoryMiB
			cachedDeviceInfo.MEIName = discoveredDeviceInfo.MEIName
			cachedDeviceInfo.VFIODevice = discoveredDeviceInfo.VFIODevice
			cachedDeviceInfo.IOMMUGroup = discoveredDeviceInfo.IOMMUGroup
			cachedDeviceInfo.VFIndex = discoveredDeviceInfo.VFIndex
			if s.isDeviceUsedExclusivelyAlready(cachedDeviceInfo.UID, s.NodeName, "") {
				// This taint is removed when the device is unprepared.
				cachedDeviceInfo.HealthStatus[device.HealthStatusUnexpectedDriver] = device.HealthUnhealthy
			}
			needToPublish = true
		} // TODO: SR-IOV: handle number of VFs changed on PF.
	}

	if needToUpdateCDI {
		if err := cdihelpers.UpdateGPUDevices(s.CdiCache, []*device.DeviceInfo{discoveredDeviceInfo}); err != nil {
			return needToPublish, fmt.Errorf("failed to add detected devices to CDI registry: %v", err)
		}
	}

	return needToPublish, nil
}

// Unprepare handles single ResourceClaim devices unpreparation, including changing.
func (s *nodeState) Unprepare(ctx context.Context, claimUID types.UID) (needToPublishSlice bool, err error) {
	s.Lock()
	defer s.Unlock()
	var unwrappedErr error

	if _, found := s.Prepared[claimUID]; !found {
		return
	}

	needToPublishSlice, unwrappedErr = s.unprepareDevices(ctx, claimUID)
	if unwrappedErr != nil {
		err = fmt.Errorf("failed to unprepare devices for claim %v: %v", claimUID, unwrappedErr)
		return
	}

	klog.V(5).Infof("Freeing devices from claim %v", claimUID)
	delete(s.Prepared, claimUID)

	// write prepared claims to file
	if unwrappedErr = WritePreparedClaimsToFile(s.PreparedClaimsFilePath, s.Prepared); unwrappedErr != nil {
		err = fmt.Errorf("failed to write prepared claims to file: %v", unwrappedErr)
	}

	return
}

// prepareVFIODevice is called when a GPU needs to be prepared to be used in a VM.
func (s *nodeState) prepareVFIODevice(allocatableDevice *device.DeviceInfo) (bool, error) {
	needToPublishSlice := false
	targetDriver, err := deduceTargetVFIODriver(allocatableDevice.Driver)
	if err != nil {
		return needToPublishSlice, fmt.Errorf("failed to deduce target VFIO driver for device %v: %v", allocatableDevice.PCIAddress, err)
	}

	if allocatableDevice.CurrentDriver == targetDriver {
		return needToPublishSlice, nil
	}

	needToPublishSlice, err = s.changeKernelDriver(allocatableDevice.PCIAddress, targetDriver)
	if err != nil {
		return needToPublishSlice, fmt.Errorf("failed to change driver for device %v: %v", allocatableDevice.PCIAddress, err)
	}
	time.Sleep(device.DriverChangeDelay)

	// update current driver in device info after successful driver change
	allocatableDevice.CurrentDriver = targetDriver
	needToPublishSlice = true

	vfioDevice, err := discovery.GetVFIODevice(allocatableDevice.PCIAddress)
	if err != nil {
		return needToPublishSlice, fmt.Errorf("failed to get VFIO device for PCI address %v: %v", allocatableDevice.PCIAddress, err)
	}
	allocatableDevice.VFIODevice = vfioDevice

	iommuGroup, err := discovery.GetIOMMUGroup(allocatableDevice.PCIAddress)
	if err != nil {
		return needToPublishSlice, fmt.Errorf("failed to get IOMMU group for device %v: %v", allocatableDevice.PCIAddress, err)
	}
	allocatableDevice.IOMMUGroup = iommuGroup

	// Save new CDI device
	if err := cdihelpers.UpdateGPUDevices(s.CdiCache, []*device.DeviceInfo{allocatableDevice}); err != nil {
		return needToPublishSlice, fmt.Errorf("failed to add device %v to CDI registry: %v", allocatableDevice.PCIAddress, err)
	}

	return needToPublishSlice, nil
}

func deduceTargetVFIODriver(defaultDriver string) (string, error) {
	if defaultDriver == device.SysfsXeDriverName { // try xe-vfio-pci
		if err := vfio.EnsureKernelModuleLoaded(device.SysfsXeVFIODriverName); err == nil {
			return device.SysfsXeVFIODriverName, nil
		}
		klog.Warningf("preferred kernel module %v is not available, falling back to %v", device.SysfsXeVFIODriverName, device.SysfsVFIODriverName)
	}

	if err := vfio.EnsureKernelModuleLoaded(device.SysfsVFIODriverName); err == nil {
		return device.SysfsVFIODriverName, nil
	}

	return "", fmt.Errorf("no suitable kernel module is available [%v, %v]", device.SysfsXeVFIODriverName, device.SysfsVFIODriverName)
}

// prepareDRMDevice is called when a GPU needs to be prepared to be used in a non-VM Pod.
func (s *nodeState) prepareDRMDevice(allocatableDevice *device.DeviceInfo) (needToPublishSlice bool, err error) {
	klog.V(5).Info("prepareDRMDevice")
	var unwrappedErr error

	if unwrappedErr := vfio.EnsureKernelModuleLoaded(allocatableDevice.Driver); unwrappedErr != nil {
		klog.Errorf("kernel module %v is not loaded", allocatableDevice.Driver)
		err = fmt.Errorf("kernel module %v is not loaded", allocatableDevice.Driver)
		return
	}

	if allocatableDevice.CurrentDriver == allocatableDevice.Driver {
		return needToPublishSlice, nil
	}

	needToPublishSlice, unwrappedErr = s.changeKernelDriver(allocatableDevice.PCIAddress, allocatableDevice.Driver)
	if unwrappedErr != nil {
		err = fmt.Errorf("failed to change driver for device %v: %v", allocatableDevice.PCIAddress, unwrappedErr)
		return
	}
	time.Sleep(device.DriverChangeDelay)

	// update current driver in device info after successful driver change
	allocatableDevice.CurrentDriver = allocatableDevice.Driver

	deviceSysfsDir := path.Join(s.SysfsRoot, device.SysfsPCIDevicesPath, allocatableDevice.PCIAddress)
	cardName, renderDName, unwrappedErr := drm.DeduceCardAndRenderDNames(deviceSysfsDir)
	if unwrappedErr != nil {
		err = fmt.Errorf("failed to get DRM device for PCI address %v: %v", allocatableDevice.PCIAddress, unwrappedErr)
		return
	}
	allocatableDevice.CardName = cardName
	allocatableDevice.RenderDName = renderDName

	// Save new CDI device
	if unwrappedErr := cdihelpers.UpdateGPUDevices(s.CdiCache, []*device.DeviceInfo{allocatableDevice}); unwrappedErr != nil {
		err = fmt.Errorf("failed to add device %v to CDI registry: %v", allocatableDevice.PCIAddress, unwrappedErr)
		return
	}

	return
}

func (s *nodeState) getAllocatableByPCIAddress(pciAddress string) (*device.DeviceInfo, error) {
	// nolint:forcetypeassert
	allocatable := s.Allocatable.(map[string]*device.DeviceInfo)

	for _, deviceInfo := range allocatable {
		if deviceInfo.PCIAddress == pciAddress {
			return deviceInfo, nil
		}
	}

	return nil, fmt.Errorf("no device found with PCI address %s", pciAddress)
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
		klog.V(6).Infof("Checking device %v info", deviceUID)
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

		klog.V(6).Infof("Updated health status for device: %v to: overall: %v; details: %v", deviceUID, foundDevice.Health, foundDevice.HealthStatus)
	}

	return needToPublish, nil
}

func (s *nodeState) changeKernelDriver(pciAddress, driverName string) (bool, error) {
	klog.V(5).Infof("Changing driver for device %v to %v", pciAddress, driverName)
	supportedDrivers := map[string]struct{}{
		device.SysfsI915DriverName:   {},
		device.SysfsXeDriverName:     {},
		device.SysfsVFIODriverName:   {},
		device.SysfsXeVFIODriverName: {},
	}
	if _, found := supportedDrivers[driverName]; !found {
		return false, fmt.Errorf("unsupported driver: %v", driverName)
	}

	if !s.ManageBinding {
		return false, fmt.Errorf("driver binding management is disabled, cannot change driver for device %v to %v", pciAddress, driverName)
	}

	if err := vfio.UnbindDeviceFromKernelDriver(pciAddress); err != nil {
		klog.Errorf("error unbinding device %v from current driver: %v", pciAddress, err)
		return false, fmt.Errorf("failed to unbind device %v from current driver: %v", pciAddress, err)
	}

	time.Sleep(device.DriverChangeDelay)

	if err := vfio.BindDeviceToDriver(pciAddress, driverName); err != nil {
		klog.Errorf("failed binding device %v to %v: %v", pciAddress, driverName, err)
		return true, fmt.Errorf("failed to bind device %v to driver %v: %v", pciAddress, driverName, err)
	}

	klog.V(5).Infof("Successfully changed driver for device %v to %v", pciAddress, driverName)
	return true, nil
}

func (s *nodeState) getRequestDeviceClassNameFromClaim(requestName string, claim *resourcev1.ResourceClaim) string {
	klog.V(5).Infof("Getting device class name for request %v in claim %v", requestName, claim.Name)
	for _, deviceRequest := range claim.Spec.Devices.Requests {
		klog.V(5).Infof("Checking device request %v: %+v", deviceRequest.Name, deviceRequest)
		requestNameParts := strings.Split(requestName, "/")
		if deviceRequest.Name == requestNameParts[0] {
			if deviceRequest.Exactly != nil {
				klog.V(5).Infof("Exact request %v: %+v", requestName, deviceRequest.Exactly)
				return deviceRequest.Exactly.DeviceClassName
			}

			if len(deviceRequest.FirstAvailable) > 0 && len(requestNameParts) == 2 {
				for _, subRequest := range deviceRequest.FirstAvailable {
					if subRequest.Name == requestNameParts[1] {
						klog.V(5).Infof("FirstAvailable request %v: %+v", requestName, subRequest)
						return subRequest.DeviceClassName
					}
				}
			}

			return ""
		}
	}

	return ""
}

// unprepareDevices checks if any taints need to be cleaned up from devices, and CDI devices removed from CDI cache.
func (s *nodeState) unprepareDevices(ctx context.Context, claimUID types.UID) (bool, error) {
	preparedClaim, found := s.Prepared[claimUID]
	needToPublishSlice := false
	if !found {
		return needToPublishSlice, nil
	}

	klog.V(5).Infof("Freeing devices from claim %v", claimUID)

	allocatableDevices, ok := s.Allocatable.(map[string]*device.DeviceInfo)
	if !ok {
		return needToPublishSlice, fmt.Errorf("failed to cast allocatable devices")
	}

	cdiDevicesToRemove := []string{}
	for _, preparedDevice := range preparedClaim.PreparedDevices {
		klog.V(5).Infof(
			"Unpreparing device %v (CDI ids: %v) for claim %v",
			preparedDevice.KubeletpluginDevice.DeviceName,
			preparedDevice.KubeletpluginDevice.CDIDeviceIDs,
			claimUID)
		allocatableDevice, found := allocatableDevices[preparedDevice.KubeletpluginDevice.DeviceName]
		if !found {
			klog.V(5).Infof("could not find allocatable device %v for claim %v", preparedDevice.KubeletpluginDevice.DeviceName, claimUID)
			return needToPublishSlice, fmt.Errorf("allocatable device %v not found", preparedDevice.KubeletpluginDevice.DeviceName)
		}

		// cleanup UnexpectedDevice taint that could have been places when the device was in use / in prepared claim, and the driver was changed.
		if allocatableDevice.HealthStatus[device.HealthStatusUnexpectedDriver] == device.HealthUnhealthy {
			klog.Infof("Cleaning up %v taint from device %v", device.HealthStatusUnexpectedDriver, allocatableDevice.PCIAddress)
			allocatableDevice.HealthStatus[device.HealthStatusUnexpectedDriver] = device.HealthHealthy
			needToPublishSlice = true
		}

		klog.V(5).Infof("Found allocatable device %v for CDI device %v", allocatableDevice.PCIAddress, preparedDevice.KubeletpluginDevice.DeviceName)
		switch {
		case allocatableDevice.IsVFIOBound():
			cdiDevicesToRemove = append(cdiDevicesToRemove, preparedDevice.KubeletpluginDevice.CDIDeviceIDs...)
		case allocatableDevice.IsDRMBound():
			cdiDevicesToRemove = append(cdiDevicesToRemove, preparedDevice.KubeletpluginDevice.CDIDeviceIDs...)
		default:
			klog.Warningf("Device %v is neither a VFIO device nor a DRM device during unpreparing.", allocatableDevice.PCIAddress)
		}
	}

	return needToPublishSlice, cdihelpers.RemoveDevices(s.CdiCache, cdiDevicesToRemove)
}
