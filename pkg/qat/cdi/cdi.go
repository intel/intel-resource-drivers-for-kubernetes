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

package cdi

import (
	"fmt"
	"path"

	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"
	cdispecs "tags.cncf.io/container-device-interface/specs-go"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/qat/device"
)

const (
	CDIRoot   = cdiapi.DefaultDynamicDir
	CDIVendor = "intel.com"
	CDIClass  = "qat"
	CDIKind   = CDIVendor + "/" + CDIClass
)

type CDI struct {
	cache *cdiapi.Cache
}

func New(cdidir string) (*CDI, error) {
	fmt.Printf("Setting up CDI\n")

	if err := cdiapi.Configure(cdiapi.WithSpecDirs(cdidir)); err != nil {
		return nil, fmt.Errorf("unable to refresh the CDI registry: %v", err)
	}

	cdiCache := cdiapi.GetDefaultCache()

	cdi := &CDI{
		cache: cdiCache,
	}

	return cdi, nil
}

func (c *CDI) getQatSpecs() []*cdiapi.Spec {
	qatSpecs := []*cdiapi.Spec{}
	for _, cdiSpec := range c.cache.GetVendorSpecs(CDIVendor) {
		if cdiSpec.Kind == CDIKind {
			qatSpecs = append(qatSpecs, cdiSpec)
		}
	}
	return qatSpecs
}

func (c *CDI) SyncDevices(vfdevices device.VFDevices) error {
	fmt.Printf("Sync CDI devices\n")

	vfspec := &cdispecs.Spec{
		Kind: CDIKind,
	}
	vfspecname := cdiapi.GenerateSpecName(CDIVendor, CDIClass)

	for _, vendorspec := range c.getQatSpecs() {
		vendorspecname := path.Base(vendorspec.GetPath())

		if vendorspec.Kind != CDIKind {
			fmt.Printf("Spec file %s is for other kind %s, skippng...\n", vendorspecname, vendorspec.Kind)
			continue
		}

		fmt.Printf("spec file %s of kind %s\n", vendorspecname, vendorspec.Kind)

		name := vfspecname + path.Ext(vendorspecname)
		if name == vendorspecname {
			fmt.Printf("file %s where to add the rest of the devices\n", name)
			vfspec = vendorspec.Spec
		}

		vendorspecupdate := false
		vendorspecdevices := []cdispecs.Device{}

		for _, vendordevice := range vendorspec.Devices {
			if _, exists := vfdevices[vendordevice.Name]; exists {
				fmt.Printf("vendor spec %s contains device name %s\n", vendorspecname, vendordevice.Name)

				delete(vfdevices, vendordevice.Name)
				vendorspecdevices = append(vendorspecdevices, vendordevice)
			} else {
				fmt.Printf("CDI device %s not found on host\n", vendordevice.Name)
				vendorspecupdate = true
			}
		}
		if vendorspecupdate {
			fmt.Printf("Updating spec file %s with existing devices\n", path.Base(vendorspec.GetPath()))
			vendorspec.Devices = vendorspecdevices
			err := c.cache.WriteSpec(vendorspec.Spec, vendorspecname)
			if err != nil {
				fmt.Printf("failed to overwrite CDI spec %s: %v", vendorspecname, err)
			}
		}
	}

	if len(vfdevices) > 0 {
		return c.appendDevices(vfspec, vfdevices, vfspecname)
	}

	return nil
}

func (c *CDI) adddevicespec(spec *cdispecs.Spec, vfdevices device.VFDevices) error {
	fmt.Printf("Add detected devices\n")
	for _, vf := range vfdevices {
		cdidevice := cdispecs.Device{
			Name: vf.UID(),
			ContainerEdits: cdispecs.ContainerEdits{
				DeviceNodes: []*cdispecs.DeviceNode{
					{Path: vf.DeviceNode(), Type: "c"},
				},
			},
		}
		spec.Devices = append(spec.Devices, cdidevice)

		fmt.Printf("added device %s name %s\n", cdidevice.ContainerEdits.DeviceNodes[0].Path, cdidevice.Name)
	}
	return nil
}

func (c *CDI) appendDevices(spec *cdispecs.Spec, vfdevices device.VFDevices, name string) error {

	fmt.Printf("Append CDI devices\n")

	if err := c.adddevicespec(spec, vfdevices); err != nil {
		return err
	}

	version, err := cdiapi.MinimumRequiredVersion(spec)
	if err != nil {
		return fmt.Errorf("minimum CDI spec version not found: %v", err)
	}
	spec.Version = version

	err = c.cache.WriteSpec(spec, name)
	if err != nil {
		return fmt.Errorf("failed to write CDI spec %s: %v", name, err)
	}

	fmt.Printf("CDI %s: Kind %s, Version %v\n", name, spec.Kind, spec.Version)
	return nil
}

func (c *CDI) OverwriteDevices(vfdevices device.VFDevices) error {
	var err error

	fmt.Printf("Add/overwrite CDI devices\n")

	spec := &cdispecs.Spec{
		Kind: CDIKind,
	}

	name, err := cdiapi.GenerateNameForSpec(spec)
	if err != nil {
		return fmt.Errorf("spec name not created: %v", err)
	}

	return c.appendDevices(spec, vfdevices, name)
}
