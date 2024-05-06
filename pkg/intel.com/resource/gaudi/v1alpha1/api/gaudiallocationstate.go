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

package api

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	intelclientset "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gaudi/clientset/versioned"
	intelcrd "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gaudi/v1alpha1"
)

const (
	// Status indicating that CRD entry can be used by controller.
	GaudiAllocationStateStatusReady = "Ready"
	// Status indicating that CRD entry cannot be used by controller.
	GaudiAllocationStateStatusNotReady = "NotReady"
)

// Config to help getting entry of GaudiAllocationState.
type GaudiAllocationStateConfig struct {
	Name      string
	Namespace string
	Owner     *metav1.OwnerReference
}

// AllocatableDevice represents an allocatable Gaudi on a node.
type AllocatableDevice = intelcrd.AllocatableDevice

// TaintedDevice represents a tainted Gaudi on a node.
type TaintedDevice = intelcrd.TaintedDevice

// TaintedDevices is map of tainted devices on a node.
type TaintedDevices = intelcrd.TaintedDevices

// AllocatedDevice represents an allocated Gaudi on a node.
type AllocatedDevice = intelcrd.AllocatedDevice

// AllocatedDevices represents a list of allocated devices on a node.
type AllocatedDevices = intelcrd.AllocatedDevices

// Resources that were allocated for the claim by controller.
type AllocatedClaim = intelcrd.AllocatedClaim

// Map of resources allocated per claim UID.
type AllocatedClaims = intelcrd.AllocatedClaims

// GaudiAllocationStateSpec is the spec for the GaudiAllocationState CRD.
type GaudiAllocationStateSpec = intelcrd.GaudiAllocationStateSpec

// Main GaudiAllocationState object structure - used to track allocatable devices,
// allocated devices per ResourceClaim.UID, prepared devices per ResourceClaim.UID.
type GaudiAllocationState struct {
	*intelcrd.GaudiAllocationState
	clientset intelclientset.Interface
	// available is a list of devices available for allocation.
	// Updated only manually when GaudiAllocationState.UpdateAvailable() is called.
	Available map[string]intelcrd.AllocatableDevice
}

// Returns a blank GaudiAllocationState object ready to retrieve the record from
// API or creates a new one.
func NewGaudiAllocationState(config *GaudiAllocationStateConfig, clientset intelclientset.Interface) *GaudiAllocationState {
	object := &intelcrd.GaudiAllocationState{
		ObjectMeta: metav1.ObjectMeta{
			Name:      config.Name,
			Namespace: config.Namespace,
		},
	}

	if config.Owner != nil {
		object.OwnerReferences = []metav1.OwnerReference{*config.Owner}
	}

	gas := &GaudiAllocationState{
		object,
		clientset,
		map[string]intelcrd.AllocatableDevice{},
	}

	return gas
}

// Returns an existing GaudiAllocationState record fetched from API or submits
// new record ensuring it exists in the API.
func (g *GaudiAllocationState) GetOrCreate(ctx context.Context) error {
	err := g.Get(ctx)
	if err == nil {
		return nil
	}
	if errors.IsNotFound(err) {
		return g.Create(ctx)
	}
	klog.Errorf("Could not get GaudiAllocationState: %v", err)

	return err
}

