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

	cdihelpers "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gaudi/cdihelpers"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gaudi/device"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gaudi/discovery"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/helpers"
	driverVersion "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/version"
)

// compile-time test for implementation conformance with the interface.
var _ drav1.DRAPluginServer = (*driver)(nil)

type driver struct {
	client coreclientset.Interface
	state  *helpers.NodeState
	plugin kubeletplugin.DRAPlugin
	// If HLML monitoring is running - it will need to be stopped.
	hlmlShutdown context.CancelFunc
}

func getGaudiFlags(someFlags interface{}) (*GaudiFlags, error) {
	gaudiFlags, OK := someFlags.(*GaudiFlags)
	if !OK {
		return &GaudiFlags{}, fmt.Errorf("could not parse driver flags as GaudiFlags")
	}

	klog.V(5).Infof("Gaudi parameters parsing OK: %+v", gaudiFlags)

	if gaudiFlags.HealthcareInterval < HealthcareIntervalFlagMin || gaudiFlags.HealthcareInterval > HealthcareIntervalFlagMax {
		return gaudiFlags, fmt.Errorf("unsupported health interval value %v. Should be [%v~%v]",
			gaudiFlags.HealthcareInterval, HealthcareIntervalFlagMin, HealthcareIntervalFlagMax)
	}

	return gaudiFlags, nil
}

func newDriver(ctx context.Context, config *helpers.Config) (helpers.Driver, error) {
	driverVersion.PrintDriverVersion(device.DriverName)
	sysfsDir := helpers.GetSysfsRoot(device.SysfsAccelPath)
	preparedClaimsFilePath := path.Join(config.CommonFlags.KubeletPluginDir, device.PreparedClaimsFileName)

	gaudiFlags, err := getGaudiFlags(config.DriverFlags)
	if err != nil {
		klog.Errorf("FATAL: %v", err)
		return nil, fmt.Errorf("FATAL: %v", err)
	}

	detectedDevices := discovery.DiscoverDevices(sysfsDir, device.DefaultNamingStyle)
	if len(detectedDevices) == 0 {
		klog.Info("No supported devices detected")
	}

	klog.V(3).Info("Creating new NodeState")
	state, err := newNodeState(detectedDevices, config.CommonFlags.CdiRoot, preparedClaimsFilePath, config.CommonFlags.NodeName)
	if err != nil {
		return nil, fmt.Errorf("failed to create new NodeState: %v", err)
	}

	driver := &driver{
		state:  state,
		client: config.Coreclient,
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
		kubeletplugin.KubeletPluginSocketPath(pluginSocket),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to start kubelet-plugin: %v", err)
	}

	driver.plugin = plugin

	// Init HLML healthcare to get details needed for health monitor.
	if gaudiFlags.Healthcare {
		if err := driver.initHLML(); err != nil {
			return nil, fmt.Errorf("failed to initialize HLML for health monitoring: %v", err)
		}
	}

	if err := driver.PublishResourceSlice(ctx); err != nil {
		return nil, fmt.Errorf("startup error: %v", err)
	}

	if gaudiFlags.Healthcare {
		// startHealthMonitor listens for unhealthy UIDs, has to run in a routine.
		hlmlListenerContext, hlmlListenerCancel := context.WithCancel(ctx)
		driver.hlmlShutdown = hlmlListenerCancel
		go driver.startHealthMonitor(hlmlListenerContext, gaudiFlags.HealthcareInterval)
	}

	klog.V(3).Info("Finished creating new driver")
	return driver, nil
}

func (d *driver) NodePrepareResources(ctx context.Context, req *drav1.NodePrepareResourcesRequest) (*drav1.NodePrepareResourcesResponse, error) {
	klog.V(5).Infof("NodePrepareResource is called: request: %+v", req)

	preparedResources := &drav1.NodePrepareResourcesResponse{Claims: map[string]*drav1.NodePrepareResourceResponse{}}

	for _, claim := range req.Claims {
		preparedResources.Claims[claim.UID] = d.nodePrepareResource(ctx, claim)
	}

	return preparedResources, nil
}

func (d *driver) nodePrepareResource(ctx context.Context, claim *drav1.Claim) *drav1.NodePrepareResourceResponse {
	klog.V(5).Infof("NodePrepareResource is called: request: %+v", claim)

	if claimPreparation, found := d.state.Prepared[claim.UID]; found {
		klog.V(3).Infof("Claim %s was already prepared, nothing to do", claim.UID)
		return &drav1.NodePrepareResourceResponse{
			Devices: claimPreparation,
		}
	}

	resourceClaim, err := d.client.ResourceV1beta1().ResourceClaims(claim.Namespace).Get(ctx, claim.Name, metav1.GetOptions{})
	if err != nil {
		return &drav1.NodePrepareResourceResponse{
			Error: fmt.Sprintf("could not find ResourceClaim %s in namespace %s: %v", claim.Name, claim.Namespace, err),
		}
	}

	state := nodeState{d.state}
	if err := state.Prepare(ctx, resourceClaim); err != nil {
		return &drav1.NodePrepareResourceResponse{
			Error: err.Error(),
		}
	}

	return &drav1.NodePrepareResourceResponse{Devices: d.state.Prepared[claim.UID]}
}

func (d *driver) NodeUnprepareResources(ctx context.Context, req *drav1.NodeUnprepareResourcesRequest) (*drav1.NodeUnprepareResourcesResponse, error) {
	klog.V(5).Infof("NodeUnprepareResource is called: number of claims: %d", len(req.Claims))
	unpreparedResources := &drav1.NodeUnprepareResourcesResponse{
		Claims: map[string]*drav1.NodeUnprepareResourceResponse{},
	}

	for _, claim := range req.Claims {
		unpreparedResources.Claims[claim.UID] = d.nodeUnprepareResource(ctx, claim)
	}

	return unpreparedResources, nil
}

func (d *driver) nodeUnprepareResource(ctx context.Context, claim *drav1.Claim) *drav1.NodeUnprepareResourceResponse {
	klog.V(3).Infof("NodeUnprepareResource is called: claim: %+v", claim)

	if err := d.state.Unprepare(ctx, claim.UID); err != nil {
		return &drav1.NodeUnprepareResourceResponse{Error: fmt.Sprintf("error freeing devices: %v", err)}
	}

	if err := cdihelpers.DeleteDeviceAndWrite(d.state.CdiCache, claim.UID); err != nil {
		return &drav1.NodeUnprepareResourceResponse{Error: fmt.Sprintf("error deleting CDI device: %v", err)}
	}

	klog.V(3).Infof("Freed devices for claim '%v'", claim.UID)
	return &drav1.NodeUnprepareResourceResponse{}
}

func (d *driver) PublishResourceSlice(ctx context.Context) error {
	state := nodeState{NodeState: d.state}
	resources := state.GetResources()
	klog.FromContext(ctx).Info("Publishing resources", "len", len(resources.Devices))
	klog.V(5).Infof("devices: %+v", resources.Devices)
	if err := d.plugin.PublishResources(ctx, resources); err != nil {
		return fmt.Errorf("error publishing resources: %v", err)
	}

	return nil
}
