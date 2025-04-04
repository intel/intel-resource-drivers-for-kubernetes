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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	coreclientset "k8s.io/client-go/kubernetes"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
	"k8s.io/klog/v2"
	drav1 "k8s.io/kubelet/pkg/apis/dra/v1beta1"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/discovery"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/helpers"
	driverVersion "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/version"
)

// compile-time test for implementation conformance with the interface.
var _ drav1.DRAPluginServer = (*driver)(nil)

type driver struct {
	client coreclientset.Interface
	state  *helpers.NodeState
	plugin kubeletplugin.DRAPlugin
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

	registrarSocket := path.Join(config.CommonFlags.KubeletPluginsRegistryDir, device.PluginRegistrarFileName)
	pluginSocket := path.Join(config.CommonFlags.KubeletPluginDir, device.PluginSocketFileName)
	klog.Infof(`Starting DRA resource-driver kubelet-plugin
RegistrarSocketPath: %v
PluginSocketPath: %v
KubeletPluginSocketPath: %v`,
		registrarSocket,
		pluginSocket,
		pluginSocket)

	plugin, err := kubeletplugin.Start(
		ctx,
		[]any{driver},
		kubeletplugin.KubeClient(config.Coreclient),
		kubeletplugin.NodeName(config.CommonFlags.NodeName),
		kubeletplugin.DriverName(device.DriverName),
		kubeletplugin.RegistrarSocketPath(registrarSocket),
		kubeletplugin.PluginSocketPath(pluginSocket),
		kubeletplugin.KubeletPluginSocketPath(pluginSocket))
	if err != nil {
		return nil, fmt.Errorf("failed to start kubelet-plugin: %v", err)
	}

	driver.plugin = plugin

	state := nodeState{NodeState: driver.state}
	resources := state.GetResources()
	klog.FromContext(ctx).Info("Publishing resources", "len", len(resources.Devices))
	klog.V(5).Infof("devices: %+v", resources.Devices)
	if err := plugin.PublishResources(ctx, resources); err != nil {
		return nil, fmt.Errorf("error publishing resources: %v", err)
	}
	klog.V(3).Info("Finished creating new driver")

	return driver, nil
}

func (d *driver) NodePrepareResources(ctx context.Context, req *drav1.NodePrepareResourcesRequest) (*drav1.NodePrepareResourcesResponse, error) {
	klog.V(5).Infof("NodePrepareResource is called: request: %+v", req)

	preparedResources := &drav1.NodePrepareResourcesResponse{Claims: map[string]*drav1.NodePrepareResourceResponse{}}

	for _, claim := range req.Claims {
		preparedResources.Claims[claim.UID] = d.nodePrepareResources(ctx, claim)
	}

	return preparedResources, nil
}

func (d *driver) nodePrepareResources(ctx context.Context, claimMetadata *drav1.Claim) *drav1.NodePrepareResourceResponse {
	klog.V(5).Infof("NodePrepareResource is called: request: %+v", claimMetadata)

	if claimPreparation, found := d.state.Prepared[claimMetadata.UID]; found {
		klog.V(3).Infof("Claim %s was already prepared, nothing to do", claimMetadata.UID)
		return &drav1.NodePrepareResourceResponse{
			Devices: claimPreparation,
		}
	}

	claim, err := d.client.ResourceV1beta1().ResourceClaims(claimMetadata.Namespace).Get(ctx, claimMetadata.Name, metav1.GetOptions{})
	if err != nil {
		return &drav1.NodePrepareResourceResponse{
			Error: fmt.Sprintf("could not find ResourceClaim %s in namespace %s: %v", claimMetadata.Name, claimMetadata.Namespace, err),
		}
	}

	state := nodeState{d.state}
	if err := state.Prepare(ctx, claim); err != nil {
		return &drav1.NodePrepareResourceResponse{
			Error: fmt.Sprintf("error preparing devices for claim %v: %v", claimMetadata.UID, err),
		}
	}

	return &drav1.NodePrepareResourceResponse{Devices: d.state.Prepared[claimMetadata.UID]}
}

func (d *driver) NodeUnprepareResources(ctx context.Context, req *drav1.NodeUnprepareResourcesRequest) (*drav1.NodeUnprepareResourcesResponse, error) {
	klog.V(5).Infof("NodeUnprepareResource is called: number of claims: %d", len(req.Claims))
	unpreparedResources := &drav1.NodeUnprepareResourcesResponse{
		Claims: map[string]*drav1.NodeUnprepareResourceResponse{},
	}

	for _, claim := range req.Claims {
		result := &drav1.NodeUnprepareResourceResponse{}
		if err := d.state.Unprepare(ctx, claim.UID); err != nil {
			result.Error = fmt.Sprintf("could not unprepare resource: %v", err)
		}

		unpreparedResources.Claims[claim.UID] = result
	}

	return unpreparedResources, nil
}

func (d *driver) Shutdown(ctx context.Context) error {
	d.plugin.Stop()
	return nil
}