// Submits a new GaudiAllocationState record to the API.
func (g *GaudiAllocationState) Create(ctx context.Context) error {
	gas := g.GaudiAllocationState.DeepCopy()
	gas, err := g.clientset.GaudiV1alpha1().GaudiAllocationStates(g.Namespace).Create(ctx, gas, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	*g.GaudiAllocationState = *gas

	return nil
}

// Removes the GaudiAllocationState record from the API.
func (g *GaudiAllocationState) Delete(ctx context.Context) error {
	deletePolicy := metav1.DeletePropagationForeground
	deleteOptions := metav1.DeleteOptions{PropagationPolicy: &deletePolicy}
	err := g.clientset.GaudiV1alpha1().GaudiAllocationStates(g.Namespace).Delete(
		ctx, g.GaudiAllocationState.Name, deleteOptions)
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	return nil
}

// Updates the GaudiAllocationState record in the API.
func (g *GaudiAllocationState) Update(ctx context.Context, spec *intelcrd.GaudiAllocationStateSpec) error {
	gas := g.GaudiAllocationState.DeepCopy()
	gas.Spec = *spec
	gas, err := g.clientset.GaudiV1alpha1().GaudiAllocationStates(g.Namespace).Update(ctx, gas, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	*g.GaudiAllocationState = *gas

	return nil
}

// Updates only status field of the GaudiAllocationState record in the API.
func (g *GaudiAllocationState) UpdateStatus(ctx context.Context, status string) error {
	gas := g.GaudiAllocationState.DeepCopy()
	gas.Status = status
	gas, err := g.clientset.GaudiV1alpha1().GaudiAllocationStates(g.Namespace).Update(ctx, gas, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	*g.GaudiAllocationState = *gas

	return nil
}

// Fetches existing GaudiAllocationState record from the API or returns error.
func (g *GaudiAllocationState) Get(ctx context.Context) error {
	gas, err := g.clientset.GaudiV1alpha1().GaudiAllocationStates(g.Namespace).Get(ctx, g.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	*g.GaudiAllocationState = *gas

	return nil
}

// Returns list of existing GaudiAllocationState records in the API.
func (g *GaudiAllocationState) ListNames(ctx context.Context) ([]string, error) {
	gasnames := []string{}
	gass, err := g.clientset.GaudiV1alpha1().GaudiAllocationStates(g.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return gasnames, err
	}
	for _, gas := range gass.Items {
		gasnames = append(gasnames, gas.Name)
	}

	return gasnames, nil
}

// UpdateAvailable updates Available devices map, filtering out tainted and allocated
// devices from AllocatableDevices.
// Use this method after the fresh contents of GAS.Spec was fetched from cache or API.
// Do not use this after the GAS.Spec was modified and changes not submitted to API.
func (g *GaudiAllocationState) UpdateAvailable() {
	available := make(map[string]intelcrd.AllocatableDevice)

	klog.V(5).Infof(
		"GaudiAllocationState spec has %v allocatable devices, %v allocated claims",
		len(g.Spec.AllocatableDevices),
		len(g.Spec.AllocatedClaims))

	for _, device := range g.Spec.AllocatableDevices {
		if g.DeviceIsTainted(device.UID) {
			continue
		}
		available[device.UID] = device
	}

	for claimUID, claimAllocation := range g.Spec.AllocatedClaims {
		klog.V(5).Infof("Claim %v: %+v", claimUID, claimAllocation)
		for _, device := range claimAllocation.Devices {
			if _, found := available[device.UID]; !found {
				klog.Warningf("Allocated device %v is not available", device.UID)
				continue
			}
			delete(available, device.UID)
		}
	}

	g.Available = available
}

// returns true (only) if device is in the TaintedDevices map.
func (g *GaudiAllocationState) DeviceIsTainted(deviceUID string) bool {
	if g.Spec.TaintedDevices == nil {
		return false
	}
	if status, found := g.Spec.TaintedDevices[deviceUID]; found {
		klog.V(5).Infof("Device %v is tainted due to: %v", deviceUID, status.Reasons)
		return true
	}
	return false
}

// DeviceIsAllocated returns true if device is present in any allocation,
// otherwise false.
func (g *GaudiAllocationState) DeviceIsAllocated(deviceUID string) bool {
	for claimUID, claimAllocation := range g.Spec.AllocatedClaims {
		for _, allocatedDevice := range claimAllocation.Devices {
			if allocatedDevice.UID == deviceUID {
				klog.V(5).Infof("Device %v is already allocated to claim %v", deviceUID, claimUID)

				return true
			}
		}
	}

	return false
}
