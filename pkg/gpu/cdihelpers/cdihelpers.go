//
// Copyright (C) 2024-2026 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0
//

package cdihelpers

import (
	"fmt"
	"os"
	"path"
	"path/filepath"

	"k8s.io/klog/v2"
	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"
	specs "tags.cncf.io/container-device-interface/specs-go"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/helpers"
)

const (
	containerDevdriPath  = "/dev/dri"
	containerDevPath     = "/dev"
	containerDevVFIOPath = "/dev/vfio"
)

func getGPUSpecs(cdiCache *cdiapi.Cache) []*cdiapi.Spec {
	gpuSpecs := []*cdiapi.Spec{}
	for _, cdiSpec := range cdiCache.GetVendorSpecs(device.CDIVendor) {
		if cdiSpec.Kind == device.CDIKind {
			gpuSpecs = append(gpuSpecs, cdiSpec)
		}
	}
	return gpuSpecs
}

func getMEISpecs(cdiCache *cdiapi.Cache) []*cdiapi.Spec {
	meiSpecs := []*cdiapi.Spec{}
	for _, cdiSpec := range cdiCache.GetVendorSpecs(device.CDIVendor) {
		if cdiSpec.Kind == device.CDIMEIKind {
			meiSpecs = append(meiSpecs, cdiSpec)
		}
	}
	return meiSpecs
}

func replaceGPUCDISpecs(cdiCache *cdiapi.Cache, devices device.DevicesInfo) error {
	for _, spec := range getGPUSpecs(cdiCache) {
		if err := cdiCache.RemoveSpec(filepath.Base(spec.GetPath())); err != nil {
			return fmt.Errorf("failed to remove old GPU CDI spec %v: %v", spec, err)
		}
	}

	klog.V(5).Infof("Adding %v GPU devices to new spec", len(devices))
	gpuSpec := &specs.Spec{Kind: device.CDIKind}
	addGPUDevicesToGPUSpec(devices, gpuSpec)

	if err := writeSpecSpec(cdiCache, gpuSpec, ""); err != nil {
		return fmt.Errorf("failed adding devices to new GPU CDI spec: %v", err)
	}

	return nil
}

func replaceMEICDISpecs(cdiCache *cdiapi.Cache, devices device.DevicesInfo) error {
	for _, spec := range getMEISpecs(cdiCache) {
		if err := cdiCache.RemoveSpec(filepath.Base(spec.GetPath())); err != nil {
			return fmt.Errorf("failed to remove old MEI CDI spec %v: %v", spec, err)
		}
	}

	klog.V(5).Infof("Adding %v MEI devices to new spec", len(devices))
	meiSpec := &specs.Spec{Kind: device.CDIMEIKind}
	addMEIDevicesToMEISpec(devices, meiSpec)

	if err := writeSpecSpec(cdiCache, meiSpec, ""); err != nil {
		return fmt.Errorf("failed adding devices to new MEI CDI spec: %v", err)
	}

	return nil
}

// AddDetectedDevicesToCDIRegistry adds detected devices into cdi registry after deleting old specs.
func AddDetectedDevicesToCDIRegistry(cdiCache *cdiapi.Cache, detectedDevices device.DevicesInfo) error {
	if err := replaceGPUCDISpecs(cdiCache, detectedDevices); err != nil {
		return err
	}

	return replaceMEICDISpecs(cdiCache, detectedDevices)
}

// writeSpecSpec writes a CDI spec.Spec, generates a new name, if no name is given.
func writeSpecSpec(cdiCache *cdiapi.Cache, spec *specs.Spec, name string) error {
	specname := name
	var err error

	if specname == "" {
		specname, err = cdiapi.GenerateNameForSpec(spec)
		if err != nil {
			return fmt.Errorf("failed to generate name for cdi device spec: %+v", err)
		}
		klog.V(5).Infof("new name for new CDI spec: %v", specname)
	}

	// A CDI spec without devices is invalid and cannot be written,
	// remove the file if a name was given.
	if len(spec.Devices) == 0 {
		if name != "" {
			if err := cdiCache.RemoveSpec(specname); err != nil {
				return fmt.Errorf("failed to remove empty CDI spec %v: %v", specname, err)
			}
			klog.Infof("Removed empty CDI Spec %v", specname)
		}

		return nil
	}

	cdiVersion, err := cdiapi.MinimumRequiredVersion(spec)
	if err != nil {
		return fmt.Errorf("failed to get minimum required CDI spec version: %v", err)
	}
	spec.Version = cdiVersion

	err = cdiCache.WriteSpec(spec, specname)
	if err != nil {
		return fmt.Errorf("failed to write CDI spec %v: %v", specname, err)
	}

	return nil
}

