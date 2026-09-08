//
// Copyright (C) 2024-2025 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0
//

package main

import (
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/klog/v2"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/qat/device"
)

func deviceResources(qatvfdevices device.VFDevices) *[]resourceapi.Device {
	resourcedevices := []resourceapi.Device{}

	for _, qatvfdevice := range qatvfdevices {
		services := qatvfdevice.Services()
		device := resourceapi.Device{
			Name: qatvfdevice.UID(),
			Attributes: map[resourceapi.QualifiedName]resourceapi.DeviceAttribute{
				"services": {
					StringValue: &services,
				},
			},
		}
		resourcedevices = append(resourcedevices, device)

		klog.V(5).Infof("Adding Device resource: name '%s', service '%s'", device.Name, *device.Attributes["services"].StringValue)
	}

	return &resourcedevices
}
