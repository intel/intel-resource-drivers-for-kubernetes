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
	"path"
	"strconv"
	"strings"

	cdiapi "github.com/container-orchestrated-devices/container-device-interface/pkg/cdi"
	specs "github.com/container-orchestrated-devices/container-device-interface/specs-go"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gaudi/device"
	"k8s.io/klog/v2"
)

func getGaudiCDISpecs(registry cdiapi.Registry) []*cdiapi.Spec {
	gaudiSpecs := []*cdiapi.Spec{}
	for _, cdiSpec := range registry.SpecDB().GetVendorSpecs(device.CDIVendor) {
		if cdiSpec.Kind == device.CDIKind {
			gaudiSpecs = append(gaudiSpecs, cdiSpec)
		}
	}
	return gaudiSpecs
}

// SyncDetectedDevicesWithCdiRegistry adds detected devices into cdi registry if they are not yet there.
// Update existing registry devices with detected.
// Remove absent registry devices.
func SyncDetectedDevicesWithCdiRegistry(registry cdiapi.Registry, detectedDevices device.DevicesInfo, doCleanup bool) error {
	gaudiSpecs := getGaudiCDISpecs(registry)
	if len(gaudiSpecs) == 0 {
		klog.V(5).Infof("No existing specs found for vendor %v of kind %v, creating new", device.CDIVendor, device.CDIKind)

		if err := addDevicesToNewSpec(registry, detectedDevices); err != nil {
			return fmt.Errorf("failed adding devices to new CID spec: %v", err)
		}

		return nil
	}

	devicesToAdd, err := updateDevicesInCDISpecsAndWrite(registry, detectedDevices, gaudiSpecs)
	if err != nil {
		return fmt.Errorf("failed updating CDI specs: %v", err)
	}

	if len(devicesToAdd) > 0 {
		apispec := gaudiSpecs[0]
		specName := path.Base(apispec.GetPath())

		klog.V(5).Infof("Adding %d new devices to CDI spec", len(devicesToAdd))

		return addDevicesToCDISpecAndWrite(registry, devicesToAdd, apispec.Spec, specName)
	}

	return nil
}

// updateDevicesInCDISpec updates existing devices with potentially new data in devicesToAdd
// and returns leftover devices that were not found in spec and need plain adding.
func updateDevicesInCDISpecsAndWrite(registry cdiapi.Registry, devicesToAdd device.DevicesInfo, vendorSpecs []*cdiapi.Spec) (device.DevicesInfo, error) {
	// loop through each Gaudi spec's devices
	// - remove from spec not detected devices
	// - update found devices with accel and accel_controlD indexes
	//   - delete from devicesToAdd so they are not added as duplicates
	// - write spec
	// add rest of detected devices to first vendor spec
	devices := devicesToAdd.DeepCopy()
	for specIdx, vendorSpec := range vendorSpecs {
		if vendorSpec.Kind != device.CDIKind {
			continue
		}

		klog.V(5).Infof("checking vendorspec %v", specIdx)

		specChanged := false // if devices were updated or deleted
		filteredDevices := []specs.Device{}

		for specDeviceIdx, specDevice := range vendorSpec.Devices {
			klog.V(5).Infof("checking device %v: %v", specDeviceIdx, specDevice)

			// if matched detected - check and update device nodes, if needed - add to filtered Devices
			if detectedDevice, found := devices[specDevice.Name]; found {

				if updateDeviceNodes(specDevice, detectedDevice) {
					specChanged = true
				}

				filteredDevices = append(filteredDevices, specDevice)
				// Regardless if we needed to update the existing device or not,
				// it is in CDI registry so no need to add it again later.
				delete(devices, specDevice.Name)
			} else {
				// skip CDI devices that were not detected
				klog.V(5).Infof("Removing device %v from CDI registry", specDevice.Name)
				specChanged = true
			}
		}

		// update spec if it was changed
		if specChanged {
			klog.V(5).Info("Replacing devices in spec with VFs filtered out")
			vendorSpec.Spec.Devices = filteredDevices
			specName := path.Base(vendorSpec.GetPath())

			klog.V(5).Infof("Overwriting spec %v", specName)
			if err := writeSpec(registry, vendorSpec.Spec, specName); err != nil {
				return nil, fmt.Errorf("failed to save CDI spec %v: %v", specName, err)
			}
		}
	}

	return devices, nil
}