func addMEIDevicesToMEISpec(devices device.DevicesInfo, spec *specs.Spec) {
	for _, newDevice := range devices {
		if newDevice.MEIName == "" {
			continue
		}

		spec.Devices = append(spec.Devices, specs.Device{
			Name: newDevice.UID,
			ContainerEdits: specs.ContainerEdits{
				DeviceNodes: []*specs.DeviceNode{
					{
						Path:     path.Join(containerDevPath, newDevice.MEIName),
						HostPath: path.Join(helpers.GetDevfsRoot(""), newDevice.MEIName),
						Type:     "c",
					},
				},
			},
		})
	}
}

func addGPUDevicesToGPUSpec(devices device.DevicesInfo, spec *specs.Spec) {
	for _, newDevice := range devices {
		// A device in survivability mode has no DRM devices, only the MEI device that is used for
		// firmware reflashing, and MEI devices have their own CDI spec.
		if newDevice.Survivability {
			klog.V(5).Infof("Device %v is in survivability mode, skipping CDI device creation", newDevice.UID)
			continue
		}

		newCDIDevice := specs.Device{
			Name: newDevice.UID,
		}
		addDeviceContainerEdits(newDevice, &newCDIDevice)

		// CDI spec validation rejects devices without container edits, and such a device could not
		// be used for anything anyway.
		if len(newCDIDevice.ContainerEdits.DeviceNodes) == 0 {
			klog.V(5).Infof("Device %v has no device nodes, skipping CDI device creation", newDevice.UID)
			continue
		}

		spec.Devices = append(spec.Devices, newCDIDevice)
	}
}

func addDeviceContainerEdits(newdevice *device.DeviceInfo, cdiDevice *specs.Device) {
	if newdevice.IsVFIOBound() {
		klog.V(5).Infof("Adding VFIO edits for device %v", newdevice.UID)
		addVFIOEdits(newdevice, cdiDevice)
	}
	if newdevice.IsDRMBound() {
		klog.V(5).Infof("Adding DRM edits for device %v", newdevice.UID)
		addDRMEdits(newdevice, cdiDevice)
	}
}

func addVFIOEdits(newDevice *device.DeviceInfo, cdiDevice *specs.Device) {
	devVFIOPath := path.Join(helpers.GetDevfsRoot(device.DevfsVFIOPath), device.DevfsVFIOPath)

	cdiDevice.ContainerEdits = specs.ContainerEdits{
		DeviceNodes: []*specs.DeviceNode{
			{
				Path:     path.Join(containerDevVFIOPath, newDevice.IOMMUGroup),
				HostPath: path.Join(devVFIOPath, newDevice.IOMMUGroup),
				Type:     "c",
			},
			{
				Path:     path.Join(containerDevVFIOPath, "vfio"),
				HostPath: path.Join(devVFIOPath, "vfio"),
				Type:     "c",
			},
			{
				Path:     path.Join(containerDevVFIOPath, "devices", newDevice.VFIODevice),
				HostPath: path.Join(devVFIOPath, "devices", newDevice.VFIODevice),
				Type:     "c",
			},
		},
	}
}

func addDRMEdits(newDevice *device.DeviceInfo, cdiDevice *specs.Device) {
	devdriPath := device.GetDriDevPath()

	cdiDevice.ContainerEdits = specs.ContainerEdits{
		DeviceNodes: []*specs.DeviceNode{
			{
				Path:     path.Join(containerDevdriPath, newDevice.CardName),
				HostPath: path.Join(devdriPath, newDevice.CardName),
				Type:     "c",
			},
		},
	}

	// render nodes can be optional: https://www.kernel.org/doc/html/latest/gpu/drm-uapi.html#render-nodes
	if newDevice.RenderDName != "" {
		cdiDevice.ContainerEdits.DeviceNodes = append(
			cdiDevice.ContainerEdits.DeviceNodes,
			&specs.DeviceNode{
				Path:     path.Join(containerDevdriPath, newDevice.RenderDName),
				HostPath: path.Join(devdriPath, newDevice.RenderDName),
				Type:     "c",
			},
		)
	}

	addBypathMounts(newDevice, cdiDevice, devdriPath)
}

// Add GPU specific by-path mounts to the spec.
func addBypathMounts(info *device.DeviceInfo, spec *specs.Device, dridevPath string) {
	containerBypathPath := filepath.Join(containerDevdriPath, "by-path")
	bypathPath := filepath.Join(dridevPath, "by-path")

	basename := filepath.Join(bypathPath, fmt.Sprintf("pci-%s-", info.PCIAddress))
	containerBasename := filepath.Join(containerBypathPath, fmt.Sprintf("pci-%s-", info.PCIAddress))

	gpuFiles := map[string]string{
		basename + "card":   containerBasename + "card",
		basename + "render": containerBasename + "render",
	}

	for gpuFile, containerFile := range gpuFiles {
		if _, err := os.Stat(gpuFile); err == nil {
			spec.ContainerEdits.Mounts = append(spec.ContainerEdits.Mounts, &specs.Mount{
				HostPath:      gpuFile,
				ContainerPath: containerFile,
				Type:          "none",
				Options:       []string{"bind", "rw"},
			})
		}
	}
}

