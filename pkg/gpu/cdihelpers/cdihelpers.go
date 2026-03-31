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
	containerDevdriPath = "/dev/dri"
	containerDevPath    = "/dev"
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
	AddDevicesToSpec(devices, gpuSpec)

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
	AddMeiDevicesToSpec(devices, meiSpec)

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

func AddMeiDevicesToSpec(devices device.DevicesInfo, spec *specs.Spec) {
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
						HostPath: path.Join(helpers.GetDevfsRoot(helpers.DevfsEnvVarName, ""), gpuDevice.MEIName),
						Type:     "c",
					},
				},
			},
		})
	}
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
