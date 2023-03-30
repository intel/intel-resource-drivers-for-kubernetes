/*
 * Copyright (c) 2023, Intel Corporation.  All Rights Reserved.
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

	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	drapbv1 "k8s.io/kubelet/pkg/apis/dra/v1alpha1"

	intelcrd "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/crd/intel/v1alpha/api"
)

const bytesInMB = 1048576

type driver struct {
	gas   *intelcrd.GpuAllocationState
	state *nodeState
}

func NewDriver(config *configType) (*driver, error) {
	gas := intelcrd.NewGpuAllocationState(config.crdconfig, config.clientset.intel)

	klog.V(3).Info("Creating new GpuAllocationState")
	err := gas.GetOrCreate()
	if err != nil {
		return nil, fmt.Errorf("failed to get GAS: %v", err)
	}

	klog.V(3).Info("Updating GpuAllocationState status")
	err = gas.UpdateStatus(intelcrd.GpuAllocationStateStatusNotReady)
	if err != nil {
		return nil, fmt.Errorf("failed to update GAS status: %v", err)
	}

	klog.V(3).Info("Creating new DeviceState")
	state, err := newNodeState(gas)
	if err != nil {
		return nil, fmt.Errorf("failed to create new NodeState: %v", err)
	}

	klog.V(3).Info("Updating GpuAllocationState with detected GPUs")
	err = gas.Update(state.getUpdatedSpec(&gas.Spec))
	if err != nil {
		return nil, fmt.Errorf("failed to update GAS: %v", err)
	}

	klog.V(3).Info("Updating GpuAllocationState status")
	err = gas.UpdateStatus(intelcrd.GpuAllocationStateStatusReady)
	if err != nil {
		return nil, fmt.Errorf("failed to update GAS status: %v", err)
	}

	d := &driver{
		gas:   gas,
		state: state,
	}
	klog.V(3).Info("Finished creating new driver")

	return d, nil
}

func (d *driver) NodePrepareResource(
	ctx context.Context, req *drapbv1.NodePrepareResourceRequest) (*drapbv1.NodePrepareResourceResponse, error) {
	klog.V(5).Infof("NodePrepareResource is called: request: %+v", req)

	var err error
	var cdinames []string
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		err = d.gas.Get()
		if err != nil {
			return fmt.Errorf("failed to get GAS: %v", err)
		}
		klog.V(5).Info("GAS get OK")

		// map[string:parent_uid][string:vf_uid]vfInfo
		toProvision := map[string]map[string]*DeviceInfo{}

		// map VFs in req context that need provisioning against parent uids
		for _, device := range d.gas.Spec.ResourceClaimAllocations[req.ClaimUid].Gpus {
			if device.Type == intelcrd.VfDeviceType {
				if _, exists := d.state.allocatable[device.UID]; !exists {
					klog.Infof("VF %v is not provisioned", device.UID)
					if _, parentInPlanned := toProvision[device.ParentUID]; !parentInPlanned {
						toProvision[device.ParentUID] = map[string]*DeviceInfo{}
					}
					toProvision[device.ParentUID][device.UID] = d.state.DeviceInfoFromAllocated(device)
				} else {
					klog.V(5).Infof("VF %v is already provisioned", device.UID)
				}
			}
		}

		if len(toProvision) != 0 {
			klog.V(5).Infof("Need to provision VFs on %d GPUs", len(toProvision))
			d.PickupMoreVFs(req.ClaimUid, toProvision)

			provisionedVFs, err2 := d.provisionVFs(toProvision)
			if err2 != nil {
				klog.Errorf("Could not prepare resource: %v", err2)
				return err2
			}

			// add to CDI registry and d.allocatable
			err = d.state.announceNewVFs(provisionedVFs)
			if err != nil {
				return err
			}
		}

		// add resource claim to prepared list
		err = d.state.makePreparedClaimAllocation(req.ClaimUid, d.gas.Spec.ResourceClaimAllocations[req.ClaimUid])
		if err != nil {
			return fmt.Errorf("Failed creating prepared claim allocation: %v", err)
		}

		err = d.gas.Update(d.state.getUpdatedSpec(&d.gas.Spec))
		if err != nil {
			return fmt.Errorf("Failed to update GAS: %v", err)
		}

		// CDI devices names
		cdinames = d.state.GetAllocatedCDINames(req.ClaimUid)

		if len(cdinames) == 0 {
			return fmt.Errorf("Could not find CDI device name from CDI registry")
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("error preparing resource: %v", err)
	}

	klog.V(3).Infof("Prepared devices for claim '%v': %s", req.ClaimUid, cdinames)
	return &drapbv1.NodePrepareResourceResponse{CdiDevices: cdinames}, nil
}

func (d *driver) NodeUnprepareResource(
	ctx context.Context, req *drapbv1.NodeUnprepareResourceRequest) (*drapbv1.NodeUnprepareResourceResponse, error) {
	klog.V(3).Infof("NodeUnprepareResource is called: request: %+v", req)

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		err := d.gas.Get()
		if err != nil {
			return fmt.Errorf("error freeing devices for claim '%v': %v", req.ClaimUid, err)
		}

		err = d.state.FreeClaimDevices(req.ClaimUid, &d.gas.Spec)
		if err != nil {
			return fmt.Errorf("error freeing devices for claim '%v': %v", req.ClaimUid, err)
		}

		err = d.gas.Update(d.state.getUpdatedSpec(&d.gas.Spec))
		if err != nil {
			return fmt.Errorf("failed to update GAS: %v", err)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("error unpreparing resource: %v", err)
	}

	klog.V(3).Infof("Freed devices for claim '%v'", req.ClaimUid)
	return &drapbv1.NodeUnprepareResourceResponse{}, nil
}

// Add VFs from other resourceClaimAllocation that need provisioning together with current claim.
func (d *driver) PickupMoreVFs(currentClaimUID string, toProvision map[string]map[string]*DeviceInfo) {
	for claimUID, ca := range d.gas.Spec.ResourceClaimAllocations {
		if claimUID == currentClaimUID {
			continue
		}
		for _, device := range ca.Gpus {
			if device.Type == intelcrd.VfDeviceType {
				_, vfExists := d.state.allocatable[device.UID]
				_, affectedParent := toProvision[device.ParentUID]
				if !vfExists && affectedParent {
					klog.V(5).Infof("Picking VF %v for claim %v to be provisioned (was not requested yet)",
						device.UID,
						claimUID)
					toProvision[device.ParentUID][device.UID] = d.state.DeviceInfoFromAllocated(device)
				}
			}
		}
	}
}
