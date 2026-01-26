package main

import (
	"context"
	"fmt"
	"time"

	"k8s.io/klog/v2"
	drahealthv1alpha1 "k8s.io/kubelet/pkg/apis/dra-health/v1alpha1"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/goxpusmi"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
)

type HealthStatusUpdates map[string]map[string]string

func (d *driver) startHealthMonitor(ctx context.Context, gpuFlags *GPUFlags) {
	healthStatusUpdatesCh := make(chan HealthStatusUpdates)
	goxpusmiCtx, stopMonitor := context.WithCancel(ctx)
	go d.watchGPUHealthStatuses(goxpusmiCtx, gpuFlags, healthStatusUpdatesCh)

	for {
		select {
		// Listen to original ctx, when driver is shutting down, stop HLML watcher.
		case <-goxpusmiCtx.Done():
			stopMonitor()
			return
		case healthDeltas := <-healthStatusUpdatesCh:
			d.updateHealth(goxpusmiCtx, healthDeltas)
		}
	}
}

// updateHealth applies health status updates, publishes the updated resource slice,
// and broadcasts the health update to all registered streams.
func (d *driver) updateHealth(ctx context.Context, healthStatusUpdates HealthStatusUpdates) {
	if err := d.applyHealthDeltas(healthStatusUpdates); err != nil {
		klog.Errorf("could not apply health deltas: %v", err)
		return
	}

	// Health is updated from a go routine, nothing we can do when publishing
	// resource slice fails, so error is only logged.
	if err := d.PublishResourceSlice(ctx); err != nil {
		klog.Errorf("could not publish updated resource slice: %v", err)
	}

	response := d.buildHealthResponse()
	// Broadcast health update to all connected health streams.
	d.broadcastHealthUpdateWithResponse(response)
}

func (d *driver) applyHealthDeltas(healthDeltas HealthStatusUpdates) error {
	d.state.Lock()
	defer d.state.Unlock()

	//nolint:forcetypeassert // We want the code to panic if our assumption turns out to be wrong.
	allocatable := d.state.Allocatable.(map[string]*device.DeviceInfo)

	for deviceUID, healthStatus := range healthDeltas {
		klog.Infof("Updating info for device %v to status=%v", deviceUID, healthStatus)
		foundDevice, found := allocatable[deviceUID]
		if !found {
			return fmt.Errorf("could not find allocatable device with UID %v", deviceUID)
		}

		// Determine overall health: healthy unless any status is CRITICAL.
		foundDevice.Health = device.HealthHealthy
		if foundDevice.HealthStatus == nil {
			// As xpu-smi initializes all health statuses to healthy,
			// we do the same here to have synchronized values.
			foundDevice.HealthStatus = map[string]string{
				"CoreThermal":   "OK",
				"MemoryThermal": "OK",
				"Power":         "OK",
				"Memory":        "OK",
				"FabricPort":    "OK",
				"Frequency":     "OK",
			}
		}

		for healthType, status := range healthStatus {
			foundDevice.HealthStatus[healthType] = status
		}
		for _, status := range foundDevice.HealthStatus {
			if !d.state.StatusHealth(status) {
				foundDevice.Health = device.HealthUnhealthy
				break
			}
		}
		klog.V(5).Infof("Updated health status for device: %v to %v", deviceUID, foundDevice.HealthStatus)
	}

	return nil
}

// watchGPUHealthStatuses polls XPUM metric health info and sends per-interval
// health status deltas to healthStatusUpdatesCh only when there are updates.
func (d *driver) watchGPUHealthStatuses(ctx context.Context, gpuFlags *GPUFlags, healthStatusUpdatesCh chan<- HealthStatusUpdates) {
	nonVerboseDiscovery := false
	devices, err := goxpusmi.Discover(nonVerboseDiscovery)
	if err != nil {
		klog.Errorf("could not discover devices for health monitoring: %v", err)
		return
	}

	if gpuFlags.CoreThermalLimit != HealthCoreThermalLimitUnset {
		goxpusmi.SetHealthConfig(devices, "CoreThermalLimit", gpuFlags.CoreThermalLimit)
	}
	if gpuFlags.MemoryThermalLimit != HealthMemoryThermalLimitUnset {
		goxpusmi.SetHealthConfig(devices, "MemoryThermalLimit", gpuFlags.MemoryThermalLimit)
	}
	if gpuFlags.PowerLimit != HealthPowerLimitUnset {
		goxpusmi.SetHealthConfig(devices, "PowerLimit", gpuFlags.PowerLimit)
	}

	HealthcareInterval := time.NewTicker(time.Duration(int(gpuFlags.HealthcareInterval)) * time.Second)
	for {
		select {
		case <-ctx.Done():
			if err = goxpusmi.Shutdown(); err != nil {
				klog.Errorf("failed to shutdown xpu-smi: %v", err)
			}
			return
		case <-HealthcareInterval.C:
			if updates := goxpusmi.HealthCheck(devices); len(updates) > 0 {
				healthStatusUpdatesCh <- updates
			}
		}
	}
}

// registerHealthStream registers a new health stream.
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

// sendCurrentHealthStatus sends the current health status of all devices to a stream.
func (d *driver) sendCurrentHealthStatus(ctx context.Context, stream drahealthv1alpha1.DRAResourceHealth_NodeWatchResourcesServer) error {
	response := d.buildHealthResponse()
	if err := stream.Send(response); err != nil {
		return fmt.Errorf("failed to send health status: %w", err)
	}
	klog.Infof("Sent initial health status, devices: %d", len(response.Devices))
	return nil
}

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
