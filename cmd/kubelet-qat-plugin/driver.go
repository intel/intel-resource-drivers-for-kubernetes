//
// Copyright (C) 2024-2025 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0
//

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
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
	"k8s.io/klog/v2"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/helpers"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/qat/device"
	driverVersion "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/version"
)

type driver struct {
	sync.Mutex
	client coreclientset.Interface
	state  nodeState
	helper *kubeletplugin.Helper
}

func (d *driver) PrepareResourceClaims(ctx context.Context, claims []*resourceapi.ResourceClaim) (map[types.UID]kubeletplugin.PrepareResult, error) {
	klog.V(5).Infof("NodePrepareResource is called: number of claims: %d", len(claims))

	response := map[types.UID]kubeletplugin.PrepareResult{}

	for _, claim := range claims {
		klog.V(5).Infof("NodePrepareResources: claim %s", claim.UID)
		response[claim.UID] = d.prepareResourceClaim(ctx, claim)
	}

	return response, nil
}

func (d *driver) prepareResourceClaim(ctx context.Context, claim *resourceapi.ResourceClaim) kubeletplugin.PrepareResult {
	klog.V(5).Infof("prepareResourceClaim is called for claim %v", claim.UID)
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
	klog.V(5).Infof("UnprepareResourceClaims is called: number of claims: %d", len(claims))
	response := map[types.UID]error{}

	var updateFound bool
	for _, claim := range claims {
		var updated bool
		var err error
		if updated, err = d.state.Unprepare(ctx, claim); err != nil {
			response[claim.UID] = fmt.Errorf("error freeing devices: %v", err)
			continue
		}
		updateFound = updateFound || updated

		response[claim.UID] = nil
		klog.V(3).Infof("Freed devices for claim '%v'", claim.UID)
	}

	if updateFound {
		if err := d.PublishResourceSlice(ctx); err != nil {
			klog.Errorf("could not publish updated resource slice: %v", err)
		}
	}

	return response, nil
}

func (d *driver) PublishResourceSlice(ctx context.Context) error {
	resources := d.state.GetResources()
	klog.FromContext(ctx).Info("Publishing resources", "len", len(resources.Pools[d.state.NodeName].Slices[0].Devices))
	if err := d.helper.PublishResources(ctx, resources); err != nil {
		return fmt.Errorf("error publishing resources: %v", err)
	}
	return nil
}

func newDriver(ctx context.Context, config *helpers.Config) (helpers.Driver, error) {
	driverVersion.PrintDriverVersion(device.DriverName)
	preparedClaimsFilePath := path.Join(config.CommonFlags.KubeletPluginDir, device.PreparedClaimsFileName)

	pfdevices, err := device.New()
	if err != nil {
		return nil, fmt.Errorf("could not find PF devices: %v", err)
	}

	for _, pf := range pfdevices {
		if err := pf.EnableVFs(); err != nil {
			return nil, fmt.Errorf("cannot enable PF device '%s': %v", pf.Device, err)
		}
	}
	if err := getDefaultConfiguration(config.CommonFlags.NodeName, pfdevices); err != nil {
		klog.Warningf("Cannot apply default configuration: %vn", err)
	}

	detectedVFDevices := device.GetCDIDevices(pfdevices)

	state, err := newNodeState(detectedVFDevices, config.CommonFlags.CdiRoot, preparedClaimsFilePath, config.CommonFlags.NodeName)
	if err != nil {
		return nil, fmt.Errorf("failed to create new NodeState: %v", err)
	}

	driver := &driver{
		state:  *state,
		client: config.Coreclient,
	}

	klog.Infof(`Starting DRA resource-driver kubelet-plugin
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

	if err := driver.PublishResourceSlice(ctx); err != nil {
		return nil, fmt.Errorf("could not publish ResourceSlice: %v", err)
	}

	klog.V(3).Info("Finished creating new driver")
	return driver, nil
}

func (d *driver) Shutdown(ctx context.Context) error {
	klog.V(5).Info("Shutting down driver")

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
		klog.FromContext(ctx).Error(err, "Unrecoverable error.")
	}

	runtime.HandleErrorWithContext(ctx, err, message)
}
