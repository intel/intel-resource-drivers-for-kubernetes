/*
 * Copyright (c) 2025, Intel Corporation.  All Rights Reserved.
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

package main

import (
	"context"
	"strings"

	"github.com/containers/nri-plugins/pkg/udev"
	"k8s.io/klog/v2"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
)

/* Cheat sheet from v7.0 kernel observed udev events:
- unbind event: {
	Header:unbind@/devices/pci0000:00/0000:00:06.0/0000:02:00.0/0000:03:01.0/0000:04:00.0
	Subsystem:pci
	Action:unbind
	Devpath:/devices/pci0000:00/0000:00:06.0/0000:02:00.0/0000:03:01.0/0000:04:00.0
	Seqnum:8348
	Properties:map[
		ACTION:unbind
		DEVPATH:/devices/pci0000:00/0000:00:06.0/0000:02:00.0/0000:03:01.0/0000:04:00.0
		PCI_CLASS:30000
		PCI_ID:8086:E211
		PCI_SLOT_NAME:0000:04:00.0
		PCI_SUBSYS_ID:1849:6023
		SEQNUM:8348
		SUBSYSTEM:pci
	]
}

- bind event: {
	Header:bind@/devices/pci0000:00/0000:00:06.0/0000:02:00.0/0000:03:01.0/0000:04:00.0
	Subsystem:pci
	Action:bind
	Devpath:/devices/pci0000:00/0000:00:06.0/0000:02:00.0/0000:03:01.0/0000:04:00.0
	Seqnum:8351
	Properties:map[
		ACTION:bind
		DEVPATH:/devices/pci0000:00/0000:00:06.0/0000:02:00.0/0000:03:01.0/0000:04:00.0
		DRIVER:xe-vfio-pci
		MODALIAS:pci:v00008086d0000E211sv00001849sd00006023bc03sc00i00
		PCI_CLASS:30000
		PCI_ID:8086:E211
		PCI_SLOT_NAME:0000:04:00.0
		PCI_SUBSYS_ID:1849:6023
		SEQNUM:8351
		SUBSYSTEM:pci
	]
}

- remove event: {
	Header:remove@/devices/pci0000:00/0000:00:06.0/0000:02:00.0/0000:03:01.0/0000:04:00.1
	Subsystem:pci
	Action:remove
	Devpath:/devices/pci0000:00/0000:00:06.0/0000:02:00.0/0000:03:01.0/0000:04:00.1
	Seqnum:8371
	Properties:map[
		ACTION:remove
		DEVPATH:/devices/pci0000:00/0000:00:06.0/0000:02:00.0/0000:03:01.0/0000:04:00.1
		MODALIAS:pci:v00008086d0000E211sv00001849sd00006023bc03sc00i00
		PCI_CLASS:30000
		PCI_ID:8086:E211
		PCI_SLOT_NAME:0000:04:00.1
		PCI_SUBSYS_ID:1849:6023
		SEQNUM:8371
		SUBSYSTEM:pci
	]
}

- change event {
	Header:change@/devices/pci0000:00/0000:00:06.0/0000:02:00.0/0000:03:01.0/0000:04:00.0
	Subsystem:pci
	Action:change
	Devpath:/devices/pci0000:00/0000:00:06.0/0000:02:00.0/0000:03:01.0/0000:04:00.0
	Seqnum:8393
	Properties:map[
		ACTION:change
		DEVPATH:/devices/pci0000:00/0000:00:06.0/0000:02:00.0/0000:03:01.0/0000:04:00.0
		DRIVER:xe
		MODALIAS:pci:v00008086d0000E211sv00001849sd00006023bc03sc00i00
		PCI_CLASS:30000
		PCI_ID:8086:E211
		PCI_SLOT_NAME:0000:04:00.0
		PCI_SUBSYS_ID:1849:6023
		SEQNUM:8393
		SUBSYSTEM:pci
	]
}

- add event {
	Header:add@/devices/pci0000:00/0000:00:06.0/0000:02:00.0/0000:03:01.0/0000:04:00.2
	Subsystem:pci
	Action:add
	Devpath:/devices/pci0000:00/0000:00:06.0/0000:02:00.0/0000:03:01.0/0000:04:00.2
	Seqnum:8384
	Properties:map[
		ACTION:add
		DEVPATH:/devices/pci0000:00/0000:00:06.0/0000:02:00.0/0000:03:01.0/0000:04:00.2
		MODALIAS:pci:v00008086d0000E211sv00001849sd00006023bc03sc00i00
		PCI_CLASS:30000
		PCI_ID:8086:E211
		PCI_SLOT_NAME:0000:04:00.2
		PCI_SUBSYS_ID:1849:6023
		SEQNUM:8384
		SUBSYSTEM:pci
	]
}
*/

