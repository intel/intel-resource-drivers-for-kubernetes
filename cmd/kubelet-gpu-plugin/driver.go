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
	"sync"

	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/runtime"
	coreclientset "k8s.io/client-go/kubernetes"
	devicemetadata "k8s.io/dynamic-resource-allocation/api/metadata/v1alpha1"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
	"k8s.io/klog/v2"
	drahealthv1alpha1 "k8s.io/kubelet/pkg/apis/dra-health/v1alpha1"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/discovery"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/helpers"
	driverVersion "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/version"
)

type driver struct {
	client coreclientset.Interface
	state  *nodeState
	helper *kubeletplugin.Helper

	// Flag to stop XPUMD listener and prevent it from attempting to connect to XPUMD.
	stopXPUMDListener   bool
	ignoreHealthWarning bool // true if devices with health warnings should still be considered as healthy.

	// Health streaming support
	healthStreams      map[int]chan *drahealthv1alpha1.NodeWatchResourcesResponse
	healthStreamsMutex sync.RWMutex
	healthStreamID     int
	healthcheck        *healthcheck

	// Embed unimplemented server for forward compatibility
	drahealthv1alpha1.UnimplementedDRAResourceHealthServer
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
	sysfsRoot := helpers.GetSysfsRoot("bus/pci")
	klog.Infof("sysfs root: %v", sysfsRoot)
	klog.Infof("devfs root: %v", helpers.GetDevfsRoot("dri"))

	gpuFlags, err := getGPUFlags(config.DriverFlags)
	if err != nil {
		return nil, fmt.Errorf("get GPU flags: %w", err)
	}

	driver := &driver{
		client: config.Coreclient,
		state: &nodeState{
			PreparedClaimsFilePath: path.Join(config.CommonFlags.KubeletPluginDir, device.PreparedClaimsFileName),
			SysfsRoot:              sysfsRoot,
			NodeName:               config.CommonFlags.NodeName,
		},
		healthStreams:       make(map[int]chan *drahealthv1alpha1.NodeWatchResourcesResponse),
		ignoreHealthWarning: gpuFlags.IgnoreHealthWarning,
	}

	// If we run in privileged mode, device details can be obtained from devfs, otherwise XPUMD has
	// to supply the details after at some point later when it's up.
	detectedDevices := discovery.DiscoverDevices(driver.state.SysfsRoot, device.DefaultNamingStyle, gpuFlags.Healthcare)
	if len(detectedDevices) == 0 {
		klog.Warning("No supported devices detected on this node")
	}

	if !gpuFlags.Healthcare {
		klog.V(5).Info("Health monitoring is disabled")
	}

	klog.V(3).Info("Creating new NodeState")
	driver.state, err = newNodeState(detectedDevices, config.CommonFlags.CdiRoot, driver.state.PreparedClaimsFilePath, driver.state.SysfsRoot, driver.state.NodeName, gpuFlags.ManageBinding)
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
		kubeletplugin.EnableDeviceMetadata(true),
		kubeletplugin.MetadataVersions(devicemetadata.SchemeGroupVersion),
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

	if gpuFlags.Healthcare {
		// Enable monitoring health stream from xpumd 2.0+.
		klog.Info("Starting health monitoring")
		go driver.xpumdListen(ctx, gpuFlags.XPUMDSocketFilePath)

		// Start udev device events listener.
		go driver.watchDevices(ctx)
	}

	klog.V(3).Info("Finished creating new driver")
	return driver, nil
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

func (d *driver) PrepareResourceClaims(ctx context.Context, claims []*resourceapi.ResourceClaim) (map[types.UID]kubeletplugin.PrepareResult, error) {
	klog.V(5).Infof("PrepareResourceClaims is called: number of claims: %d", len(claims))
	response := map[types.UID]kubeletplugin.PrepareResult{}

	var needToPublishSlice bool

	for _, claim := range claims {
		var needToPublish bool
		needToPublish, response[claim.UID] = d.prepareResourceClaim(ctx, claim)
		if needToPublish {
			needToPublishSlice = true
		}
	}

	if needToPublishSlice {
		if err := d.PublishResourceSlice(ctx); err != nil {
			klog.Errorf("Failed to publish resource slice: %v", err)
		}
	}
	return response, nil
}

func (d *driver) prepareResourceClaim(ctx context.Context, claim *resourceapi.ResourceClaim) (bool, kubeletplugin.PrepareResult) {
	klog.V(5).Infof("prepareResourceClaim is called for claim %v", claim.UID)

	// TODO: check all devices anyway?
	if claimPreparation, found := d.state.Prepared[claim.UID]; found {
		klog.V(3).Infof("Claim %v was already prepared, nothing to do", claim.UID)
		return false, claimPreparation.PrepareResult()
	}

	return d.state.Prepare(ctx, claim)
}

func (d *driver) UnprepareResourceClaims(ctx context.Context, claims []kubeletplugin.NamespacedObject) (map[types.UID]error, error) {
	klog.V(5).Infof("UnprepareResourceClaims is called: number of claims: %d", len(claims))
	response := map[types.UID]error{}

	var needToPublishSlice bool

	for _, claim := range claims {
		needToPublish, err := d.state.Unprepare(ctx, claim.UID)
		if needToPublish {
			needToPublishSlice = true
		}
		if err != nil {
			response[claim.UID] = fmt.Errorf("could not unprepare resource: %v", err)
			klog.V(5).Infof("claim %v: %v", claim.UID, response[claim.UID])
			continue
		}
		response[claim.UID] = nil
	}

	if needToPublishSlice {
		if err := d.PublishResourceSlice(ctx); err != nil {
			klog.Errorf("Failed to publish resource slice: %v", err)
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
