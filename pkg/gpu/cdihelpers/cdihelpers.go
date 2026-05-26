/*
 * Copyright (c) 2024, Intel Corporation.  All Rights Reserved.
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

package cdihelpers

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

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
		// RemoveSpec expects spec name (without extension), not full file path.
		// Example: /var/run/cdi/intel.com_gpu.yaml -> intel.com_gpu
		specName := strings.TrimSuffix(filepath.Base(spec.GetPath()), filepath.Ext(spec.GetPath()))
		if err := cdiCache.RemoveSpec(specName); err != nil {
			return fmt.Errorf("failed to remove old GPU CDI spec %v: %v", spec, err)
		}
	}

	klog.V(5).Infof("Adding %v GPU devices to new spec", len(devices))
	gpuSpec := &specs.Spec{Kind: device.CDIKind}
	addDevicesToSpec(devices, gpuSpec)

	if err := writeSpec(cdiCache, gpuSpec); err != nil {
		return fmt.Errorf("failed adding devices to new GPU CDI spec: %v", err)
	}

	return nil
}

func replaceMEICDISpecs(cdiCache *cdiapi.Cache, devices device.DevicesInfo) error {
	for _, spec := range getMEISpecs(cdiCache) {
		// RemoveSpec expects spec name (without extension), not full file path.
		// Example: /var/run/cdi/intel.com_gpu-mei.yaml -> intel.com_gpu-mei.yaml -> intel.com_gpu-mei
		specName := strings.TrimSuffix(filepath.Base(spec.GetPath()), filepath.Ext(spec.GetPath()))
		if err := cdiCache.RemoveSpec(specName); err != nil {
			return fmt.Errorf("failed to remove old MEI CDI spec %v: %v", spec, err)
		}
	}

	klog.V(5).Infof("Adding %v MEI devices to new spec", len(devices))
	meiSpec := &specs.Spec{Kind: device.CDIMEIKind}
	addMeiDevicesToSpec(devices, meiSpec)

	if err := writeSpec(cdiCache, meiSpec); err != nil {
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

// writeSpec writes a prepared CDI spec into cache.
func writeSpec(cdiCache *cdiapi.Cache, spec *specs.Spec) error {
	klog.V(5).Infof("spec devices length: %v", len(spec.Devices))
	if len(spec.Devices) == 0 {
		return nil
	}

	cdiVersion, err := cdiapi.MinimumRequiredVersion(spec)
	if err != nil {
		return fmt.Errorf("failed to get minimum required CDI spec version: %v", err)
	}
	klog.V(5).Infof("CDI version required for new spec: %v", cdiVersion)
	spec.Version = cdiVersion

	specname, err := cdiapi.GenerateNameForSpec(spec)
	if err != nil {
		return fmt.Errorf("failed to generate name for cdi device spec: %+v", err)
	}
	klog.V(5).Infof("new name for new CDI spec: %v", specname)

	err = cdiCache.WriteSpec(spec, specname)
	if err != nil {
		return fmt.Errorf("failed to write CDI spec %v: %v", specname, err)
	}

	return nil
}

func addMeiDevicesToSpec(devices device.DevicesInfo, spec *specs.Spec) {
	seenMEI := make(map[string]bool)

	for _, gpuDevice := range devices {
		if gpuDevice.MEIName == "" || seenMEI[gpuDevice.MEIName] {
			continue
		}
		seenMEI[gpuDevice.MEIName] = true

		spec.Devices = append(spec.Devices, specs.Device{
			Name: gpuDevice.MEIName,
			ContainerEdits: specs.ContainerEdits{
				DeviceNodes: []*specs.DeviceNode{
					{
						Path:     path.Join(containerDevPath, gpuDevice.MEIName),
						HostPath: path.Join(helpers.GetDevfsRoot(""), gpuDevice.MEIName),
						Type:     "c",
					},
				},
			},
		})
	}
}

func addDevicesToSpec(devices device.DevicesInfo, spec *specs.Spec) {
	for _, newDevice := range devices {
		newCDIDevice := specs.Device{
			Name: newDevice.UID,
		}
		addDeviceContainerEdits(newDevice, &newCDIDevice)
		spec.Devices = append(spec.Devices, newCDIDevice)
	}
}

func addDeviceContainerEdits(newdevice *device.DeviceInfo, cdiDevice *specs.Device) {
	if newdevice.CurrentDriver == device.SysfsVFIODriverName || newdevice.CurrentDriver == device.SysfsXeVFIODriverName {
		klog.V(5).Infof("Adding VFIO edits for device %v", newdevice.UID)
		addVFIOEdits(newdevice, cdiDevice)
	} else {
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
// on up supplied DevicesInfo.
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
		if err := AddGPUDevice(cdiCache, deviceToAdd); err != nil {
			return fmt.Errorf("failed to add updated GPU device to CDI spec: %v", err)
		}
	}

	return nil
}

// AddGPUDevice adds a new GPU device entry into cdi registry.
func AddGPUDevice(cdiCache *cdiapi.Cache, newDevice *device.DeviceInfo) error {
	gpuSpecs := getGPUSpecs(cdiCache)
	var gpuSpec *specs.Spec
	if len(gpuSpecs) == 0 {
		gpuSpec = &specs.Spec{Kind: device.CDIGPUKind}
	} else {
		gpuSpec = gpuSpecs[0].Spec
	}
	addDevicesToSpec(device.DevicesInfo{newDevice.UID: newDevice}, gpuSpec)

	if err := writeSpec(cdiCache, gpuSpec); err != nil {
		return fmt.Errorf("failed adding devices to new GPU CDI spec: %v", err)
	}

	return nil
}

func RemoveDevices(cdiCache *cdiapi.Cache, devicesToRemove []string) error {
	gpuSpecs := getGPUSpecs(cdiCache)
	if len(gpuSpecs) == 0 {
		return nil
	}

	for _, oldDevice := range devicesToRemove {
		for _, spec := range gpuSpecs {
			remainingDevices := []specs.Device{}
			for _, cdiDevice := range spec.Devices {
				if cdiDevice.Name == oldDevice {
					klog.V(5).Infof("Removing device %v from CDI spec %v", oldDevice, spec.GetPath())
					continue
				}
				remainingDevices = append(remainingDevices, cdiDevice)
			}
			spec.Devices = remainingDevices

			specname := path.Base(spec.GetPath())
			if err := cdiCache.WriteSpec(spec.Spec, specname); err != nil {
				return fmt.Errorf("failed to write CDI spec %v: %v", specname, err)
			}
		}
	}

	return nil
}
