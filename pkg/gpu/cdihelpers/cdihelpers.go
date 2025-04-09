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
	"regexp"
	"strconv"
	"strings"

	"k8s.io/klog/v2"
	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"
	specs "tags.cncf.io/container-device-interface/specs-go"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
)

const (
	containerDevdriPath = "/dev/dri"
)

func getGPUSpecs(cdiCache *cdiapi.Cache) []*cdiapi.Spec {
	gaudiSpecs := []*cdiapi.Spec{}
	for _, cdiSpec := range cdiCache.GetVendorSpecs(device.CDIVendor) {
		if cdiSpec.Kind == device.CDIKind {
			gaudiSpecs = append(gaudiSpecs, cdiSpec)
		}
	}
	return gaudiSpecs
}

// SyncDetectedDevicesWithRegistry adds detected devices into cdi registry if they are not yet there.
// Update existing registry devices with detected.
// Remove absent registry devices.
func SyncDetectedDevicesWithRegistry(cdiCache *cdiapi.Cache, detectedDevices device.DevicesInfo, doCleanup bool) error {

	vendorSpecs := getGPUSpecs(cdiCache)
	devicesToAdd := detectedDevices.DeepCopy()

	if len(vendorSpecs) == 0 {
		klog.V(5).Infof("No existing specs found for vendor %v, creating new", device.CDIVendor)
		if err := addNewDevicesToNewRegistry(cdiCache, devicesToAdd); err != nil {
			klog.V(5).Infof("Failed adding card to cdi registry: %v", err)
			return err
		}
		return nil
	}

	// loop through spec devices
	// - remove from CDI those not detected
	// - update with card and renderD indexes
	//   - delete from detected so they are not added as duplicates
	// - write spec
	// add rest of detected devices to first vendor spec
	for specidx, vendorSpec := range vendorSpecs {
		klog.V(5).Infof("checking vendorspec %v", specidx)

		specChanged := false // if devices were updated or deleted
		filteredDevices := []specs.Device{}

		for specDeviceIdx, specDevice := range vendorSpec.Devices {
			klog.V(5).Infof("checking device %v: %v", specDeviceIdx, specDevice)

			// if matched detected - check and update cardIdx and renderDIdx if needed - add to filtered Devices
			if detectedDevice, found := devicesToAdd[specDevice.Name]; found {

				if SyncDeviceNodes(specDevice, detectedDevice, device.CardRegexp, device.RenderdRegexp) {
					specChanged = true
				}

				filteredDevices = append(filteredDevices, specDevice)
				// Regardless if we needed to update the existing device or not,
				// it is in CDI registry so no need to add it again later.
				delete(devicesToAdd, specDevice.Name)
			} else if doCleanup {
				// skip CDI devices that were not detected
				klog.V(5).Infof("Removing device %v from CDI registry", specDevice.Name)
				specChanged = true
			} else {
				filteredDevices = append(filteredDevices, specDevice)
			}
		}
		// update spec if it was changed
		if specChanged {
			vendorSpec.Devices = filteredDevices
			specName := path.Base(vendorSpec.GetPath())
			klog.V(5).Infof("Updating spec %v", specName)
			err := cdiCache.WriteSpec(vendorSpec.Spec, specName)
			if err != nil {
				klog.Errorf("failed writing CDI spec %v: %v", vendorSpec.GetPath(), err)
				return fmt.Errorf("failed writing CDI spec %v: %v", vendorSpec.GetPath(), err)
			}
		}
	}

	if len(devicesToAdd) > 0 {
		// add devices that were not found in registry to the first existing vendor spec
		apispec := vendorSpecs[0]
		klog.V(5).Infof("Adding %d devices to CDI spec", len(devicesToAdd))
		AddDevicesToSpec(devicesToAdd, apispec.Spec)
		specName := path.Base(apispec.GetPath())

		cdiVersion, err := cdiapi.MinimumRequiredVersion(apispec.Spec)
		if err != nil {
			klog.Errorf("failed to get minimum CDI version for spec %v: %v", apispec.GetPath(), err)
			return fmt.Errorf("failed to get minimum CDI version for spec %v: %v", apispec.GetPath(), err)
		}
		if apispec.Version != cdiVersion {
			apispec.Version = cdiVersion
		}

		klog.V(5).Infof("Overwriting spec %v", specName)
		err = cdiCache.WriteSpec(apispec.Spec, specName)
		if err != nil {
			klog.Errorf("failed to write CDI spec %v: %v", apispec.GetPath(), err)
			return fmt.Errorf("failed write CDI spec %v: %v", apispec.GetPath(), err)
		}
	}

	return nil
}

func SyncDeviceNodes(
	specDevice specs.Device, detectedDevice *device.DeviceInfo,
	cardregexp *regexp.Regexp, renderdregexp *regexp.Regexp) bool {
	specChanged := false
	dridevpath := device.GetDriDevPath()

	for deviceNodeIdx, deviceNode := range specDevice.ContainerEdits.DeviceNodes {
		driFileName := path.Base(deviceNode.Path) // e.g. card1 or renderD129
		switch {
		case cardregexp.MatchString(driFileName):
			klog.V(5).Infof("CDI device node %v is a card device: %v", deviceNodeIdx, driFileName)
			cardIdx, err := strconv.ParseUint(strings.Split(driFileName, "card")[1], 10, 64)
			if err != nil {
				klog.Errorf("Failed to parse index of DRI card device '%v', skipping", driFileName)
				continue // deviceNode loop
			}
			if cardIdx != detectedDevice.CardIdx {
				klog.V(5).Infof("Fixing card index for CDI device %v", detectedDevice.UID)
				deviceNode.Path = path.Join(dridevpath, fmt.Sprintf("card%d", detectedDevice.CardIdx))
				specChanged = true
			}
		case renderdregexp.MatchString(driFileName):
			klog.V(5).Infof("CDI device node %v is a renderD device: %v", deviceNodeIdx, driFileName)
			renderdIdx, err := strconv.ParseUint(strings.Split(driFileName, "renderD")[1], 10, 64)
			if err != nil {
				klog.Errorf("Failed to parse index of DRI renderD device '%v', skipping", driFileName)
				continue // deviceNode loop
			}
			if renderdIdx != detectedDevice.RenderdIdx {
				klog.V(5).Infof("Fixing renderD index for CDI device %v", detectedDevice.UID)
				deviceNode.Path = path.Join(dridevpath, fmt.Sprintf("renderD%d", detectedDevice.RenderdIdx))
				specChanged = true
			}
		default:
			klog.Warningf("Unexpected device node %v in CDI device %v", deviceNode.Path, specDevice.Name)
		}
	}
	return specChanged
}

// addNewDevicesToNewRegistry writes devices into new vendor-specific CDI spec, should only be called if such spec does not exist.
func addNewDevicesToNewRegistry(cdiCache *cdiapi.Cache, devices device.DevicesInfo) error {
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
