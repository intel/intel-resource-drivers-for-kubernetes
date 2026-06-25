//
// Copyright (C) 2024-2026 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0
//

package cdihelpers

import (
	"fmt"
	"path"

	"k8s.io/klog/v2"
	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"
	cdiSpecs "tags.cncf.io/container-device-interface/specs-go"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/npu/device"
)

const (
	containerDevfsRoot = "/dev"
)

func getNPUSpecs(cdiCache *cdiapi.Cache) []*cdiapi.Spec {
	npuSpecs := []*cdiapi.Spec{}
	for _, cdiSpec := range cdiCache.GetVendorSpecs(device.CDIVendor) {
		if cdiSpec.Kind == device.CDIKind {
			npuSpecs = append(npuSpecs, cdiSpec)
		}
	}
	return npuSpecs
}

// AddDetectedDevicesToCDIRegistry adds detected devices into cdi registry after deleting old specs.
func AddDetectedDevicesToCDIRegistry(cdiCache *cdiapi.Cache, detectedDevices device.DevicesInfo) error {
	npuSpecs := getNPUSpecs(cdiCache)
	for _, spec := range npuSpecs {
		if err := cdiCache.RemoveSpec(spec.GetPath()); err != nil {
			return fmt.Errorf("failed to remove old CDI spec %v: %v", spec, err)
		}
	}

	if err := addDevicesToNewSpec(cdiCache, detectedDevices); err != nil {
		return fmt.Errorf("failed adding devices to new CDI spec: %v", err)
	}

	return nil
}

// addDevicesToNewSpec creates new CDI spec and adds devices to it.
func addDevicesToNewSpec(cdiCache *cdiapi.Cache, devices device.DevicesInfo) error {
	klog.V(5).Infof("Adding %v devices to new spec", len(devices))

	spec := &cdiSpecs.Spec{
		Kind: device.CDIKind,
	}

	specName, err := cdiapi.GenerateNameForSpec(spec)
	if err != nil {
		return fmt.Errorf("failed to generate name for cdi device spec: %+v", err)
	}
	klog.V(5).Infof("New name for new CDI spec: %v", specName)

	return addDevicesToSpecAndWrite(cdiCache, devices, spec, specName)
}

func addDevicesToSpecAndWrite(cdiCache *cdiapi.Cache, devices device.DevicesInfo, spec *cdiSpecs.Spec, specName string) error {
	for name, device := range devices {
		// primary / control node (for modesetting)
		newDevice := cdiSpecs.Device{
			Name: name,
			ContainerEdits: cdiSpecs.ContainerEdits{
				DeviceNodes: newContainerEditsDeviceNodes(device.DeviceIdx),
			},
		}
		spec.Devices = append(spec.Devices, newDevice)
	}

	if err := writeSpec(cdiCache, spec, specName); err != nil {
		return fmt.Errorf("failed to save new CDI spec %v: %v", specName, err)
	}

	return nil
}

func newContainerEditsDeviceNodes(deviceIdx uint64) []*cdiSpecs.DeviceNode {
	accelDevPath := device.GetAccelDevfsPath()
	deviceNodes := []*cdiSpecs.DeviceNode{
		{
			Path:     path.Join(containerDevfsRoot, device.DevfsAccelPath, fmt.Sprintf("accel%d", deviceIdx)),
			HostPath: path.Join(accelDevPath, fmt.Sprintf("accel%d", deviceIdx)),
			Type:     "c"},
		{
			Path:     path.Join(containerDevfsRoot, device.DevfsAccelPath, fmt.Sprintf("accel_controlD%d", deviceIdx)),
			HostPath: path.Join(accelDevPath, fmt.Sprintf("accel_controlD%d", deviceIdx)),
			Type:     "c",
		},
	}

	return deviceNodes
}

// writeSpec sets latest cdiVersion for spec and writes it.
func writeSpec(cdiCache *cdiapi.Cache, spec *cdiSpecs.Spec, specName string) error {
	cdiVersion, err := cdiapi.MinimumRequiredVersion(spec)
	if err != nil {
		return fmt.Errorf("failed to get minimum required CDI spec version: %v", err)
	}
	spec.Version = cdiVersion

	if len(spec.Devices) == 0 {
		klog.V(5).Infof("No devices in spec %v, deleting it", specName)
		if err := cdiCache.RemoveSpec(specName); err != nil {
			return fmt.Errorf("failed to remove empty CDI spec %v: %v", specName, err)
		}
		return nil
	}

	klog.V(5).Infof("Writing spec %v", specName)
	err = cdiCache.WriteSpec(spec, specName)
	if err != nil {
		return fmt.Errorf("failed to write CDI spec %v: %v", specName, err)
	}

	return nil
}
