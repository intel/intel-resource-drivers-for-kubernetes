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

	"k8s.io/klog/v2"
	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"
	cdiSpecs "tags.cncf.io/container-device-interface/specs-go"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/qat/device"
)

func getQatSpecs(cdiCache *cdiapi.Cache) []*cdiapi.Spec {
	qatSpecs := []*cdiapi.Spec{}
	for _, cdiSpec := range cdiCache.GetVendorSpecs(device.CDIVendor) {
		if cdiSpec.Kind == device.CDIKind {
			qatSpecs = append(qatSpecs, cdiSpec)
		}
	}
	return qatSpecs
}

// AddDetectedDevicesToCDIRegistry adds detected devices into cdi registry after
// deleting old specs.
func AddDetectedDevicesToCDIRegistry(cdiCache *cdiapi.Cache, vfDevices device.VFDevices) error {
	qatSpecs := getQatSpecs(cdiCache)
	// delete all existing QAT specs.
	for _, spec := range qatSpecs {
		if err := cdiCache.RemoveSpec(spec.GetPath()); err != nil {
			return fmt.Errorf("failed to remove old CDI spec %v: %v", spec, err)
		}
	}

	if err := addDevicesToNewSpec(cdiCache, vfDevices); err != nil {
		return fmt.Errorf("failed adding devices to new CDI spec: %v", err)
	}

	return nil
}

// addDevicesToNewSpec creates new CDI spec, adds devices to it and calls writeSpec.
// Old specs are expected to be deleted before writing new spec.
func addDevicesToNewSpec(cdiCache *cdiapi.Cache, devices device.VFDevices) error {
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

func addDevicesToSpecAndWrite(cdiCache *cdiapi.Cache, vfDevices device.VFDevices, spec *cdiSpecs.Spec, specName string) error {
	for _, vf := range vfDevices {
		// primary / control node (for modesetting)
		newDevice := cdiSpecs.Device{
			Name: vf.UID(),
			ContainerEdits: cdiSpecs.ContainerEdits{
				DeviceNodes: []*cdiSpecs.DeviceNode{
					{Path: vf.DeviceNode(), Type: "c"},
				},
			},
		}
		spec.Devices = append(spec.Devices, newDevice)
	}

	if err := writeSpec(cdiCache, spec, specName); err != nil {
		return fmt.Errorf("failed to save new CDI spec %v: %v", specName, err)
	}
	return nil
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
