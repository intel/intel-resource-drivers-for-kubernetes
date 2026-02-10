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

	"k8s.io/klog/v2"
	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"
	specs "tags.cncf.io/container-device-interface/specs-go"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
)

const (
	containerDevdriPath = "/dev/dri"
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

// AddDetectedDevicesToCDIRegistry adds detected devices into cdi registry after deleting old specs.
func AddDetectedDevicesToCDIRegistry(cdiCache *cdiapi.Cache, detectedDevices device.DevicesInfo) error {
	gpuSpecs := getGPUSpecs(cdiCache)
	for _, spec := range gpuSpecs {
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
	if len(devices) == 0 {
		return nil
	}

	klog.V(5).Infof("Adding %v devices to new spec", len(devices))

	spec := &specs.Spec{
		Kind: device.CDIKind,
	}

	AddDevicesToSpec(devices, spec)
	klog.V(5).Infof("spec devices length: %v", len(spec.Devices))

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

func AddDevicesToSpec(devices device.DevicesInfo, spec *specs.Spec) {
	devdriPath := device.GetDriDevPath()

	for name, device := range devices {
		// primary / control node (for modesetting)
		newDevice := specs.Device{
			Name: name,
			ContainerEdits: specs.ContainerEdits{
				DeviceNodes: []*specs.DeviceNode{
					{
						Path:     path.Join(containerDevdriPath, fmt.Sprintf("card%d", device.CardIdx)),
						HostPath: path.Join(devdriPath, fmt.Sprintf("card%d", device.CardIdx)),
						Type:     "c",
					},
				},
			},
		}
		// render nodes can be optional: https://www.kernel.org/doc/html/latest/gpu/drm-uapi.html#render-nodes
		if device.RenderdIdx != 0 {
			newDevice.ContainerEdits.DeviceNodes = append(
				newDevice.ContainerEdits.DeviceNodes,
				&specs.DeviceNode{
					Path:     path.Join(containerDevdriPath, fmt.Sprintf("renderD%d", device.RenderdIdx)),
					HostPath: path.Join(devdriPath, fmt.Sprintf("renderD%d", device.RenderdIdx)),
					Type:     "c",
				},
			)
		}

		addBypathMounts(device, &newDevice, devdriPath)

		spec.Devices = append(spec.Devices, newDevice)
	}
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