// UpdateGPUDevices removes existing entries from CDI registry and adds new entries based
// on up supplied DevicesInfo. It is called from udev monitoring when GPU is bound to the
// driver, not during startup discovery.
func UpdateGPUDevices(cdiCache *cdiapi.Cache, devicesToUpdate []*device.DeviceInfo) error {
	devicesToRemove := []string{}
	for _, deviceToUpdate := range devicesToUpdate {
		devicesToRemove = append(devicesToRemove, deviceToUpdate.UID)
	}
	if err := RemoveDevices(cdiCache, devicesToRemove); err != nil {
		return fmt.Errorf("failed to remove old GPU devices from CDI spec: %v", err)
	}
	for _, deviceToAdd := range devicesToUpdate {
		if deviceToAdd.CurrentDriver == "" {
			klog.V(5).Infof("Device %v is not bound to any no driver, skipping CDI creation", deviceToAdd.UID)
			continue
		}
		if err := addGPUDevice(cdiCache, deviceToAdd); err != nil {
			return fmt.Errorf("failed to add updated GPU device to CDI spec: %v", err)
		}
		if err := addMEIDevice(cdiCache, deviceToAdd); err != nil {
			return fmt.Errorf("failed to add updated MEI device to CDI spec: %v", err)
		}
	}

	return nil
}

// addGPUDevice adds a new GPU device entry into cdi registry.
func addGPUDevice(cdiCache *cdiapi.Cache, newDevice *device.DeviceInfo) error {
	gpuSpecs := getGPUSpecs(cdiCache)
	var gpuSpec *specs.Spec
	var name string
	if len(gpuSpecs) == 0 {
		gpuSpec = &specs.Spec{Kind: device.CDIGPUKind}
	} else {
		gpuSpec = gpuSpecs[0].Spec
		name = filepath.Base(gpuSpecs[0].GetPath())
	}

	addGPUDevicesToGPUSpec(device.DevicesInfo{newDevice.UID: newDevice}, gpuSpec)

	if err := writeSpecSpec(cdiCache, gpuSpec, name); err != nil {
		return fmt.Errorf("failed adding devices to new GPU CDI spec: %v", err)
	}

	return nil
}

// addMEIDevice adds a new MEI device entry into cdi registry.
func addMEIDevice(cdiCache *cdiapi.Cache, newDevice *device.DeviceInfo) error {
	meiSpecs := getMEISpecs(cdiCache)
	var meiSpec *specs.Spec
	var name string
	if len(meiSpecs) == 0 {
		meiSpec = &specs.Spec{Kind: device.CDIMEIKind}
	} else {
		meiSpec = meiSpecs[0].Spec
		name = filepath.Base(meiSpecs[0].GetPath())
	}

	addMEIDevicesToMEISpec(device.DevicesInfo{newDevice.UID: newDevice}, meiSpec)

	if err := writeSpecSpec(cdiCache, meiSpec, name); err != nil {
		return fmt.Errorf("failed adding devices to new MEI CDI spec: %v", err)
	}

	return nil
}

// RemoveDevices removes the CDI devices from both - GPU and MEI specs.
func RemoveDevices(cdiCache *cdiapi.Cache, deviceUIDs []string) error {
	supportedSpecs := getGPUSpecs(cdiCache)
	supportedSpecs = append(supportedSpecs, getMEISpecs(cdiCache)...)

	for _, oldDevice := range deviceUIDs {
		for _, spec := range supportedSpecs {
			remainingDevices := []specs.Device{}
			for _, cdiDevice := range spec.Devices {
				if cdiDevice.Name == oldDevice {
					klog.V(5).Infof("Removing device %v from CDI spec %v", oldDevice, spec.GetPath())
					continue
				}
				remainingDevices = append(remainingDevices, cdiDevice)
			}

			if len(remainingDevices) == len(spec.Devices) {
				// No devices were removed, skip writing the spec.
				continue
			}

			spec.Devices = remainingDevices
			if err := writeSpecSpec(cdiCache, spec.Spec, filepath.Base(spec.GetPath())); err != nil {
				return fmt.Errorf("failed to write CDI spec %v: %v", spec.GetPath(), err)
			}
		}
	}

	return nil
}
