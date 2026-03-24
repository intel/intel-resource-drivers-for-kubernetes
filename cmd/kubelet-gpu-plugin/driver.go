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
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/containers/nri-plugins/pkg/udev"
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/runtime"
	coreclientset "k8s.io/client-go/kubernetes"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
	"k8s.io/klog/v2"
	drahealthv1alpha1 "k8s.io/kubelet/pkg/apis/dra-health/v1alpha1"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/discovery"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/helpers"
	driverVersion "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/version"
)

type driver struct {
	client     coreclientset.Interface
	state      *nodeState
	helper     *kubeletplugin.Helper
	healthcare bool

	// Health streaming support
	healthStreams      map[int]chan *drahealthv1alpha1.NodeWatchResourcesResponse
	healthStreamsMutex sync.RWMutex
	healthStreamID     int
	healthcheck        *healthcheck

	// Embed unimplemented server for forward compatibility
	drahealthv1alpha1.UnimplementedDRAResourceHealthServer
}

func (d *driver) PublishResourceSlice(ctx context.Context) error {
	resources := d.state.GetResources()
	klog.FromContext(ctx).Info("Publishing resources", "len", len(resources.Pools[d.state.NodeName].Slices[0].Devices))
	klog.V(5).Infof("devices: %+v", resources.Pools[d.state.NodeName].Slices[0].Devices)
	if err := d.helper.PublishResources(ctx, resources); err != nil {
		return fmt.Errorf("error publishing resources: %v", err)
	}

	return nil
}

func getGPUFlags(someFlags any) (*GPUFlags, error) {
	switch v := someFlags.(type) {
	case *GPUFlags:
		return v, nil
	default:
		return &GPUFlags{}, fmt.Errorf("could not parse driver flags as GPUFlags (got type: %T)", v)
	}
}

func newDriver(ctx context.Context, config *helpers.Config) (helpers.Driver, error) {
	driverVersion.PrintDriverVersion(device.DriverName)

	gpuFlags, err := getGPUFlags(config.DriverFlags)
	if err != nil {
		return nil, fmt.Errorf("get GPU flags: %w", err)
	}

	driver := &driver{
		client: config.Coreclient,
		state: &nodeState{
			NodeState: &helpers.NodeState{
				PreparedClaimsFilePath: path.Join(config.CommonFlags.KubeletPluginDir, device.PreparedClaimsFileName),
				SysfsRoot:              helpers.GetSysfsRoot(device.SysfsDRMpath),
				NodeName:               config.CommonFlags.NodeName,
			},
			ignoreHealthWarning: gpuFlags.IgnoreHealthWarning,
		},
		healthcare:    gpuFlags.Healthcare,
		healthStreams: make(map[int]chan *drahealthv1alpha1.NodeWatchResourcesResponse),
	}

	klog.V(5).Infof("Prepared claims: %v", driver.state)

	detectedDevices := discovery.DiscoverDevices(driver.state.SysfsRoot, device.DefaultNamingStyle)
	if len(detectedDevices) == 0 {
		klog.Warning("No supported devices detected on this node")
	}

	if !driver.healthcare {
		klog.V(5).Info("Healthcare is disabled, setting all device health to HealthUnknown")
		for _, dev := range detectedDevices {
			dev.Health = device.HealthUnknown
		}
	}

	klog.V(3).Info("Creating new NodeState")
	driver.state, err = newNodeState(detectedDevices, config.CommonFlags.CdiRoot, driver.state.PreparedClaimsFilePath, driver.state.SysfsRoot, driver.state.NodeName, gpuFlags.IgnoreHealthWarning)
	if err != nil {
		return nil, fmt.Errorf("failed to create new NodeState: %v", err)
	}

	klog.Infof(`Starting DRA kubelet-plugin
RegistrarDirectoryPath: %v
PluginDataDirectoryPath: %v`,
		config.CommonFlags.KubeletPluginsRegistryDir,
		config.CommonFlags.KubeletPluginDir)

	helper, err := kubeletplugin.Start(
		ctx,
		driver,
		kubeletplugin.KubeClient(config.Coreclient),
		kubeletplugin.NodeName(config.CommonFlags.NodeName),
		kubeletplugin.DriverName(device.DriverName),
		kubeletplugin.RegistrarDirectoryPath(config.CommonFlags.KubeletPluginsRegistryDir),
		kubeletplugin.PluginDataDirectoryPath(config.CommonFlags.KubeletPluginDir),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to start kubelet-plugin: %v", err)
	}

	driver.helper = helper

	klog.V(3).Info("Publishing ResourceSlice")
	if err := driver.PublishResourceSlice(ctx); err != nil {
		return nil, err
	}

	// Enable health- and readiness- probes endpoints.
	hc, err := startHealthcheck(ctx, gpuFlags.HealthcheckPort,
		config.CommonFlags.KubeletPluginsRegistryDir,
		config.CommonFlags.KubeletPluginDir,
		device.DriverName)
	if err != nil {
		klog.Errorf("Failed to start health check server: %v", err)
	}
	driver.healthcheck = hc

	if driver.healthcare {
		klog.Info("Starting health monitoring")

		// Start device change watcher
		go driver.watchDevices(ctx)
	}

	klog.V(3).Info("Finished creating new driver")
	return driver, nil
}

