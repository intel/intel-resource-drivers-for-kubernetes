/* Copyright (C) 2024 Intel Corporation
 * SPDX-License-Identifier: Apache-2.0
 */

package main

import (
	"fmt"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/qat/device"
)

func printPFDevice(pfdev *device.PFDevice) {
	fmt.Printf("PF device: %s\n", pfdev.Device)
	fmt.Printf("State:     %s\n", pfdev.State.String())
	fmt.Printf("Services:  %s\n", pfdev.Services.String())
	fmt.Printf("Num VFs:   %d\n", pfdev.NumVFs)
	fmt.Printf("Max VFs:   %d\n", pfdev.TotalVFs)

	for _, vfdev := range pfdev.AvailableDevices {
		fmt.Printf("\tVF UID %s: device %s, device node %s, IOMMU %s, driver %s\n", vfdev.UID(), vfdev.PCIDevice(), vfdev.DeviceNode(), vfdev.Iommu(), vfdev.Driver())
	}
}

func main() {
	pfdevices, err := device.New()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	if len(pfdevices) == 0 {
		fmt.Printf("No PF devices found\n")
		return
	}

	for _, pfdev := range pfdevices {
		printPFDevice(pfdev)
		fmt.Printf("---\n\n")
	}

}
