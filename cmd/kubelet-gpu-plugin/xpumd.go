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
	"errors"
	"fmt"
	"io"
	"time"

	xpumapi "github.com/intel/xpumanager/xpumd/exporter/api/deviceinfo/v1alpha1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"k8s.io/klog/v2"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
	deviceHelpers "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/helpers"
)

const (
	DefaultSocketFilename  = "intelxpuinfo.sock"
	DefaultXPUMDSocketPath = "/run/xpumd/" + DefaultSocketFilename

	// Within 5 minutes xpumd should start and provide device health information,
	// otherwise the health monitoring will be disabled, potentially stopping
	// DRA driver init and result in a graceful exit with an error.
	ConnectAttemptsMax     = 30
	ConnectAttemptInterval = 10 * time.Second
)

func (d *driver) waitForXPUMDStream(ctx context.Context, c xpumapi.DeviceInfoClient, infiniteWait bool) (xpumapi.DeviceInfo_WatchDeviceHealthClient, error) {
	var err error
	var stream xpumapi.DeviceInfo_WatchDeviceHealthClient

	// Allow waiting infinitely for the XPUM daemon without causing a panic in the caller.
	attemptStep := 1
	if infiniteWait {
		attemptStep = 0
	}

	for attempt := 0; attempt < ConnectAttemptsMax; attempt += attemptStep {
		klog.V(5).Infof("trying to connect to xpumd, attempt %v/%v", attempt+1, ConnectAttemptsMax)
		stream, err = c.WatchDeviceHealth(ctx, &xpumapi.WatchDeviceHealthRequest{})
		if err == nil || d.stopXPUMDListener {
			break
		}

		klog.Error("xpumd-client: error calling WatchDeviceHealth", "error", err)
		time.Sleep(ConnectAttemptInterval)
	}

	return stream, err
}

// xpumdListen is the entrypoint go routine to receive health status and device details
// updates from XPUMD stream. The received updates are handled by ConsumeXPUMDDeviceDetails function.
func (d *driver) xpumdListen(ctx context.Context, socketFilePath string, infiniteWait bool) {
	klog.V(3).Info("starting xpumd listener")
	var conn *grpc.ClientConn

	conn, err := grpc.NewClient("unix://"+socketFilePath, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		klog.Error("xpumd-client: failed to create GRPC client, health monitoring will be disabled", "error", err)
		return
	}
	defer conn.Close() // nolint:errcheck

	c := xpumapi.NewDeviceInfoClient(conn)

	// If the main context is canceled, indicate to waitForXPUMDStream and infinite loop below to stop.
	go func() {
		<-ctx.Done()
		klog.V(5).Info("xpumd-client: context canceled, stopping xpumd listener")
		d.stopXPUMDListener = true
	}()

	stream, err := d.waitForXPUMDStream(ctx, c, infiniteWait)
	if err != nil {
		panic("xpumd-client: failed to connect to xpumd within expected time, exiting")
	}

	klog.V(5).Infof("xpumd-client: successfully connected to xpumd at %s", socketFilePath)

	errCounter := 0
	maxErrors := 5
	arbitraryErrorDelay := 5 * time.Second
	for {
		msg, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				// Socket was closed by remote, likely due to xpumd restart.
				// Try to reconnect same way as on startup..
				klog.Errorf("xpumd-client: error receiving data: %v", err)
				stream, err = d.waitForXPUMDStream(ctx, c, infiniteWait)
				if err != nil {
					panic("xpumd-client: failed to reconnect to xpumd, exiting")
				}
				// Messages need to be fetched, continue to the next loop iteration.
				continue
			} else {
				// Arbitrary error. Retry until maxErrors reached then panic in case the GRPC is incompatible.
				// If :latest DRA image tag is used chances are new image will fix the issue.
				if errCounter < maxErrors {
					klog.Errorf("xpumd-client: error receiving data: %v", err)
					errCounter++
					time.Sleep(arbitraryErrorDelay)
					continue
				}

				panic(fmt.Sprintf("xpumd-client: %v consecutive errors: %v", maxErrors, err))
			}
		}

		klog.V(6).Infof("xpumd-client: received %d device info items", len(msg.Devices))
		d.ConsumeXPUMDDeviceDetails(ctx, msg.GetDevices())

		if d.stopXPUMDListener {
			klog.Info("xpumd-client: stopping xpumd listener")
			return
		}
	}
}

