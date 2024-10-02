/* Copyright (C) 2024 Intel Corporation
 * SPDX-License-Identifier: Apache-2.0
 */

package main

import (
	"fmt"

	resourceapi "k8s.io/api/resource/v1alpha3"
	"k8s.io/utils/ptr"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/qat/device"
)

func deviceResources(qatvfdevices device.VFDevices) *[]resourceapi.Device {
	resourcedevices := []resourceapi.Device{}

	for _, qatvfdevice := range qatvfdevices {
		device := resourceapi.Device{
			Name: qatvfdevice.UID(),
			Basic: &resourceapi.BasicDevice{
				Attributes: map[resourceapi.QualifiedName]resourceapi.DeviceAttribute{
					"services": {
						StringValue: ptr.To(qatvfdevice.Services()),
					},
				},
			},
		}
		resourcedevices = append(resourcedevices, device)

		fmt.Printf("Adding Device resource: name '%s', service '%s'\n", device.Name, *device.Basic.Attributes["services"].StringValue)
	}

	return &resourcedevices
}
