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

	resourcev1 "k8s.io/api/resource/v1alpha2"
	"k8s.io/klog/v2"
	drav1 "k8s.io/kubelet/pkg/apis/dra/v1alpha3"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
)

func (d *driver) NodeListAndWatchResources(req *drav1.NodeListAndWatchResourcesRequest, stream drav1.Node_NodeListAndWatchResourcesServer) error {
	klog.V(5).Info("NodeListAndWatchResources is called")

	if err := d.sendResourceModel(stream); err != nil {
		return err
	}

	select {
	case <-d.doneCh:
		return nil
	case <-d.updateCh:
		if err := d.sendResourceModel(stream); err != nil {
			return err
		}
	}

	return nil
}

func (d *driver) sendResourceModel(stream drav1.Node_NodeListAndWatchResourcesServer) error {
	model := d.state.getResourceModel()
	resp := &drav1.NodeListAndWatchResourcesResponse{
		Resources: []*resourcev1.ResourceModel{&model},
	}

	if err := stream.Send(resp); err != nil {
		return err
	}

	return nil
}

func (d *driver) nodePrepareStructuredResource(ctx context.Context, claim *drav1.Claim) *drav1.NodePrepareResourceResponse {
	klog.V(5).Infof("NodePrepareResource is called: request: %+v", claim)

	// FIXME: TODO: Add monitoring support, K8s 1.31 might have it in structured parameters,
	// for now rely on resource class.

	// TODO: bring back retry loop for GAS update when partitioning support is in place
	if _, found := d.state.prepared[claim.Uid]; found {
		klog.V(3).Infof("Claim %s was already prepared, nothing to do", claim.Uid)
		return d.cdiDevices(claim.Uid)
	}

	perClaimDevices := map[string][]*device.DeviceInfo{}
	claimDevices, err := d.structuredClaimDevices(claim)
	if err != nil {
		return &drav1.NodePrepareResourceResponse{Error: fmt.Sprintf("error preparing resource: %v", err)}
	}

	// add resource claim to prepared list
	perClaimDevices[claim.Uid] = claimDevices
	if err = d.state.makePreparedClaimAllocation(perClaimDevices); err != nil {
		return &drav1.NodePrepareResourceResponse{Error: fmt.Sprintf("failed creating prepared claim: %v", err)}
	}

	return d.cdiDevices(claim.Uid)
}

func (d *driver) structuredClaimDevices(claim *drav1.Claim) ([]*device.DeviceInfo, error) {
	allocatedDevices := []*device.DeviceInfo{}

	for _, handle := range claim.StructuredResourceHandle {
		for _, result := range handle.Results {
			deviceUID := result.AllocationResultModel.NamedResources.Name
			device, found := d.state.allocatable[deviceUID]
			if !found {
				return nil, fmt.Errorf("allocated device %v not found", deviceUID)
			}

			allocatedDevices = append(allocatedDevices, device)
		}
	}

	return allocatedDevices, nil
}
