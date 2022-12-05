/*
 * Copyright (c) 2022, Intel Corporation.  All Rights Reserved.
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

type driver struct {
	gas   *intelcrd.GpuAllocationState
	state *nodeState
}

func NewDriver(config *config_t) (*driver, error) {
	gas := intelcrd.NewGpuAllocationState(config.crdconfig, config.clientset.intel)

	klog.V(3).Info("Creating new GpuAllocationState")
	err := gas.GetOrCreate()
	if err != nil {
		return nil, err
	}

	klog.V(3).Info("Creating new DeviceState")
	state, err := newNodeState(gas)
	if err != nil {
		return nil, err
	}

	klog.V(3).Info("Updating GpuAllocationState")
	err = gas.Update(state.getUpdatedSpec(&gas.Spec))
	if err != nil {
		return nil, err
	}

	klog.V(3).Info("Updating GpuAllocationState status")
	err = gas.UpdateStatus(intelcrd.GpuAllocationStateStatusReady)
	if err != nil {
		return nil, err
	}

	d := &driver{
		gas:   gas,
		state: state,
	}
	klog.V(3).Info("Finished creating new driver")

	return d, nil
}

func (d *driver) NodePrepareResource(ctx context.Context, req *drapbv1.NodePrepareResourceRequest) (*drapbv1.NodePrepareResourceResponse, error) {
	klog.V(5).Infof("NodePrepareResource is called: request: %+v", req)

	var err error
	var cdinames []string
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		err = d.gas.Get()
		if err != nil {
			return err
		}
		klog.V(5).Info("GAS get OK")

		err = d.state.syncAllocatedDevicesFromGASSpec(&d.gas.Spec)
		if err != nil {
			return err
		}

		// TODO: sr-iov handling

		// CDI devices names
		cdinames = d.state.getAllocatedAsCDIDevices(req.ClaimUid)
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("error preparing resource: %v", err)
	}

	klog.V(3).Infof("Prepared devices for claim '%v': %s", req.ClaimUid, cdinames)
	return &drapbv1.NodePrepareResourceResponse{CdiDevices: cdinames}, nil
}

func (d *driver) NodeUnprepareResource(ctx context.Context, req *drapbv1.NodeUnprepareResourceRequest) (*drapbv1.NodeUnprepareResourceResponse, error) {
	klog.V(3).Infof("NodeUnprepareResource is called: request: %+v", req)

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		err := d.Free(req.ClaimUid)
		if err != nil {
			return fmt.Errorf("error freeing devices for claim '%v': %v", req.ClaimUid, err)
		}

		err = d.gas.Update(d.state.getUpdatedSpec(&d.gas.Spec))
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("error unpreparing resource: %v", err)
	}

	klog.V(3).Infof("Freed devices for claim '%v'", req.ClaimUid)
	return &drapbv1.NodeUnprepareResourceResponse{}, nil
}

func (d *driver) Free(claimUid string) error {
	err := d.gas.Get()
	if err != nil {
		return err
	}
	err = d.state.free(claimUid)
	if err != nil {
		return err
	}
	return nil
}