// ConsumeXPUMDDeviceDetails passes the received info to the nodeState and publishes
// updated ResourceSlice if needed.
func (d *driver) ConsumeXPUMDDeviceDetails(ctx context.Context, devices []*xpumapi.DeviceHealth) {
	devicesInfoUpdate := xpumDevicesToAllocatableDevicesInfo(devices, d.ignoreHealthWarning)

	publishResourceSlice, err := d.state.applyDeviceUpdates(devicesInfoUpdate)
	if err != nil {
		klog.Errorf("could not apply health deltas: %v", err)
		return
	}

	// Exit early if no device updates reported by applyDeviceUpdates().
	if !publishResourceSlice {
		return
	}

	// XPUMD stream is handled by a go routine, nothing we can do when publishing
	// resource slice fails, so error is only logged.
	if err := d.PublishResourceSlice(ctx); err != nil {
		klog.Errorf("could not publish updated resource slice: %v", err)
	}

	// Resource Health Pod Status.
	// Broadcast health state to all connected health streams.
	response := d.buildHealthResponse()
	d.broadcastHealthUpdateWithResponse(response)
}

func xpumDevicesToAllocatableDevicesInfo(xpumDevice []*xpumapi.DeviceHealth, ignoreWarning bool) device.DevicesInfo {
	devicesInfo := device.DevicesInfo{}
	unhealthyThreshold := xpumapi.SeverityLevel_SEVERITY_LEVEL_WARNING
	if ignoreWarning {
		unhealthyThreshold = xpumapi.SeverityLevel_SEVERITY_LEVEL_CRITICAL
	}

	for _, xpumDevice := range xpumDevice {
		xpumDeviceInfo := xpumDevice.GetInfo()
		xpumDeviceHealth := xpumDevice.GetHealth()

		klog.V(6).Infof("xpumd-client: processing device %s: %v\n%v", xpumDeviceInfo.Pci.Bdf, xpumDeviceInfo, xpumDeviceHealth)
		deviceHealthStatus := make(map[string]string)
		for _, health := range xpumDeviceHealth {
			healthValue := device.HealthHealthy
			if health.GetSeverity() >= unhealthyThreshold {
				klog.V(6).Infof("xpumd-client: device %s health issue: %s severity: %s", xpumDeviceInfo.Pci.Bdf, health.GetName(), health.GetSeverity().String())
				healthValue = device.HealthUnhealthy
			}
			deviceHealthStatus[health.Name] = healthValue
		}

		model := xpumDeviceInfo.Pci.DeviceId
		if len(model) == 4 {
			model = "0x" + model
		}
		// Populate details and overall health.
		deviceInfo := &device.DeviceInfo{
			UID:          deviceHelpers.DeviceUIDFromPCIinfo(xpumDeviceInfo.Pci.Bdf, xpumDeviceInfo.Pci.DeviceId),
			PCIAddress:   xpumDeviceInfo.Pci.Bdf,
			Model:        model,
			ModelName:    xpumDeviceInfo.Model,
			HealthStatus: deviceHealthStatus,
		}

		klog.V(6).Infof("xpumd-client: device %s has memory info: %v", deviceInfo.UID, xpumDeviceInfo.Memory)
		if len(xpumDeviceInfo.Memory) > 0 {
			deviceInfo.MemoryMiB = xpumDeviceInfo.Memory[0].Size / (1024 * 1024)
		} else {
			klog.V(6).Infof("xpumd-client: device %s has no memory info", deviceInfo.UID)
		}

		devicesInfo[deviceInfo.UID] = deviceInfo
	}

	return devicesInfo
}

