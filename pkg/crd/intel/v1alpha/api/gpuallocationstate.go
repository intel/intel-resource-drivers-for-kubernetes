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

package api

import (
	"context"
	"fmt"

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
type AllocatedGpus = intelcrd.AllocatedGpus
type ResourceClaimAllocation = intelcrd.ResourceClaimAllocation
type ResourceClaimAllocations = intelcrd.ResourceClaimAllocations
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
	klog.Errorf("Could not get GpuAllocationState: %v", err)

	return err
}

func (g *GpuAllocationState) Create() error {
	gas := g.GpuAllocationState.DeepCopy()
	gas, err := g.clientset.GpuV1alpha().GpuAllocationStates(g.Namespace).Create(
		context.TODO(), gas, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	*g.GpuAllocationState = *gas

	return nil
}

func (g *GpuAllocationState) Delete() error {
	deletePolicy := metav1.DeletePropagationForeground
	deleteOptions := metav1.DeleteOptions{PropagationPolicy: &deletePolicy}
	err := g.clientset.GpuV1alpha().GpuAllocationStates(g.Namespace).Delete(
		context.TODO(), g.GpuAllocationState.Name, deleteOptions)
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	return nil
}

func (g *GpuAllocationState) Update(spec *intelcrd.GpuAllocationStateSpec) error {
	gas := g.GpuAllocationState.DeepCopy()
	gas.Spec = *spec
	gas, err := g.clientset.GpuV1alpha().GpuAllocationStates(g.Namespace).Update(
		context.TODO(), gas, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update GAS: %v", err)
	}
	*g.GpuAllocationState = *gas

	return nil
}

func (g *GpuAllocationState) UpdateStatus(status string) error {
	gas := g.GpuAllocationState.DeepCopy()
	gas.Status = status
	gas, err := g.clientset.GpuV1alpha().GpuAllocationStates(g.Namespace).Update(
		context.TODO(), gas, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	*g.GpuAllocationState = *gas

	return nil
}

func (g *GpuAllocationState) Get() error {
	gas, err := g.clientset.GpuV1alpha().GpuAllocationStates(g.Namespace).Get(
		context.TODO(), g.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	*g.GpuAllocationState = *gas

	return nil
}

func (g *GpuAllocationState) ListNames() ([]string, error) {
	gasnames := []string{}
	gass, err := g.clientset.GpuV1alpha().GpuAllocationStates(g.Namespace).List(
		context.TODO(), metav1.ListOptions{})
	if err != nil {
		return gasnames, err
	}
	for _, gas := range gass.Items {
		gasnames = append(gasnames, gas.Name)
	}

	return gasnames, nil
}

// Filter out Allocatable devices that have VFs and calculate consumed resources based on allocations.
func (g *GpuAllocationState) AvailableAndConsumed() (
	map[string]*intelcrd.AllocatableGpu, map[string]*intelcrd.AllocatableGpu) {
	available := make(map[string]*intelcrd.AllocatableGpu)
	consumed := make(map[string]*intelcrd.AllocatableGpu)

	klog.V(5).Infof(
		"GAS spec has %v allocatable devices, %v claimallocations",
		len(g.Spec.Allocatable),
		len(g.Spec.ResourceClaimAllocations))

	// safest way is two iterations, first Gpus, then Vfs, to prevent nil-exceptions and overwriting
	for _, device := range g.Spec.Allocatable {
		if device.Type == intelcrd.GpuDeviceType {
			available[device.UID] = device.DeepCopy()
			consumed[device.UID] = &intelcrd.AllocatableGpu{
				UID:       device.UID,
				ParentUID: device.ParentUID,
				Type:      device.Type}
		}
	}

	for _, device := range g.Spec.Allocatable {
		if device.Type == intelcrd.VfDeviceType {
			// test for presence in consumed, because available entry could have been deleted by preceeding child VF
			if _, found := consumed[device.ParentUID]; !found {
				klog.Errorf("GAS %v is broken, parent %v of Vf %v is missing", g.Name, device.ParentUID, device.UID)
				return make(map[string]*intelcrd.AllocatableGpu), make(map[string]*intelcrd.AllocatableGpu)
			}
			available[device.UID] = device.DeepCopy()
			consumed[device.UID] = &intelcrd.AllocatableGpu{
				UID:       device.UID,
				ParentUID: device.ParentUID,
				Type:      device.Type}
			consumed[device.ParentUID].Maxvfs++
			consumed[device.ParentUID].Memory += device.Memory
			delete(available, device.ParentUID)
		}
	}

	klog.V(3).Infof("Available %v devices: %v", len(available), available)

	for claimUID, claimAllocation := range g.Spec.ResourceClaimAllocations {
		klog.V(5).Infof("Claim %v: %+v", claimUID, claimAllocation)
		for _, device := range claimAllocation.Gpus {
			switch device.Type {
			case intelcrd.GpuDeviceType:
				if _, found := consumed[device.UID]; !found {
					klog.Warningf("Allocated device (GPU) %v is not available", device.UID)
					continue
				}
				consumed[device.UID].Memory += device.Memory
			case intelcrd.VfDeviceType:
				if _, found := consumed[device.UID]; !found {
					// yet to be provisioned, did not consume anything
					continue
				}
				consumed[device.UID].Memory = device.Memory
				consumed[device.UID].Maxvfs++
			default:
				klog.Warningf("Unsupported device type %v of device %v", string(device.Type), device.UID)
			}
		}
	}

	for duid, device := range consumed {
		klog.V(5).Infof("total consumed in device %v: %+v", duid, device)
	}

	return available, consumed
}

// Return true if  all VFs currently allocated from parentUID belong to the same owner, otherwise false.
func (g *GpuAllocationState) SameOwnerVFAllocations(parentUID string, owner string) bool {
	klog.V(5).Infof("Checking if all VFs on device %v owned by %v", parentUID, owner)
	if g.Spec.ResourceClaimAllocations == nil {
		klog.V(5).Infof("No allocations yet, nothing to check")

		return true
	}

	for _, claimAllocation := range g.Spec.ResourceClaimAllocations {
		for _, device := range claimAllocation.Gpus {
			if device.Type == intelcrd.VfDeviceType && device.ParentUID == parentUID && claimAllocation.Owner != owner {
				return false
			}
		}
	}
	return true
}

// Check if allocatable devices have VFs with parentUID, or
// allocated devices have pending / not yet provisioned VFs with parentUID.
func (g *GpuAllocationState) GpuHasVFs(parentUID string) bool {
	foundVFs := 0
	if _, exists := g.Spec.Allocatable[parentUID]; !exists {
		klog.Warning("Parent device %v does not exist in allocatable devices", parentUID)

		return false
	}
	for deviceUID, device := range g.Spec.Allocatable {
		klog.V(5).Infof("Checking %v: type: %v, parent: %v", deviceUID, string(device.Type), device.ParentUID)
		if device.Type == intelcrd.VfDeviceType && device.ParentUID == parentUID {
			klog.V(5).Infof("Found allocatable VF %v from parent %v", deviceUID, device.ParentUID)
			return true
		}
	}

	for claimUID, claimAllocation := range g.Spec.ResourceClaimAllocations {
		klog.V(5).Infof("Checking claim %v", claimUID)
		for _, device := range claimAllocation.Gpus {
			if device.Type == intelcrd.VfDeviceType && device.ParentUID == parentUID {
				klog.V(5).Infof("Found allocated unprovisioned VF %v from parent %v", device.UID, device.ParentUID)
				return true
			}
		}
	}
	klog.V(5).Infof("Device %v has %v VFs", parentUID, foundVFs)

	return foundVFs != 0
}

func (g *GpuAllocationState) DeviceIsAllocated(deviceUID string) bool {
	for claimUID, claimAllocation := range g.Spec.ResourceClaimAllocations {
		for _, allocatedDevice := range claimAllocation.Gpus {
			if allocatedDevice.UID == deviceUID {
				klog.V(5).Infof("Device %v is already allocated to claim %v", deviceUID, claimUID)

				return true
			}
		}
	}

	return false
}

func AllocatedFromAllocatable(source *AllocatableGpu, memory int) AllocatedGpu {
	return AllocatedGpu{
		UID:       source.UID,
		Memory:    memory,
		ParentUID: source.ParentUID,
		Type:      source.Type,
	}
}
