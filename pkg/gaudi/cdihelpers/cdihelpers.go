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

	coreDiscovery "k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"
	cdiparser "tags.cncf.io/container-device-interface/pkg/parser"
	cdiSpecs "tags.cncf.io/container-device-interface/specs-go"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gaudi/device"
)

const (
	containerDevfsRoot = "/dev"
)

func getGaudiSpecs(cdiCache *cdiapi.Cache) []*cdiapi.Spec {
	gaudiSpecs := []*cdiapi.Spec{}
	for _, cdiSpec := range cdiCache.GetVendorSpecs(device.CDIVendor) {
		if cdiSpec.Kind == device.CDIKind {
			gaudiSpecs = append(gaudiSpecs, cdiSpec)
		}
	}
	return gaudiSpecs
}

// AddDetectedDevicesToCDIRegistry adds detected devices into cdi registry after deleting old specs.
func AddDetectedDevicesToCDIRegistry(cdiCache *cdiapi.Cache, detectedDevices device.DevicesInfo) error {
	gaudiSpecs := getGaudiSpecs(cdiCache)
	for _, spec := range gaudiSpecs {
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
				DeviceNodes: newContainerEditsDeviceNodes(device.DeviceIdx, device.UVerbsIdx),
			},
		}
		spec.Devices = append(spec.Devices, newDevice)
	}

	if err := writeSpec(cdiCache, spec, specName); err != nil {
		return fmt.Errorf("failed to save new CDI spec %v: %v", specName, err)
	}

	return nil
}

func newContainerEditsDeviceNodes(deviceIdx uint64, uverbsIdx uint64) []*cdiSpecs.DeviceNode {
	accelDevPath := device.GetAccelDevfsPath()
	infinibandDevPath := device.GetInfinibandDevfsPath()
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

	if uverbsIdx != device.UverbsMissingIdx {
		deviceNodes = append(deviceNodes, &cdiSpecs.DeviceNode{
			Path:     path.Join(containerDevfsRoot, device.DevfsInfiniBandPath, fmt.Sprintf("uverbs%d", uverbsIdx)),
			HostPath: path.Join(infinibandDevPath, fmt.Sprintf("uverbs%d", uverbsIdx)),
			Type:     "c",
		})
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

// NewBlankDevice adds a special CDI device with no device nodes, but with
// Gaudi-specific env variables that span multiple devices, and cannot be in a
// particular Gaudi CDI device. This "blank" device is mutated before saving:
// a CID hook entry for Gaudi NICs is added here.
func NewBlankDevice(cdiCache *cdiapi.Cache, newDevice cdiSpecs.Device, hookPath string) error {
	vendorSpecs := cdiCache.GetVendorSpecs(device.CDIVendor)
	if len(vendorSpecs) == 0 {
		return fmt.Errorf("no %v CDI specs found", device.CDIVendor)
	}
	cdiSpec := vendorSpecs[0]

	newDevice.ContainerEdits.Hooks = []*cdiSpecs.Hook{
		{
			HookName: "createRuntime",
			Path:     hookPath,
			Args:     []string{filepath.Base(device.DefaultHabanaHookPath), "createRuntime"},
			Env: []string{
				fmt.Sprintf("PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:%s", filepath.Dir(device.DefaultHabanaHookPath)),
			},
		},
	}

	// Add gaudinet mount if it exists.
	if _, err := os.Stat(device.GaudinetPath); err == nil {
		newDevice.ContainerEdits.Mounts = []*cdiSpecs.Mount{
			{
				HostPath:      device.GaudinetPath,
				ContainerPath: device.GaudinetPath,
				Options:       []string{"bind"},
			},
		}
	}

	cdiSpec.Devices = append(cdiSpec.Devices, newDevice)
	specName := path.Base(cdiSpec.GetPath())

	return writeSpec(cdiCache, cdiSpec.Spec, specName)
}

// DeleteBlankDevices removes the special CDI devices that contains only env vars,
// and no device nodes. Its name is the UUID of the resource claim it was created for.
func DeleteBlankDevices(cdiCache *cdiapi.Cache, claimUID string) error {
	qualifiedName := cdiparser.QualifiedName(device.CDIVendor, device.CDIClass, claimUID)
	cdidev := cdiCache.GetDevice(qualifiedName)
	if cdidev == nil {
		return nil
	}

	filteredDevices := make([]cdiSpecs.Device, len(cdidev.GetSpec().Devices)-1)
	filterIdx := 0
	cdiSpec := cdidev.GetSpec()

	for _, device := range cdiSpec.Devices {
		if device.Name != claimUID {
			filteredDevices[filterIdx] = device
			filterIdx++
		}
	}
	cdiSpec.Devices = filteredDevices
	specName := path.Base(cdiSpec.GetPath())

	return writeSpec(cdiCache, cdiSpec.Spec, specName)
}

func IsRHOCP(restClient rest.Interface) bool {
	discoveryClient := coreDiscovery.NewDiscoveryClient(restClient)
	apiGroups, err := discoveryClient.ServerGroups()
	if err != nil {
		klog.Errorf("failed to get server groups: %v", err)
		return false
	}
	openShiftGroups := map[string]bool{
		"config.openshift.io":   true,
		"operator.openshift.io": true,
		"security.openshift.io": true,
		"project.openshift.io":  true,
	}
	for _, group := range apiGroups.Groups {
		if _, found := openShiftGroups[group.Name]; found {
			return true
		}
	}
	return false
}