// writeSpec sets latest cdiVersion for spec and writes it.
func writeSpec(registry cdiapi.Registry, spec *specs.Spec, specName string) error {
	cdiVersion, err := cdiapi.MinimumRequiredVersion(spec)
	if err != nil {
		return fmt.Errorf("failed to get minimum required CDI spec version: %v", err)
	}
	spec.Version = cdiVersion

	klog.V(5).Infof("Writing spec %v", specName)
	err = registry.SpecDB().WriteSpec(spec, specName)
	if err != nil {
		return fmt.Errorf("failed to write CDI spec %v: %v", specName, err)
	}

	return nil
}

func addDevicesToCDISpecAndWrite(registry cdiapi.Registry, devices device.DevicesInfo, spec *specs.Spec, specName string) error {
	for _, device := range devices {
		// primary / control node (for modesetting)
		newDevice := specs.Device{
			Name: device.UID,
			ContainerEdits: specs.ContainerEdits{
				DeviceNodes: newContainerEditsDeviceNodes(device.DeviceIdx),
			},
		}
		// TODO: add missing files, if any, when discovery is in place.
		spec.Devices = append(spec.Devices, newDevice)
	}

	if err := writeSpec(registry, spec, specName); err != nil {
		return fmt.Errorf("failed to save new CDI spec %v: %v", specName, err)
	}

	return nil
}

func updateDeviceNodes(specDevice specs.Device, detectedDevice *device.DeviceInfo) bool {
	replaceDeviceNodes := false

	for deviceNodeIdx, deviceNode := range specDevice.ContainerEdits.DeviceNodes {
		accelFileName := path.Base(deviceNode.Path) // e.g. accel1 or accel_controlD1
		var separatorToUse string
		switch {
		case device.AccelRegexp.MatchString(accelFileName):
			separatorToUse = "accel"
		case device.AccelControlRegexp.MatchString(accelFileName):
			separatorToUse = "accel_controlD"
		default:
			klog.Warningf("unexpected device node %v in CDI device %v", deviceNode.Path)

			continue
		}

		klog.V(5).Infof("CDI device node %v is an accel device: %v", deviceNodeIdx, accelFileName)
		deviceIdx, err := strconv.ParseUint(strings.Split(accelFileName, separatorToUse)[1], 10, 64)
		if err != nil {
			klog.Errorf("failed to parse index of Accel device '%v', skipping", accelFileName)

			continue
		}

		if deviceIdx != detectedDevice.DeviceIdx {
			replaceDeviceNodes = true

			break
		} else {
			klog.V(5).Info("Accel index for CDI device is correct")
		}
	}

	if replaceDeviceNodes {
		specDevice.ContainerEdits.DeviceNodes = newContainerEditsDeviceNodes(detectedDevice.DeviceIdx)
	}

	return replaceDeviceNodes
}

// addDevicesToNewSpec creates new CDI spec, adds devices to it and calls writeSpec.
// Should only be called if no vendor spec not exists.
func addDevicesToNewSpec(registry cdiapi.Registry, devices device.DevicesInfo) error {
	klog.V(5).Infof("Adding %v devices to new spec", len(devices))

	spec := &specs.Spec{
		Kind: device.CDIKind,
	}

	specName, err := cdiapi.GenerateNameForSpec(spec)
	if err != nil {
		return fmt.Errorf("failed to generate name for cdi device spec: %+v", err)
	}
	klog.V(5).Infof("New name for new CDI spec: %v", specName)

	return addDevicesToCDISpecAndWrite(registry, devices, spec, specName)
}

func newContainerEditsDeviceNodes(deviceIdx uint64) []*specs.DeviceNode {
	accelDevPath := device.GetDevfsAccelDir()
	return []*specs.DeviceNode{
		{Path: path.Join(accelDevPath, fmt.Sprintf("accel%d", deviceIdx)), Type: "c"},
		{Path: path.Join(accelDevPath, fmt.Sprintf("accel_controlD%d", deviceIdx)), Type: "c"},
	}
}