func (d *driver) PrepareResourceClaims(ctx context.Context, claims []*resourceapi.ResourceClaim) (map[types.UID]kubeletplugin.PrepareResult, error) {
	klog.V(5).Infof("NodePrepareResource is called: number of claims: %d", len(claims))

	response := map[types.UID]kubeletplugin.PrepareResult{}

	for _, claim := range claims {
		response[claim.UID] = d.prepareResourceClaim(ctx, claim)
	}

	return response, nil
}

func (d *driver) prepareResourceClaim(ctx context.Context, claim *resourceapi.ResourceClaim) kubeletplugin.PrepareResult {
	klog.V(5).Infof("NodePrepareResource is called for claim %v", claim.UID)

	if claimPreparation, found := d.state.Prepared[string(claim.UID)]; found {
		klog.V(3).Infof("Claim %v was already prepared, nothing to do", claim.UID)
		return claimPreparation
	}

	if err := d.state.Prepare(ctx, claim); err != nil {
		return kubeletplugin.PrepareResult{
			Err: fmt.Errorf("error preparing devices for claim %v: %v", claim.UID, err),
		}
	}

	return d.state.Prepared[string(claim.UID)]
}

func (d *driver) UnprepareResourceClaims(ctx context.Context, claims []kubeletplugin.NamespacedObject) (map[types.UID]error, error) {
	klog.V(5).Infof("NodeUnprepareResource is called: number of claims: %d", len(claims))
	response := map[types.UID]error{}

	for _, claim := range claims {
		if err := d.state.Unprepare(ctx, string(claim.UID)); err != nil {
			response[claim.UID] = fmt.Errorf("could not unprepare resource: %v", err)
		} else {
			response[claim.UID] = nil
		}
	}

	return response, nil
}

func (d *driver) Shutdown(ctx context.Context) error {
	d.healthcheck.stop()
	d.helper.Stop()

	return nil
}

// HandleError is called by Kubelet when an error occures asyncronously, and
// needs to be communicated to the DRA driver.
//
// This is a mandatory method because drivers should check for errors
// which won't get resolved by retrying and then fail or change the
// slices that they are trying to publish:
// - dropped fields (see [resourceslice.DroppedFieldsError])
// - validation errors (see [apierrors.IsInvalid]).
func (d *driver) HandleError(ctx context.Context, err error, message string) {
	if errors.Is(err, kubeletplugin.ErrRecoverable) {
		// TODO: FIXME: error is ignored ATM, handle it properly.
		klog.FromContext(ctx).Error(err, "DRAPlugin encountered an error.")
	} else {
		klog.Errorf("Unrecoverable error: %v", err)
	}

	runtime.HandleErrorWithContext(ctx, err, message)
}

// NodeWatchResources implements the DRAResourceHealth gRPC service.
// It streams health status updates for all devices managed by this driver.
// The implementation logic is in healthcare.go.
func (d *driver) NodeWatchResources(
	req *drahealthv1alpha1.NodeWatchResourcesRequest,
	stream drahealthv1alpha1.DRAResourceHealth_NodeWatchResourcesServer,
) error {
	ctx := stream.Context()
	klog.Info("NodeWatchResources stream started")

	streamCh := make(chan *drahealthv1alpha1.NodeWatchResourcesResponse, 10)

	// Register the stream (implemented in healthcare.go).
	streamID := d.registerHealthStream(streamCh)
	defer d.unregisterHealthStream(streamID)

	// Send initial health status for all devices.
	if err := d.sendCurrentHealthStatus(ctx, stream); err != nil {
		klog.Errorf("Failed to send initial health status: %v", err)
		return err
	}

	// Keep the stream open and send updates.
	for {
		select {
		case <-ctx.Done():
			klog.Info("NodeWatchResources stream closed by client")
			return ctx.Err()
		case response, ok := <-streamCh:
			if !ok {
				klog.Info("Health stream channel closed")
				return nil
			}
			if err := stream.Send(response); err != nil {
				klog.Errorf("Failed to send health update: %v", err)
				return err
			}
			klog.V(5).Infof("Sent health update, devices: %d", len(response.Devices))
		}
	}
}

// watchDevices polls for GPU/DRI device changes and republishes ResourceSlices when needed.
func (d *driver) watchDevices(ctx context.Context) {
	klog.V(5).Info("Starting to watch for device changes (DRIVER=xe, DRIVER=i915)")

	filters := []map[string]string{
		{"DRIVER": "xe"},
		{"DRIVER": "i915"},
		{"DRIVER": "vfio-pci"},
		{"SUBSYSTEM": "pci"},
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
			if !d.shouldProcessUdevEvent(evt) {
				continue
			}
			d.refreshDeviceOnDriverEvent(ctx, evt)
		}
	}
}

