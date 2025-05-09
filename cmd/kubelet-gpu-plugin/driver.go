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
	"fmt"
	"path"

	resourceapi "k8s.io/api/resource/v1beta1"
	"k8s.io/apimachinery/pkg/types"
	coreclientset "k8s.io/client-go/kubernetes"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
	"k8s.io/klog/v2"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/discovery"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/helpers"
	driverVersion "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/version"
)

type driver struct {
	client coreclientset.Interface
	state  *helpers.NodeState
	helper *kubeletplugin.Helper
}

func newDriver(ctx context.Context, config *helpers.Config) (helpers.Driver, error) {
	driverVersion.PrintDriverVersion(device.DriverName)

	driver := &driver{
		client: config.Coreclient,
		state: &helpers.NodeState{
			PreparedClaimsFilePath: path.Join(config.CommonFlags.KubeletPluginDir, device.PreparedClaimsFileName),
			SysfsRoot:              helpers.GetSysfsRoot(device.SysfsDRMpath),
			NodeName:               config.CommonFlags.NodeName,
		},
	}

	klog.V(5).Infof("Prepared claims: %v", driver.state)

	detectedDevices := discovery.DiscoverDevices(driver.state.SysfsRoot, device.DefaultNamingStyle)
	if len(detectedDevices) == 0 {
		klog.Info("No supported devices detected")
	}

	klog.V(3).Info("Creating new NodeState")
	var err error
	driver.state, err = newNodeState(detectedDevices, config.CommonFlags.CdiRoot, driver.state.PreparedClaimsFilePath, driver.state.SysfsRoot, driver.state.NodeName)
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

	state := nodeState{NodeState: driver.state}
	resources := state.GetResources()
	klog.FromContext(ctx).Info("Publishing resources", "len", len(resources.Pools[state.NodeName].Slices[0].Devices))
	klog.V(5).Infof("devices: %+v", resources.Pools[state.NodeName].Slices[0].Devices)
	if err := helper.PublishResources(ctx, resources); err != nil {
		return nil, fmt.Errorf("error publishing resources: %v", err)
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
	klog.V(5).Infof("NodePrepareResource is called: request: %+v", claim)

	if claimPreparation, found := d.state.Prepared[string(claim.UID)]; found {
		klog.V(3).Infof("Claim %v was already prepared, nothing to do", claim.UID)
		return claimPreparation
	}

	state := nodeState{d.state}
	if err := state.Prepare(ctx, claim); err != nil {
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
	d.helper.Stop()
	return nil
}
