/*
 * Copyright (c) 2022, Intel Corporation.  All Rights Reserved.
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
	"sync"

	cdiapi "github.com/container-orchestrated-devices/container-device-interface/pkg/cdi"
	specs "github.com/container-orchestrated-devices/container-device-interface/specs-go"
	intelcrd "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/crd/intel/v1alpha/api"
	"k8s.io/klog/v2"
)

type DeviceInfo struct {
	uuid       string // to be removed ?
	model      string // official model name
	cardidx    int    // card device number (e.g. 0 : /dev/dri/card0)
	renderdidx int    // renderd device number (e.g. 128 : /dev/dri/renderD128)
	memory     int    // in MiB
	cdiname    string // name field from cdi spec, uuid if handled by resource-driver
	deviceType string // gpu, tile, vgpu, vf
}

func (g *DeviceInfo) DeepCopy() *DeviceInfo {
	return &DeviceInfo{
		uuid:       g.uuid,
		model:      g.model,
		cardidx:    g.cardidx,
		renderdidx: g.renderdidx,
		memory:     g.memory,
		cdiname:    g.cdiname,
		deviceType: g.deviceType,
	}
}

type DevicesInfo map[string]*DeviceInfo

type ClaimAllocations map[string][]*DeviceInfo

type nodeState struct {
	sync.Mutex
	cdi         cdiapi.Registry
	allocatable map[string]*DeviceInfo
	allocations ClaimAllocations
}

func (g DeviceInfo) CDIDevice() string {
	return fmt.Sprintf("%s=%s", cdiKind, g.cdiname)
}

func newNodeState(gas *intelcrd.GpuAllocationState) (*nodeState, error) {
	klog.V(3).Infof("Enumerating all devices")
	detecteddevices, err := enumerateAllPossibleDevices()
	if err != nil {
		return nil, fmt.Errorf("error enumerating all possible devices: %v", err)
	}
	for ddev := range detecteddevices {
		klog.V(3).Infof("new device: %+v", ddev)
	}

	klog.V(5).Infof("Getting CDI registry")
	cdi := cdiapi.GetRegistry(
		cdiapi.WithSpecDirs(cdiRoot),
	)

	klog.V(5).Infof("Got CDI registry, refreshing it")
	err = cdi.Refresh()
	if err != nil {
		return nil, fmt.Errorf("unable to refresh the CDI registry: %v", err)
	}

	// syncDetectedDevicesWithCdiRegistry overrides uuid in detecteddevices from existing cdi spec
	err = syncDetectedDevicesWithCdiRegistry(cdi, detecteddevices)
	if err != nil {
		return nil, fmt.Errorf("unable to sync detected devices to CDI registry: %v", err)
	}
	err = cdi.Refresh()
	if err != nil {
		return nil, fmt.Errorf("unable to refresh the CDI registry after populating it: %v", err)
	}

	for duuid, ddev := range detecteddevices {
		klog.V(3).Infof("Allocatable after CDI refresh device: %v : %+v", duuid, ddev)
	}

	klog.V(5).Infof("Creating NodeState")
	// TODO: allocatable should include cdi-described
	state := &nodeState{
		cdi:         cdi,
		allocatable: detecteddevices,
		allocations: make(ClaimAllocations),
	}

	klog.V(5).Infof("Syncing allocatable devices")
	err = state.syncAllocatedDevicesFromGASSpec(&gas.Spec)
	if err != nil {
		return nil, fmt.Errorf("unable to sync allocated devices from CRD: %v", err)
	}
	klog.V(5).Infof("Synced state with CDI and CRD: %+v", state)
	for duuid, ddev := range state.allocatable {
		klog.V(5).Infof("Allocatable device: %v : %+v", duuid, ddev)
	}

	return state, nil
}

// write detected devices into cdi registry if they are not yet there
func syncDetectedDevicesWithCdiRegistry(registry cdiapi.Registry, devices DevicesInfo) error {
	for _, device := range registry.DeviceDB().ListDevices() {
		klog.V(5).Infof("CDI device: %+v", device)
	}

	for _, class := range registry.SpecDB().ListClasses() {
		klog.V(5).Infof("CDI class: %+v", class)
	}

	vendorfound := false
	// print all vendors and devices in registry
	for _, vendor := range registry.SpecDB().ListVendors() {
		klog.V(5).Infof("CDI vendor: %+v", vendor)
		if cdiVendor == vendor {
			vendorfound = true
		}
		for _, spec := range registry.SpecDB().GetVendorSpecs(vendor) {
			klog.V(5).Infof("CDI spec: %+v", spec)
			for duuid, device := range spec.Devices {
				klog.V(5).Infof("CDI spec device: %v : %+v", duuid, device)
			}
		}
	}

	devicesToAdd := DevicesInfo{}
	if vendorfound {
		vendorspecs := registry.SpecDB().GetVendorSpecs(cdiVendor)
		// match allocatable to see if it is in the registry already
		for _, detecteddevice := range devices {
			if cardIsInRegistry(vendorspecs, detecteddevice) {
				klog.V(5).Infof("Found card %v in registry, updating uuid", detecteddevice.cardidx)
			} else { // add to the registry
				klog.V(5).Infof("Need to add new device /dev/dri/card%v | /dev/dri/renderD%v",
					detecteddevice.cardidx, detecteddevice.renderdidx)
				devicesToAdd[detecteddevice.uuid] = detecteddevice
			}
		}
	} else {
		klog.V(5).Infof("No existing specs found for vendor %v", cdiVendor)
		devicesToAdd = devices
	}

	if len(devicesToAdd) > 0 {
		klog.V(5).Info("Creating CDI files for detected video hardware")
		if err := addNewDevicesToRegistry(devicesToAdd); err != nil {
			klog.V(5).Infof("Failed adding card to cdi registry: %v")
			return err
		}
	}

	return nil
}

func addNewDevicesToRegistry(devices DevicesInfo) error {
	klog.V(5).Infof("Adding %v devices to new spec", len(devices))
	registry := cdiapi.GetRegistry()
	spec := &specs.Spec{
		Version: cdiVersion,
		Kind:    cdiKind,
	}

	for _, device := range devices {
		spec.Devices = append(spec.Devices, specs.Device{
			Name: device.cdiname,
			ContainerEdits: specs.ContainerEdits{
				DeviceNodes: []*specs.DeviceNode{
					{Path: fmt.Sprintf("%scard%d", dridevpath, device.cardidx), Type: "c"},
					{Path: fmt.Sprintf("%srenderD%d", dridevpath, device.renderdidx), Type: "c"}},
			},
		})
	}
	klog.V(5).Infof("spec devices length: %v", len(spec.Devices))

	specname, err := cdiapi.GenerateNameForSpec(spec)
	if err != nil {
		return fmt.Errorf("Failed to generate name for cdi device spec: %+v", err)
	}

	return registry.SpecDB().WriteSpec(spec, specname)
}

// sync cdiname from CDI for further UUID sync
func cardIsInRegistry(vendorspecs []*cdiapi.Spec, detecteddevice *DeviceInfo) bool {
	for specidx, vendorspec := range vendorspecs {
		klog.V(5).Infof("checking vendorspec %v", specidx)
		for devidx, device := range vendorspec.Devices {
			klog.V(5).Infof("checking device %v: %v", devidx, device)
			for devnodeidx, devicenode := range device.ContainerEdits.DeviceNodes {
				testpath := fmt.Sprintf("%scard%d", dridevpath, detecteddevice.cardidx)
				klog.V(5).Infof("checking device node %v: %v / %v", devnodeidx, testpath, devicenode.Path)
				if testpath == devicenode.Path {
					klog.V(5).Infof("Found discovered device %v in registry, new name %v", detecteddevice, device.Name)
					detecteddevice.cdiname = device.Name
					return true
				}
			}
		}
	}
	return false
}

func (s *nodeState) free(claimUid string) error {
	s.Lock()
	defer s.Unlock()

	if s.allocations[claimUid] == nil {
		return nil
	}

	for _, device := range s.allocations[claimUid] {
		var err error
		switch device.deviceType {
		case intelcrd.GpuDeviceType:
			err = s.freeGpu(device)
		// TODO: SR-IOV
		default:
			klog.Errorf("Unsupported device type: %v", device.deviceType)
			err = fmt.Errorf("unsupported device type: %v", device.deviceType)
		}
		if err != nil {
			return fmt.Errorf("free failed: %v", err)
		}
	}

	delete(s.allocations, claimUid)
	return nil
}

func (s *nodeState) getUpdatedSpec(inspec *intelcrd.GpuAllocationStateSpec) *intelcrd.GpuAllocationStateSpec {
	s.Lock()
	defer s.Unlock()

	outspec := inspec.DeepCopy()
	s.syncAllocatableDevicesToGASSpec(outspec)
	s.syncAllocatedDevicesToGASSpec(outspec)
	return outspec
}

func (s *nodeState) getAllocatedAsCDIDevices(claimUid string) []string {
	var devs []string
	klog.V(5).Infof("getAllocatedAsCDIDevices is called")
	for _, device := range s.allocations[claimUid] {
		cdidev := s.cdi.DeviceDB().GetDevice(device.CDIDevice())
		if cdidev == nil {
			klog.Errorf("Device %v from claim %v not found in cdi DB", device.uuid, claimUid)
			return []string{}
		}
		klog.V(5).Infof("Found cdi device %v", cdidev.GetQualifiedName())
		devs = append(devs, cdidev.GetQualifiedName())
	}
	return devs
}

func (s *nodeState) freeGpu(gpu *DeviceInfo) error {
	klog.V(3).Infof("freeGPU IS NOT IMPLEMENTED")
	return nil
}

func (s *nodeState) syncAllocatableDevicesToGASSpec(spec *intelcrd.GpuAllocationStateSpec) {
	gpus := make(map[string]intelcrd.AllocatableGpu)
	for _, device := range s.allocatable {
		gpus[device.uuid] = intelcrd.AllocatableGpu{
			CDIDevice: device.cdiname,
			Memory:    device.memory,
			Model:     device.model,
			Type:      device.deviceType,
			UUID:      device.uuid,
		}
	}
	// TODO: SRIOV add to allocatable

	spec.AllocatableGpus = gpus
}

func (s *nodeState) syncAllocatedDevicesFromGASSpec(spec *intelcrd.GpuAllocationStateSpec) error {
	klog.V(5).Infof("Syncing %v allocatable gpus and %v resource claim allocations", len(s.allocatable), len(spec.ResourceClaimAllocations))
	klog.V(5).Infof("Allocatable: %+v", s.allocatable)
	if s.allocations == nil {
		s.allocations = make(ClaimAllocations)
	}

	for claimuuid, devices := range spec.ResourceClaimAllocations {
		klog.V(5).Infof("claim %v has %v gpus", claimuuid, len(devices))
		s.allocations[claimuuid] = []*DeviceInfo{}
		for _, d := range devices {
			klog.V(5).Infof("Device: %+v, .Gpu %+v", d, d)
			switch d.Type {
			case intelcrd.GpuDeviceType:
				klog.V(5).Info("Matched GPU type in sync")
				if _, exists := s.allocatable[d.UUID]; !exists {
					klog.Errorf("Allocated device %v no longer available for claim %v", d.UUID, claimuuid)
					// TODO: handle this better: wipe resource claim allocation if claimAllocation does not exist anymore
					return fmt.Errorf("Could not find allocated device %v for claimAllocation %v", d.UUID, claimuuid)
				}
				newdevice := s.allocatable[d.UUID].DeepCopy()
				newdevice.memory = d.Memory
				// TODO: set the rest of allocation-specific parameters
				s.allocations[claimuuid] = append(s.allocations[claimuuid], newdevice)
			// TODO: SR-IOV
			default:
				klog.Errorf("Unsupported device type: %v", d.Type)
			}
		}
	}

	return nil
}

func (s *nodeState) syncAllocatedDevicesToGASSpec(gasspec *intelcrd.GpuAllocationStateSpec) {
	outrcas := make(map[string]intelcrd.AllocatedDevices)
	for claimuuid, devices := range s.allocations {
		allocatedDevices := intelcrd.AllocatedDevices{}
		for _, device := range devices {
			switch device.deviceType {
			case intelcrd.GpuDeviceType:
				outdevice := intelcrd.AllocatedGpu{
					UUID:      device.uuid,
					CDIDevice: device.CDIDevice(),
					Memory:    device.memory,
					Type:      device.deviceType,
				}
				allocatedDevices = append(allocatedDevices, outdevice)
			// TODO SR-IOV
			default:
				klog.Errorf("Unsupported device type: %v", device.deviceType)
			}

		}
		outrcas[claimuuid] = allocatedDevices
	}
	gasspec.ResourceClaimAllocations = outrcas
}