// refreshDeviceOnDriverEvent updates allocatable GPU details after a driver bind/unbind event.
func (d *driver) refreshDeviceOnDriverEvent(ctx context.Context, evt *udev.Event) {
	klog.V(5).Infof("Refreshing devices after udev event: %+v", evt)

	pciAddress := getPCIAddressFromDevpath(evt.Devpath)

	deviceUID, err := d.state.getDeviceUIDFromPCIAddress(pciAddress)
	if err != nil {
		klog.Errorf("failed to get device UID from PCI address %s: %v", pciAddress, err)
	}

	// if the evt.Action == "unbind", set the current driver to empty string
	currentDriver := ""
	if evt.Action == "bind" {
		currentDriver = getDriverFromDevpath(d.state.SysfsRoot, evt.Devpath)
	}

	wasTaintedWithNoDRMBound := !d.state.IsDeviceDRMBound(deviceUID) && !d.state.IsDevicePrepared(deviceUID)
	shouldUntaintNoDRMBound := wasTaintedWithNoDRMBound &&
		(currentDriver == device.SysfsXeDriverName || currentDriver == device.SysfsI915DriverName)

	if err := d.state.RefreshDeviceOnDriverEvent(deviceUID, currentDriver); err != nil {
		klog.Errorf("Failed to refresh device on driver event: %v", err)
	}

	if !d.shouldPublishResourceSlice(evt.Action, deviceUID, shouldUntaintNoDRMBound) {
		klog.V(5).Infof("Skipping ResourceSlice publish for prepared unbind event, PCI address: %s", pciAddress)
		return
	}

	if err := d.PublishResourceSlice(ctx); err != nil {
		klog.Errorf("could not publish updated resource slice: %v", err)
	}
}

// shouldPublishResourceSlice determines whether a ResourceSlice update should be published based on the udev event action and device state.
// For "bind" events, it returns true if taints should be removed.
// For "unbind" events, it returns false if the device is prepared,
// otherwise true so that the device gets tainted while being published.
func (d *driver) shouldPublishResourceSlice(action, deviceUID string, shouldUntaintNoDRMBound bool) bool {
	if action == "bind" {
		return shouldUntaintNoDRMBound
	}

	return !d.state.IsDevicePrepared(deviceUID)
}

func (d *driver) shouldProcessUdevEvent(evt *udev.Event) bool {
	// we care only when the events are about these
	if evt.Action != "bind" && evt.Action != "unbind" {
		return false
	}
	klog.V(5).Infof("checking if the event is related to GPU devices: %s", evt)

	// in cases of i915 and xe drivers
	if evt.Properties["Driver"] == device.SysfsI915DriverName || evt.Properties["Driver"] == device.SysfsXeDriverName {
		// check only events related to "cardX" devices
		if isCardDevpath(evt.Devpath) {
			return true
		}
		return false
	}
	// in case of vfio-pci driver or unbind events
	return d.state.devpathContainsGPUPCIAddress(evt.Devpath)
}

func isCardDevpath(devpath string) bool {
	// Check if the base of the devpath starts with "card"
	base := filepath.Base(devpath)
	if !strings.HasPrefix(base, "card") {
		return false
	}

	// Check if the remaining part is a number (e.g., "card0", "card1", etc.)
	cardNum := strings.TrimPrefix(base, "card")
	_, err := strconv.Atoi(cardNum)
	return err == nil
}

// getDriverFromDevpath extracts the current kernel module driver bound to the PCI device
// from the udev devpath. For example, from devpath=/devices/pci0000:00/0000:00:02.0/drm/card1,
// it resolves <sysfsRoot>/devices/pci0000:00/0000:00:02.0/driver symlink to get the driver name.
func getDriverFromDevpath(sysfsRoot, devpath string) string {
	pciAddress := getPCIAddressFromDevpath(devpath)
	if pciAddress == "" {
		klog.V(5).Infof("No PCI address found in devpath: %s", devpath)
		return ""
	}

	parts := strings.Split(strings.Trim(devpath, "/"), "/")
	pciPartIdx := -1
	for idx, part := range parts {
		if part == pciAddress {
			pciPartIdx = idx
			break
		}
	}

	if pciPartIdx == -1 {
		klog.V(5).Infof("PCI path component not found in devpath: %s", devpath)
		return ""
	}

	// Take path up to end of PCI address: /devices/pci0000:00/0000:00:02.0
	pciDevPath := filepath.Join(parts[:pciPartIdx+1]...)

	// Build sysfs driver symlink path: <sysfsRoot>/devices/pci0000:00/0000:00:02.0/driver
	// The symlink target (e.g. ../../../bus/pci/drivers/xe) is resolved automatically.
	driverLinkPath := filepath.Join(sysfsRoot, pciDevPath, "driver")

	realPath, err := filepath.EvalSymlinks(driverLinkPath)
	if err != nil {
		klog.V(5).Infof("Failed to resolve driver symlink %s: %v", driverLinkPath, err)
		return ""
	}

	return filepath.Base(realPath)
}

func getPCIAddressFromDevpath(devpath string) string {
	parts := strings.Split(strings.Trim(devpath, "/"), "/")

	// Iterate in reverse to find the last PCI address
	for i := len(parts) - 1; i >= 0; i-- {
		if device.PciRegexp.MatchString(parts[i]) {
			return parts[i]
		}
	}

	return ""
}
