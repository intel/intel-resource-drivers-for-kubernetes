package main

import (
	"context"
	"time"

	"k8s.io/klog/v2"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/goxpusmi"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
)

// HealthStatusUpdates is a type alias for map[deviceUID]map[healthType]status.
type HealthStatusUpdates map[string]map[string]string

func (d *driver) startHealthMonitor(ctx context.Context, gpuFlags *GPUFlags) {
	// Channel carries per-interval health status deltas keyed by device UID.
	healthStatusUpdatesCh := make(chan HealthStatusUpdates)
	goxpusmiCtx, stopMonitor := context.WithCancel(ctx)
	go d.watchGPUHealthStatuses(goxpusmiCtx, gpuFlags, healthStatusUpdatesCh)

	for {
		select {
		// Listen to original ctx, when driver is shutting down, stop health watcher.
		case <-goxpusmiCtx.Done():
			stopMonitor()
			return
		case healthDeltas := <-healthStatusUpdatesCh:
			d.updateHealth(goxpusmiCtx, healthDeltas)
		}
	}
}

func (d *driver) updateHealth(ctx context.Context, healthStatusUpdates HealthStatusUpdates) {
	d.state.Lock()
	defer d.state.Unlock()
	//nolint:forcetypeassert // We want the code to panic if our assumption turns out to be wrong.
	allocatable := d.state.Allocatable.(map[string]*device.DeviceInfo)
	for deviceUID, healthStatus := range healthStatusUpdates {
		klog.V(5).Infof("Updating info for device %v to status=%v", deviceUID, healthStatus)
		foundDevice, found := allocatable[deviceUID]
		if !found {
			klog.Errorf("could not find allocatable device with UID %v", deviceUID)
			return
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
		klog.V(5).Infof("Updated health status for devices: %v to %v", deviceUID, foundDevice.HealthStatus)
	}
	// Health is updated from a go routine, nothing we can do when publishing
	// resource slice fails, so error is only logged.
	if err := d.PublishResourceSlice(ctx); err != nil {
		klog.Errorf("could not publish updated resource slice: %v", err)
	}
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