// applyDeviceUpdates processes XPUMD-supplied device details and health, and
// returns a bool of whether ResourceSlice update and publication is needed,
// and a possible error.
func (s *nodeState) applyDeviceUpdates(newDevicesInfo device.DevicesInfo) (bool, error) {
	s.Lock()
	defer s.Unlock()

	needToPublish := false

	//nolint:forcetypeassert // We want the code to panic if our assumption turns out to be wrong.
	allocatable := s.Allocatable.(map[string]*device.DeviceInfo)

	for deviceUID, newDeviceInfo := range newDevicesInfo {
		klog.V(6).Infof("Checking device %v info", deviceUID)
		foundDevice, found := allocatable[deviceUID]
		if !found {
			// TODO: re-discover to check if new device was hot-plugged.
			klog.Errorf("applying xpumd health info: could not find device %v, does xpumd need a restart?", deviceUID)
			continue
		}

		// Apply memory change if any:
		// - if DRA driver runs in non-privileged mode, XPUMD info can provide memory info.
		// - PF can change it's memory amount when VFs are enabled or disabled.
		if foundDevice.MemoryMiB != newDeviceInfo.MemoryMiB {
			klog.Infof("Device %v memory changed from %v MiB to %v MiB", deviceUID, foundDevice.MemoryMiB, newDeviceInfo.MemoryMiB)
			foundDevice.MemoryMiB = newDeviceInfo.MemoryMiB
			needToPublish = true
		}

		if needToUpdate := applyHealthStatus(foundDevice, newDeviceInfo); needToUpdate {
			needToPublish = true
		}

		klog.V(6).Infof("Updated health status for device: %v to: overall: %v; details: %v", deviceUID, foundDevice.Health(), foundDevice.HealthStatus)
	}

	return needToPublish, nil
}

func applyHealthStatus(foundDevice, newDeviceInfo *device.DeviceInfo) (needToPublish bool) {
	// If the Health status was previously HealthUnknown with 0 entries,
	// and now has some health information - publish new ResourceSlice.
	previouslyUnknown := foundDevice.Health() == device.HealthUnknown
	if previouslyUnknown && newDeviceInfo.Health() != device.HealthHealthy {
		needToPublish = true
	}

	// Only overall foundDevice.Health() is exposed in the ResourceSlice Device, and not foundDevice.HealthStatus.
	// Overall health is a logical AND of all HealthStatus elements. If any HealthStatus[X] changes - the new
	// ResourceSlice needs to be published.
	for newHealthType, newHealthStatus := range newDeviceInfo.HealthStatus {
		oldHealthValue, oldHealthFound := foundDevice.HealthStatus[newHealthType]
		// Update ResourceSlice if:
		// - the health was known before and has changed
		// - health was not known before and new status is unhealthy
		if (oldHealthFound && oldHealthValue != newHealthStatus) || (!oldHealthFound && newHealthStatus == device.HealthUnhealthy) {
			klog.Infof("Device %v health status for %v changed from %v to %v", foundDevice.UID, newHealthType, oldHealthValue, newHealthStatus)
			needToPublish = true
		}
	}

	// Check if some previously known health status is no longer reported. If it was known to be
	// unhealthy last time - consider its absence as healthy and indicate ResourceSlice
	// update is needed.
	for oldHealthType, oldHealthValue := range foundDevice.HealthStatus {
		if _, healthReported := newDeviceInfo.HealthStatus[oldHealthType]; !healthReported && oldHealthValue == device.HealthUnhealthy {
			klog.Infof("Device %v health status for %v is no longer reported, considered healthy", foundDevice.UID, oldHealthType)
			needToPublish = true
		}
	}

	// Copy custom health statuses (if any) from foundDevice to newDeviceInfo to allow easy replacement.
	for customHealthKey := range device.HealthCustomList {
		if customHealthValue, exists := foundDevice.HealthStatus[customHealthKey]; exists {
			newDeviceInfo.HealthStatus[customHealthKey] = customHealthValue
		}
	}

	// Finally, overwrite the health status with the new one as a whole.
	foundDevice.HealthStatus = newDeviceInfo.HealthStatus

	return needToPublish
}
