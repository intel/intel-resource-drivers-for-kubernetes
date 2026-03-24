/*
 * Copyright (c) 2026, Intel Corporation.  All Rights Reserved.
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
	"time"

	"k8s.io/klog/v2"
	drahealthv1alpha1 "k8s.io/kubelet/pkg/apis/dra-health/v1alpha1"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
)

// registerHealthStream registers a new health stream to push health info into.
// This is used for reporting allocated device's health to Kubelet for Pod status.
func (d *driver) registerHealthStream(ch chan *drahealthv1alpha1.NodeWatchResourcesResponse) int {
	d.healthStreamsMutex.Lock()
	defer d.healthStreamsMutex.Unlock()

	d.healthStreamID++
	streamID := d.healthStreamID
	d.healthStreams[streamID] = ch
	klog.V(3).Infof("Registered health stream %d, total streams: %d", streamID, len(d.healthStreams))
	return streamID
}

// unregisterHealthStream removes a health stream by ID.
func (d *driver) unregisterHealthStream(streamID int) {
	d.healthStreamsMutex.Lock()
	defer d.healthStreamsMutex.Unlock()

	if ch, exists := d.healthStreams[streamID]; exists {
		close(ch)
		delete(d.healthStreams, streamID)
		klog.V(3).Infof("Unregistered health stream %d, remaining streams: %d", streamID, len(d.healthStreams))
	}
}

// buildHealthResponse builds a NodeWatchResourcesResponse with current health status.
// This function uses the state lock.
func (d *driver) buildHealthResponse() *drahealthv1alpha1.NodeWatchResourcesResponse {
	d.state.Lock()
	defer d.state.Unlock()

	devices := make([]*drahealthv1alpha1.DeviceHealth, 0)

	//nolint:forcetypeassert // We want the code to panic if our assumption turns out to be wrong.
	allocatable := d.state.Allocatable.(map[string]*device.DeviceInfo)

	for _, dev := range allocatable {
		deviceHealth := d.deviceInfoToDeviceHealth(dev)
		devices = append(devices, deviceHealth)
	}

	klog.V(5).Infof("Built health response with %d devices", len(devices))
	return &drahealthv1alpha1.NodeWatchResourcesResponse{Devices: devices}
}

/*
// broadcastHealthUpdateWithResponse sends a health update to all registered streams.
func (d *driver) broadcastHealthUpdateWithResponse(response *drahealthv1alpha1.NodeWatchResourcesResponse) {
	d.healthStreamsMutex.RLock()
	defer d.healthStreamsMutex.RUnlock()

	klog.V(5).Infof("Broadcasting health update to %d streams", len(d.healthStreams))
	for streamID, ch := range d.healthStreams {
		select {
		case ch <- response:
			klog.V(5).Infof("Sent health update to stream %d", streamID)
		default:
			klog.Warningf("Stream %d buffer full, skipping update", streamID)
		}
	}
}
*/

// deviceInfoToDeviceHealth converts a DeviceInfo to a DeviceHealth message.
func (d *driver) deviceInfoToDeviceHealth(dev *device.DeviceInfo) *drahealthv1alpha1.DeviceHealth {
	var healthStatus drahealthv1alpha1.HealthStatus

	// Map internal Health string to gRPC HealthStatus enum.
	switch dev.Health {
	case device.HealthHealthy:
		healthStatus = drahealthv1alpha1.HealthStatus_HEALTHY
	case device.HealthUnhealthy:
		healthStatus = drahealthv1alpha1.HealthStatus_UNHEALTHY
	default:
		// HealthUnknown or any unexpected value.
		healthStatus = drahealthv1alpha1.HealthStatus_UNKNOWN
	}

	deviceHealth := &drahealthv1alpha1.DeviceHealth{
		Device: &drahealthv1alpha1.DeviceIdentifier{
			PoolName:   d.state.NodeName,
			DeviceName: dev.UID,
		},
		Health:          healthStatus,
		LastUpdatedTime: time.Now().Unix(),
	}

	klog.V(3).Infof("Building health for device: pool=%s, device=%s, health=%v, healthStatus=%v",
		d.state.NodeName, dev.UID, dev.Health, healthStatus)

	return deviceHealth
}

// sendCurrentHealthStatus sends the current health status of all devices to a stream.
func (d *driver) sendCurrentHealthStatus(ctx context.Context, stream drahealthv1alpha1.DRAResourceHealth_NodeWatchResourcesServer) error {
	response := d.buildHealthResponse()
	if err := stream.Send(response); err != nil {
		return fmt.Errorf("failed to send health status: %w", err)
	}
	klog.Infof("Sent initial health status, devices: %d", len(response.Devices))
	return nil
}
