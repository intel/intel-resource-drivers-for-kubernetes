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

	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	drav1 "k8s.io/kubelet/pkg/apis/dra/v1alpha3"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gaudi/device"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gaudi/discovery"
	intelcrd "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gaudi/v1alpha1/api"
	driverVersion "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/version"
)

// compile-time test for implementation conformance with the interface.
var _ drav1.NodeServer = (*driver)(nil)

type driver struct {
	gas      *intelcrd.GaudiAllocationState
	state    *nodeState
	sysfsDir string
}

func newDriver(ctx context.Context, config *configType) (*driver, error) {
	var state *nodeState

	driverVersion.PrintDriverVersion(intelcrd.APIGroupName, intelcrd.APIVersion)

	sysfsDir := device.GetSysfsRoot()
	gas := intelcrd.NewGaudiAllocationState(config.crdconfig, config.clientset.intel)

	preparedClaimsFilePath := path.Join(config.driverPluginPath, "preparedClaims.json")

	setupErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		klog.V(3).Info("Creating new GaudiAllocationState")
		err := gas.GetOrCreate(ctx)
		if err != nil {
			return fmt.Errorf("failed to get GaudiAllocationState: %v", err)
		}

		klog.V(3).Info("Setting GaudiAllocationState as NotReady")
		err = gas.UpdateStatus(ctx, intelcrd.GaudiAllocationStateStatusNotReady)
		if err != nil {
			return fmt.Errorf("failed to set GaudiAllocationState as NotReady: %v", err)
		}

		detectedDevices := discovery.DiscoverDevices(sysfsDir)
		if len(detectedDevices) == 0 {
			klog.Info("No supported devices detected")
		}

		klog.V(3).Info("Creating new NodeState")
		state, err = newNodeState(gas, detectedDevices, config.cdiRoot, preparedClaimsFilePath)
		if err != nil {
			return fmt.Errorf("failed to create new NodeState: %v", err)
		}

		klog.V(3).Info("Updating GaudiAllocationState with detected devices")
		err = gas.Update(ctx, state.GetUpdatedSpec(&gas.Spec))
		if err != nil {
			return fmt.Errorf("failed to update GaudiAllocationState: %v", err)
		}

		klog.V(3).Info("Setting GaudiAllocationState status as Ready")
		return gas.UpdateStatus(ctx, intelcrd.GaudiAllocationStateStatusReady)
	})
	if setupErr != nil {
		return nil, fmt.Errorf("creating driver: %v", setupErr)
	}

	d := &driver{
		gas:      gas,
		state:    state,
		sysfsDir: sysfsDir,
	}
	klog.V(3).Info("Finished creating new driver")

	return d, nil
}

func (d *driver) NodePrepareResources(ctx context.Context, req *drav1.NodePrepareResourcesRequest) (*drav1.NodePrepareResourcesResponse, error) {
	klog.V(5).Infof("NodePrepareResource is called: request: %+v", req)

	preparedResources := &drav1.NodePrepareResourcesResponse{Claims: map[string]*drav1.NodePrepareResourceResponse{}}

	for _, claim := range req.Claims {
		preparedResources.Claims[claim.Uid] = d.nodePrepareResources(ctx, claim)
	}

	return preparedResources, nil
}

func (d *driver) nodePrepareResources(
	ctx context.Context, claim *drav1.Claim) *drav1.NodePrepareResourceResponse {
	klog.V(5).Infof("NodePrepareResource is called: request: %+v", claim)

	var cdinames []string

	// provide all devices for monitoring claims
	if claim.ResourceHandle == intelcrd.MonitorAllocType {
		cdinames = d.state.getMonitorCDINames(claim.Uid)
		klog.V(3).Infof("Prepared devices for monitor claim '%v': %s", claim.Uid, cdinames)
		return &drav1.NodePrepareResourceResponse{CDIDevices: cdinames}
	}

	if _, found := d.state.prepared[claim.Uid]; found {
		klog.V(3).Infof("Claim %s was already prepared, nothing to do", claim.Uid)
		return d.cdiDevices(claim.Uid)
	}

	err := d.gas.Get(ctx)
	if err != nil {
		return &drav1.NodePrepareResourceResponse{Error: fmt.Sprintf("failed to get GaudiAllocationState: %v", err)}
	}

	claimDevices, err := d.sanitizeClaimDevices(claim.Uid)
	if err != nil {
		return &drav1.NodePrepareResourceResponse{Error: fmt.Sprintf("failed validating devices to prepare: %v", err)}
	}

	// add resource claim to prepared list
	err = d.state.makePreparedClaimAllocation(claim.Uid, claimDevices)
	if err != nil {
		return &drav1.NodePrepareResourceResponse{Error: fmt.Sprintf("failed creating prepared claim allocation: %v", err)}
	}

	return d.cdiDevices(claim.Uid)
}

