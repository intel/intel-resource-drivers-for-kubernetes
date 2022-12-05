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

package api

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	intelclientset "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/crd/intel/clientset/versioned"
	intelcrd "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/crd/intel/v1alpha"
)

const (
	GpuAllocationStateStatusReady    = "Ready"
	GpuAllocationStateStatusNotReady = "NotReady"
)

type GpuAllocationStateConfig struct {
	Name      string
	Namespace string
	Owner     *metav1.OwnerReference
}

type AllocatableGpu = intelcrd.AllocatableGpu
type AllocatedGpu = intelcrd.AllocatedGpu
type AllocatedDevices = intelcrd.AllocatedDevices
type RequestedGpu = intelcrd.RequestedGpu
type RequestedDevices = intelcrd.RequestedDevices
type GpuAllocationStateSpec = intelcrd.GpuAllocationStateSpec
type GpuAllocationStateList = intelcrd.GpuAllocationStateList

type GpuAllocationState struct {
	*intelcrd.GpuAllocationState
	clientset intelclientset.Interface
}

func NewGpuAllocationState(config *GpuAllocationStateConfig, clientset intelclientset.Interface) *GpuAllocationState {
	object := &intelcrd.GpuAllocationState{
		ObjectMeta: metav1.ObjectMeta{
			Name:      config.Name,
			Namespace: config.Namespace,
		},
	}

	if config.Owner != nil {
		object.OwnerReferences = []metav1.OwnerReference{*config.Owner}
	}

	gas := &GpuAllocationState{
		object,
		clientset,
	}

	return gas
}

func (g *GpuAllocationState) GetOrCreate() error {
	err := g.Get()
	if err == nil {
		return nil
	}
	if errors.IsNotFound(err) {
		return g.Create()
	}
	klog.Error("Could not get GpuAllocationState: %v", err)
	return err
}

func (g *GpuAllocationState) Create() error {
	gas := g.GpuAllocationState.DeepCopy()
	gas, err := g.clientset.GpuV1alpha().GpuAllocationStates(g.Namespace).Create(context.TODO(), gas, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	*g.GpuAllocationState = *gas
	return nil
}

func (g *GpuAllocationState) Delete() error {
	deletePolicy := metav1.DeletePropagationForeground
	deleteOptions := metav1.DeleteOptions{PropagationPolicy: &deletePolicy}
	err := g.clientset.GpuV1alpha().GpuAllocationStates(g.Namespace).Delete(context.TODO(), g.GpuAllocationState.Name, deleteOptions)
	if err != nil && !errors.IsNotFound(err) {
		return err
	}
	return nil
}

func (g *GpuAllocationState) Update(spec *intelcrd.GpuAllocationStateSpec) error {
	gas := g.GpuAllocationState.DeepCopy()
	gas.Spec = *spec
	gas, err := g.clientset.GpuV1alpha().GpuAllocationStates(g.Namespace).Update(context.TODO(), gas, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	*g.GpuAllocationState = *gas
	return nil
}

func (g *GpuAllocationState) UpdateStatus(status string) error {
	gas := g.GpuAllocationState.DeepCopy()
	gas.Status = status
	gas, err := g.clientset.GpuV1alpha().GpuAllocationStates(g.Namespace).Update(context.TODO(), gas, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	*g.GpuAllocationState = *gas
	return nil
}

func (g *GpuAllocationState) Get() error {
	gas, err := g.clientset.GpuV1alpha().GpuAllocationStates(g.Namespace).Get(context.TODO(), g.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	*g.GpuAllocationState = *gas
	return nil
}

func (g *GpuAllocationState) ListNames() ([]string, error) {
	gasnames := []string{}
	gass, err := g.clientset.GpuV1alpha().GpuAllocationStates(g.Namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return gasnames, err
	}
	for _, gas := range gass.Items {
		gasnames = append(gasnames, gas.Name)
	}
	return gasnames, nil
}

func (g *GpuAllocationState) AvailableAndConsumed() (map[string]*intelcrd.AllocatableGpu, map[string]*intelcrd.AllocatedGpu) {
	available := make(map[string]*intelcrd.AllocatableGpu)
	consumed := make(map[string]*intelcrd.AllocatedGpu)

	for _, device := range g.Spec.AllocatableGpus {
		switch device.Type {
		case intelcrd.GpuDeviceType:
			available[device.UUID] = &device
			consumed[device.UUID] = &intelcrd.AllocatedGpu{}
		}
	}
	klog.V(3).Infof("Available %v GPUs: %v", len(available), available)

	klog.V(5).Infof("Calculating consumed resources")
	for _, allocatedDevices := range g.Spec.ResourceClaimAllocations {
		for _, device := range allocatedDevices {
			switch device.Type {
			case intelcrd.GpuDeviceType:
				if _, exists := consumed[device.UUID]; !exists {
					consumed[device.UUID] = &intelcrd.AllocatedGpu{}
				}
				consumed[device.UUID].Memory += device.Memory
			}
			// TODO case SR-IOV
		}
	}

	for duuid, device := range consumed {
		klog.V(5).Infof("total consumed in device (v5) %v: %+v", duuid, device)
	}

	return available, consumed
}
