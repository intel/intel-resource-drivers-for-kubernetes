/*
 * Copyright (c) 2024, Intel Corporation. All Rights Reserved.
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
	"fmt"
	"maps"
	"slices"
	"time"

	hlml "github.com/HabanaAI/gohlml"
	resourceapi "k8s.io/api/resource/v1alpha3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gaudi/device"
)

const (
	defaultHealthCheckIntervalSeconds = int(10)
)

// initHLML loops through devices HLML detecs to update serial number in allocatable.
// This is needed for health monitoring, critical events contain device serial ID.
func (d *driver) initHLML() error {
	ret := hlml.InitWithLogs()
	if ret != nil {
		return fmt.Errorf("failed to initialize HLML: %v", ret)
	}

	count, ret := hlml.DeviceCount()
	if ret != nil {
		return fmt.Errorf("failed to get device count: %v", ret)
	}

	state := nodeState{d.state}

	for i := uint(0); i < count; i++ {
		hlmlDevice, err := hlml.DeviceHandleByIndex(i)
		if err != nil {
			return fmt.Errorf("failed to get device at index %d: %v", i, err)
		}

		serial, err := hlmlDevice.SerialNumber()
		if err != nil {
			return fmt.Errorf("failed to get serial number of device at index %d: %v", i, err)
		}

		pciAddress, err := hlmlDevice.PCIBusID()
		if err != nil {
			return fmt.Errorf("failed to get PCI bus ID of device at index %d: %v", i, err)
		}

		gaudi := state.AllocatableByPCIAddress(pciAddress)
		if gaudi == nil {
			return fmt.Errorf("could not find allocatable device with PCI address %v", pciAddress)
		}
		gaudi.Serial = serial
	}

	return nil
}

// monitorHealth spawns a single Go routine to watch for events that
// might signal about device becoming unusable. If such an event
// happens, the ResourceSlice will be updated with kubernetes.io/healthy
// attribute set false.
// See https://github.com/kubernetes/kubernetes/issues/128979
//
// TODO: use KEP-5055: DRA: device taints and tolerations, when it is implemented.
func (d *driver) startHealthMonitor(ctx context.Context, intervalSeconds int) {
	// Watch for device UIDs to mark unhealthy.
	idsChan := make(chan string)
	hlmlContext, stopHLMLMonitor := context.WithCancel(ctx)
	go d.watchCriticalHLMLEvents(hlmlContext, intervalSeconds, idsChan)

	for {
		select {
		// Listen to original ctx, when driver is shutting down, stop HLML watcher.
		case <-ctx.Done():
			stopHLMLMonitor()
			return
		case unhealthyUID := <-idsChan:
			d.updateHealth(hlmlContext, false, unhealthyUID)
		}
	}
}

// updateHealth is called from healthMonitor to change device health flag and
// publish updated resource slice.
func (d *driver) updateHealth(ctx context.Context, healthy bool, uid string) {
	d.state.Lock()
	defer d.state.Unlock()

	allocatable, _ := d.state.Allocatable.(map[string]*device.DeviceInfo)
	foundDevice, found := allocatable[uid]
	if !found {
		klog.Errorf("could not find device with UID %v", uid)
		return
	}

	d.createTaintRuleMaybe(ctx, uid)

	foundDevice.Healthy = healthy
	// Health is updated from a go routine, nothing we can do when publishing
	// resource slice fails, so error is ignored.
	if err := d.PublishResourceSlice(ctx); err != nil {
		klog.Errorf("could not publish updated resoruce slice: %v", err)
	}
}

// createTaintRuleMaybe ensures there is a DeviceTaintRule for the device that
// became unhealthy.
func (d *driver) createTaintRuleMaybe(ctx context.Context, uid string) {
	taintRuleName := fmt.Sprintf("%v-%v-%v", device.DriverName, d.state.NodeName, uid)
	driverName := device.DriverName
	// Taint failed device, so it will not be scheduled.
	devTaintRule := resourceapi.DeviceTaintRule{
		ObjectMeta: metav1.ObjectMeta{
			Name: taintRuleName,
		},
		Spec: resourceapi.DeviceTaintRuleSpec{
			DeviceSelector: &resourceapi.DeviceTaintSelector{
				Driver: &driverName,
				Pool:   &d.state.NodeName,
				Device: &uid,
			},
			Taint: resourceapi.DeviceTaint{
				Key:    fmt.Sprintf("%s/unhealthy", device.DriverName),
				Value:  "CriticalError",
				Effect: resourceapi.DeviceTaintEffectNoExecute,
			},
		},
	}

	// Check if the rule already exists, or new rule creation will fail because of the name conflict.
	rule, err := d.client.ResourceV1alpha3().DeviceTaintRules().Get(ctx, taintRuleName, metav1.GetOptions{})
	if err == nil && rule != nil {
		klog.FromContext(ctx).Info("Found existing DeviceTaintRule", "rule", rule)
		return
	}

	klog.FromContext(ctx).Info("creating DeviceTaintRule", "rule", devTaintRule)
	_, err = d.client.ResourceV1alpha3().DeviceTaintRules().Create(ctx, &devTaintRule, metav1.CreateOptions{})
	if err != nil {
		klog.Errorf("failed to create device taint rule: %v", err)
	}
}

// watchCriticalHLMLEvents watches for critical events from HLML and marks the devices as unhealthy.
func (d *driver) watchCriticalHLMLEvents(ctx context.Context, intervalSeconds int, idsChan chan<- string) {
	eventSet := hlml.NewEventSet()
	defer hlml.DeleteEventSet(eventSet)

	allocatable, _ := d.state.Allocatable.(map[string]*device.DeviceInfo)

	allFailed := true
	for _, d := range allocatable {
		err := hlml.RegisterEventForDevice(eventSet, hlml.HlmlCriticalError, d.Serial)
		if err != nil {
			klog.Error("Failed registering critial event for device. Marking it unhealthy", "UID", d.UID, "error", err)
			idsChan <- d.UID
			continue
		}
		allFailed = false
	}

	if allFailed {
		return
	}

	healthCheckInterval := time.NewTicker(time.Duration(intervalSeconds) * time.Second)

	for {
		select {
		case <-ctx.Done():
			return
		case <-healthCheckInterval.C:
			if pushUIDs, uids := d.timedHLMLEventCheck(eventSet); pushUIDs {
				for _, uid := range uids {
					idsChan <- uid
				}
			}
		}
	}
}

// getUIDsOfDevicesWithHandleError returns the UIDs of devices for which getting a handle by serial has failed.
func getUIDsOfDevicesWithHandleError(allocatable map[string]*device.DeviceInfo) (uids []string) {
	for _, device := range allocatable {
		if _, err := hlml.DeviceHandleBySerial(device.Serial); err != nil {
			klog.Errorf("critical: could not get device %v handle by serial, marking unhealthy", device.UID)
			uids = append(uids, device.UID)

			return uids
		}
	}
	return []string{}
}

// timedHLMLEventCheck returns true if any device is unhealthy, and list of UIDs of unhealthy devices.
func (d *driver) timedHLMLEventCheck(eventSet hlml.EventSet) (bool, []string) {
	uids := []string{}
	allocatable, _ := d.state.Allocatable.(map[string]*device.DeviceInfo)
	updateHealth := false

	e, err := hlml.WaitForEvent(eventSet, 1000)
	if err != nil {
		klog.Errorf("HLML WaitForEvent failed: %v", err)

		uids = getUIDsOfDevicesWithHandleError(allocatable)
		updateHealth = len(uids) > 0
		time.Sleep(2 * time.Second)
		return updateHealth, uids
	}

	klog.V(5).Infof("HLML event received: %+v", e)

	if e.Etype != hlml.HlmlCriticalError {
		klog.V(5).Infof("Ignoring unexpected non-critical HLML error event: %+v", e)
		return false, uids
	}

	dev, err := hlml.DeviceHandleBySerial(e.Serial)
	if err != nil {
		klog.Error("critical: could not get device handle by serial. All devices will go unhealthy", "event", e.Etype)
		// All devices are unhealthy
		return true, slices.Collect(maps.Keys(allocatable))
	}

	serial, err := dev.SerialNumber()
	if err != nil || len(serial) == 0 {
		klog.Error("critical: could not get serial. All devices will go unhealthy", "event", e.Etype)
		// All devices are unhealthy
		return true, slices.Collect(maps.Keys(allocatable))
	}

	for deviceUID, d := range allocatable {
		if d.Serial == serial {
			klog.Error("critical: the device is unhealthy. ", "UID: ", deviceUID, " xid: ", e.Etype, " serial: ", d.Serial)
			uids = append(uids, d.UID)
			return true, uids
		}
	}

	// This should be theoretically impossible since we signed up only for devices that we know about.
	klog.Error("critical: could not find event device serial in Allocatable. All devices will go unhealthy", "event", e.Etype)
	uids = slices.Collect(maps.Keys(allocatable))

	return true, uids
}

func (d *driver) Shutdown(ctx context.Context) error {
	klog.V(5).Info("Shutting down driver")

	d.helper.Stop()

	// When health monitoring with HLML was initiated, d.hlmlShutdown will get
	// context cancel function, which we can call to signal health monitoring
	// goroutine to stop.
	if d.hlmlShutdown != nil {
		d.hlmlShutdown()

		time.Sleep(1 * time.Second)

		err := hlml.Shutdown()
		if err != nil {
			klog.Errorf("failed to shutdown HLML: %v", err)
		}
	}

	return nil
}