func (d *driver) cdiDevices(claimUID string) *drav1.NodePrepareResourceResponse {

	cdinames := d.state.GetAllocatedCDINames(claimUID)
	if len(cdinames) == 0 {
		klog.Errorf("could not find CDI device name from CDI registry for claim %s", claimUID)
		return &drav1.NodePrepareResourceResponse{Error: "error preparing resource: CDI devices not found in specs"}
	}

	klog.V(3).Infof("Prepared devices for claim '%v': %s", claimUID, cdinames)
	return &drav1.NodePrepareResourceResponse{CDIDevices: cdinames}
}

func (d *driver) NodeUnprepareResources(ctx context.Context, req *drav1.NodeUnprepareResourcesRequest) (*drav1.NodeUnprepareResourcesResponse, error) {
	klog.V(5).Infof("NodeUnprepareResource is called: number of claims: %d", len(req.Claims))
	unpreparedResources := &drav1.NodeUnprepareResourcesResponse{
		Claims: map[string]*drav1.NodeUnprepareResourceResponse{},
	}

	for _, claim := range req.Claims {
		unpreparedResources.Claims[claim.Uid] = d.nodeUnprepareResource(ctx, claim)
	}

	return unpreparedResources, nil
}

func (d *driver) nodeUnprepareResource(ctx context.Context, claim *drav1.Claim) *drav1.NodeUnprepareResourceResponse {
	klog.V(3).Infof("NodeUnprepareResource is called: claim: %+v", claim)

	// no-op for monitoring claims
	if claim.ResourceHandle == intelcrd.MonitorAllocType {
		klog.V(3).Infof("Freed devices for monitor claim '%v'", claim.Uid)
		return &drav1.NodeUnprepareResourceResponse{}
	}

	err := d.state.FreeClaimDevices(claim.Uid)
	if err != nil {
		return &drav1.NodeUnprepareResourceResponse{Error: fmt.Sprintf("error freeing devices: %v", err)}
	}

	klog.V(3).Infof("Freed devices for claim '%v'", claim.Uid)
	return &drav1.NodeUnprepareResourceResponse{}
}

// sanitizeClaimDevices returns a slice of allocated devices after sanitizing or an error
// in case sanitization failed.
func (d *driver) sanitizeClaimDevices(claimUID string) ([]*device.DeviceInfo, error) {
	claimDevices := []*device.DeviceInfo{}

	claimAllocation, found := d.gas.Spec.AllocatedClaims[claimUID]
	if !found {
		return nil, fmt.Errorf("no allocation found for claim %v in API", claimUID)
	}

	for _, gaudi := range claimAllocation.Devices {
		if _, found := d.gas.Spec.AllocatableDevices[gaudi.UID]; !found {
			return nil, fmt.Errorf("allocated device %v not found in API", gaudi.UID)
		}

		if _, found := d.state.allocatable[gaudi.UID]; !found {
			return nil, fmt.Errorf("allocated device %v not found locally", gaudi.UID)
		}

		if _, tainted := d.gas.Spec.TaintedDevices[gaudi.UID]; tainted {
			return nil, fmt.Errorf("allocated device %v is tainted and cannot be used", gaudi.UID)
		}

		claimDevices = append(claimDevices, d.state.allocatable[gaudi.UID])
	}

	return claimDevices, nil
}
