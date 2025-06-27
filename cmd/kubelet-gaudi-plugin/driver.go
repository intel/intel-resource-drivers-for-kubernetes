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

	cdihelpers "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gaudi/cdihelpers"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gaudi/device"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gaudi/discovery"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/helpers"
	driverVersion "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/version"
)

type driver struct {
	client coreclientset.Interface
	state  *helpers.NodeState
	helper *kubeletplugin.Helper
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

func (d *driver) PrepareResourceClaims(ctx context.Context, claims []*resourceapi.ResourceClaim) (map[types.UID]kubeletplugin.PrepareResult, error) {
	klog.V(5).Infof("NodePrepareResource is called: request: %+v", claims)

	response := map[types.UID]kubeletplugin.PrepareResult{}

	for _, claim := range claims {
		response[claim.UID] = d.prepareResourceClaim(ctx, claim)
	}

	return response, nil
}

func (d *driver) prepareResourceClaim(ctx context.Context, claim *resourceapi.ResourceClaim) kubeletplugin.PrepareResult {
	klog.V(5).Infof("NodePrepareResource is called: request: %+v", claim)

	if claimPreparation, found := d.state.Prepared[string(claim.UID)]; found {
		klog.V(3).Infof("Claim %s was already prepared, nothing to do", claim.UID)
		return claimPreparation
	}

	state := nodeState{d.state}
	if err := state.Prepare(ctx, claim); err != nil {
		return kubeletplugin.PrepareResult{
			Err: err,
		}
	}

	return d.state.Prepared[string(claim.UID)]
}

func (d *driver) UnprepareResourceClaims(ctx context.Context, claims []kubeletplugin.NamespacedObject) (map[types.UID]error, error) {
	klog.V(5).Infof("NodeUnprepareResource is called: number of claims: %d", len(claims))
	response := map[types.UID]error{}

	for _, claim := range claims {

		if err := d.state.Unprepare(ctx, string(claim.UID)); err != nil {
			response[claim.UID] = fmt.Errorf("error freeing devices: %v", err)
			continue
		}

		if err := cdihelpers.DeleteDeviceAndWrite(d.state.CdiCache, string(claim.UID)); err != nil {
			response[claim.UID] = fmt.Errorf("error deleting CDI device: %v", err)
			continue
		}

		response[claim.UID] = nil
		klog.V(3).Infof("Freed devices for claim '%v'", claim.UID)

	}

	return response, nil
}

func (d *driver) PublishResourceSlice(ctx context.Context) error {
	state := nodeState{NodeState: d.state}
	resources := state.GetResources()
	klog.FromContext(ctx).Info("Publishing resources", "len", len(resources.Pools[state.NodeName].Slices[0].Devices))
	klog.V(5).Infof("devices: %+v", resources.Pools[state.NodeName].Slices[0].Devices)
	if err := d.helper.PublishResources(ctx, resources); err != nil {
		return fmt.Errorf("error publishing resources: %v", err)
	}

	return nil
}
