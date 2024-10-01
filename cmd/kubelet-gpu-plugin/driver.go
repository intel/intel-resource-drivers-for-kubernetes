/*
 * Copyright (c) 2024, Intel Corporation.  All Rights Reserved.
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

	drav1 "k8s.io/kubelet/pkg/apis/dra/v1alpha4"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/discovery"
	driverVersion "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/version"
)

// compile-time test for implementation conformance with the interface.
var _ drav1.NodeServer = (*driver)(nil)

type driver struct {
	client coreclientset.Interface
	state  *nodeState
	plugin kubeletplugin.DRAPlugin
}

func newDriver(ctx context.Context, config *configType) (*driver, error) {
	driverVersion.PrintDriverVersion(device.DriverName)
	sysfsRoot := device.GetSysfsRoot()
	preparedClaimFilePath := path.Join(config.kubeletPluginDir, device.PreparedClaimsFileName)
	klog.V(5).Infof("Prepared claims: %v", preparedClaimFilePath)

	detectedDevices := discovery.DiscoverDevices(sysfsRoot, device.DefaultNamingStyle)
	if len(detectedDevices) == 0 {
		klog.Info("No supported devices detected")
	}

	klog.V(3).Info("Creating new NodeState")
	state, err := newNodeState(detectedDevices, config.cdiRoot, preparedClaimFilePath, sysfsRoot, config.nodeName)
	if err != nil {
		return nil, fmt.Errorf("failed to create new NodeState: %v", err)
	}

	d := &driver{
		state:  state,
		client: config.clientset,
	}

	registrarSocket := path.Join(config.kubeletPluginsRegistryDir, device.PluginRegistrarFileName)
	pluginSocket := path.Join(config.kubeletPluginDir, device.PluginSocketFileName)
	klog.Infof(`Starting DRA resource-driver kubelet-plugin
RegistrarSocketPath: %v
PluginSocketPath: %v
KubeletPluginSocketPath: %v`,
		registrarSocket,
		pluginSocket,
		pluginSocket)

	plugin, err := kubeletplugin.Start(
		ctx,
		d,
		kubeletplugin.KubeClient(config.clientset),
		kubeletplugin.NodeName(config.nodeName),
		kubeletplugin.DriverName(device.DriverName),
		kubeletplugin.RegistrarSocketPath(registrarSocket),
		kubeletplugin.PluginSocketPath(pluginSocket),
		kubeletplugin.KubeletPluginSocketPath(pluginSocket))
	if err != nil {
		return nil, fmt.Errorf("failed to start kubelet-plugin: %v", err)
	}

	d.plugin = plugin

	resources := d.state.GetResources()
	klog.FromContext(ctx).Info("Publishing resources", "len", len(resources.Devices))
	klog.V(5).Infof("devices: %+v", resources.Devices)
	plugin.PublishResources(ctx, resources)

	klog.V(3).Info("Finished creating new driver")
	return d, nil
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

	if claimPreparation, found := d.state.prepared[claimMetadata.UID]; found {
		klog.V(3).Infof("Claim %s was already prepared, nothing to do", claimMetadata.UID)
		return &drav1.NodePrepareResourceResponse{
			Devices: claimPreparation,
		}
	}

	claim, err := d.client.ResourceV1alpha3().ResourceClaims(claimMetadata.Namespace).Get(ctx, claimMetadata.Name, metav1.GetOptions{})
	if err != nil {
		return &drav1.NodePrepareResourceResponse{
			Error: fmt.Sprintf("could not find ResourceClaim %s in namespace %s: %v", claimMetadata.Name, claimMetadata.Namespace, err),
		}
	}

	if err := d.state.Prepare(ctx, claim); err != nil {
		return &drav1.NodePrepareResourceResponse{
			Error: fmt.Sprintf("error preparing devices for claim %v: %v", claimMetadata.UID, err),
		}
	}

	return &drav1.NodePrepareResourceResponse{Devices: d.state.prepared[claimMetadata.UID]}
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
