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

func (d *driver) waitForXPUMDStream(ctx context.Context, c xpumapi.DeviceInfoClient) (xpumapi.DeviceInfo_WatchDeviceHealthClient, error) {
	var err error
	var stream xpumapi.DeviceInfo_WatchDeviceHealthClient

	for attempt := 0; attempt < ConnectAttemptsMax; attempt++ {
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
func (d *driver) xpumdListen(ctx context.Context, socketFilePath string) {
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

	stream, err := d.waitForXPUMDStream(ctx, c)
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
				stream, err = d.waitForXPUMDStream(ctx, c)
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

		klog.V(5).Infof("xpumd-client: received %d device info items", len(msg.Devices))
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
		overallHealth := device.HealthHealthy

		klog.V(5).Infof("xpumd-client: processing device %s: %v\n%v", xpumDeviceInfo.Pci.Bdf, xpumDeviceInfo, xpumDeviceHealth)
		deviceHealthStatus := make(map[string]string)
		for _, health := range xpumDeviceHealth {
			healthValue := device.HealthHealthy
			if health.GetSeverity() >= unhealthyThreshold {
				klog.V(5).Infof("xpumd-client: device %s health issue: %s severity: %s", xpumDeviceInfo.Pci.Bdf, health.GetName(), health.GetSeverity().String())
				healthValue = device.HealthUnhealthy
				overallHealth = device.HealthUnhealthy
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
			Health:       overallHealth,
		}

		klog.V(5).Infof("xpumd-client: device %s has memory info: %v", deviceInfo.UID, xpumDeviceInfo.Memory)
		if len(xpumDeviceInfo.Memory) > 0 {
			deviceInfo.MemoryMiB = xpumDeviceInfo.Memory[0].Size / (1024 * 1024)
		} else {
			klog.V(5).Infof("xpumd-client: device %s has no memory info", deviceInfo.UID)
		}

		devicesInfo[deviceInfo.UID] = deviceInfo
	}

	return devicesInfo
}