// watchDevices polls for GPU/DRI device changes and republishes ResourceSlices when needed.
func (d *driver) watchDevices(ctx context.Context) {
	klog.V(5).Info("Starting to watch for device changes (DRIVER=xe, DRIVER=i915)")

	supportedEvents := map[string]bool{
		"bind":   true, // Driver change.
		"unbind": true, // Driver change.
		"change": true, // SR-IOV change: PFs are updated when VFs are enabled / disabled.
		"add":    true, // SR-IOV change: VFs appear this way.
		"remove": true, // SR-IOV change: VFs disappear this way.
	}

	filters := []map[string]string{
		{"DRIVER": "xe"},
		{"DRIVER": "i915"},
		{"DRIVER": "vfio-pci"},
		{"DRIVER": "xe-vfio-pci"},
		{"SUBSYSTEM": "pci"},
		{"SUBSYSTEM": "pci", "PCI_CLASS": device.UDEVPCIDisplayClassID},
		{"SUBSYSTEM": "pci", "PCI_CLASS": device.UDEVPCIVGAClassID},
	}
	filteredEvents := make(chan *udev.Event, 64)

	m, err := udev.NewMonitor(udev.WithFilters(filters...))
	if err != nil {
		klog.Errorf("failed to create udev event reader: %v", err)
		return
	}

	m.Start(filteredEvents)
	defer func() {
		klog.V(5).Info("stopping udev monitor")
		if err := m.Stop(); err != nil {
			klog.Errorf("failed to stop udev monitor: %v", err)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case evt := <-filteredEvents:
			// Ignore all events that are not binding / unbinding or that are for non Intel GPU class.
			class := evt.Properties["PCI_CLASS"]
			if class != device.UDEVPCIDisplayClassID && class != device.UDEVPCIVGAClassID {
				klog.V(5).Infof("Ignoring udev event for non-GPU PCI class device: %+v", evt)
				continue
			}
			// No 0x prefix in PCI_ID.
			if !strings.HasPrefix(evt.Properties["PCI_ID"], device.UDEVPCIVendorId) {
				klog.V(5).Infof("Ignoring udev event for non-Intel PCI Vendor device: %+v", evt)
				continue
			}
			if !supportedEvents[evt.Action] {
				klog.V(5).Infof("Ignoring udev event with unsupported action: %+v", evt)
				continue
			}
			d.refreshDeviceOnDriverEvent(ctx, evt)
		}
	}
}

// refreshDeviceOnDriverEvent rediscovers the affected GPU and compares the attributes with cached device info
// to find out if the change was triggered by the GPU DRA driver or not.
func (d *driver) refreshDeviceOnDriverEvent(ctx context.Context, evt *udev.Event) {
	klog.V(5).Infof("Refreshing devices after udev event: %+v", evt)

	pciAddress := evt.Properties["PCI_SLOT_NAME"]

	expectedDriver := ""
	if evt.Action == "bind" {
		expectedDriver = evt.Properties["DRIVER"]
	}

	shouldPublish, err := d.state.RefreshDeviceOnDriverEvent(pciAddress, expectedDriver)
	if err != nil {
		klog.Errorf("Failed to refresh device on driver event: %v", err)
	}

	if shouldPublish {
		if err := d.PublishResourceSlice(ctx); err != nil {
			klog.Errorf("could not publish updated resource slice: %v", err)
		}
	}
}
