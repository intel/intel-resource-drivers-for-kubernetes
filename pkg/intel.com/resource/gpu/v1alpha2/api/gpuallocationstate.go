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

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	intelclientset "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gpu/clientset/versioned"
	intelcrd "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gpu/v1alpha2"
)

const (
	// Status indicating that CRD entry can be used by controller.
	GpuAllocationStateStatusReady = "Ready"
	// Status indicating that CRD entry cannot be used by controller.
	GpuAllocationStateStatusNotReady = "NotReady"
	// Value for UID field of device, used for VFs that are not yet provisioned.
	NewVFUID = ""
)

// Config to help getting entry of GpuAllocationState.
type GpuAllocationStateConfig struct {
	Name      string
	Namespace string
	Owner     *metav1.OwnerReference
}

// AllocatableGpu represents an allocatable Gpu on a node.
type AllocatableGpu = intelcrd.AllocatableGpu

// TaintedGpu represents a tainted Gpu on a node.
type TaintedGpu = intelcrd.TaintedGpu

// TaintedDevices is map of tainted devices on a node.
type TaintedDevices = intelcrd.TaintedDevices

// AllocatedGpu represents an allocated Gpu on a node.
type AllocatedGpu = intelcrd.AllocatedGpu

// AllocatedGpus represents a list of allocated devices on a node.
type AllocatedGpus = intelcrd.AllocatedGpus

// Resources that were allocated for the claim by controller.
type AllocatedClaim = intelcrd.AllocatedClaim

// Map of resources allocated per claim UID.
type AllocatedClaims = intelcrd.AllocatedClaims

// Resources prepared for the claim by kubelet-plugin.
type PreparedClaim = intelcrd.PreparedClaim

// Resources prepared for the claim by kubelet-plugin.
type PreparedClaims = intelcrd.PreparedClaims

// GpuAllocationStateSpec is the spec for the GpuAllocationState CRD.
type GpuAllocationStateSpec = intelcrd.GpuAllocationStateSpec

// Main GpuAllocationState object structure - used to track allocatable devices,
// allocated devices per ResourceClaim.UID, prepared devices per ResourceClaim.UID.
type GpuAllocationState struct {
	*intelcrd.GpuAllocationState
	clientset intelclientset.Interface
	// available is a list of devices available for allocation.
	// Updated only manually when GpuAllocationState.UpdateAvailableAndConsumed() is called.
	Available map[string]*intelcrd.AllocatableGpu
	// consumed is a list of available devices' resources consumed.
	// It is identical to available but contains the occupied resources.
	// Updated only manually when GpuAllocationState.UpdateAvailableAndConsumed() is called.
	Consumed map[string]*intelcrd.AllocatableGpu
}

// Returns a blank GpuAllocationState object ready to retrieve the record from
// API or creates a new one.
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
		map[string]*intelcrd.AllocatableGpu{},
		map[string]*intelcrd.AllocatableGpu{},
	}

	return gas
}

// Returns an existing GpuAllocationState record fetched from API or submits
// new record ensuring it exists in the API.
func (g *GpuAllocationState) GetOrCreate(ctx context.Context) error {
	err := g.Get(ctx)
	if err == nil {
		return nil
	}
	if errors.IsNotFound(err) {
		return g.Create(ctx)
	}
	klog.Errorf("Could not get GpuAllocationState: %v", err)

	return err
}

// Submits a new GpuAllocationState record to the API.
func (g *GpuAllocationState) Create(ctx context.Context) error {
	gas := g.GpuAllocationState.DeepCopy()
	gas, err := g.clientset.GpuV1alpha2().GpuAllocationStates(g.Namespace).Create(ctx, gas, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	*g.GpuAllocationState = *gas

	return nil
}

// Removes the GpuAllocationState record from the API.
func (g *GpuAllocationState) Delete(ctx context.Context) error {
	deletePolicy := metav1.DeletePropagationForeground
	deleteOptions := metav1.DeleteOptions{PropagationPolicy: &deletePolicy}
	err := g.clientset.GpuV1alpha2().GpuAllocationStates(g.Namespace).Delete(
		ctx, g.GpuAllocationState.Name, deleteOptions)
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	return nil
}

// Updates the GpuAllocationState record in the API.
func (g *GpuAllocationState) Update(ctx context.Context, spec *intelcrd.GpuAllocationStateSpec) error {
	gas := g.GpuAllocationState.DeepCopy()
	gas.Spec = *spec
	gas, err := g.clientset.GpuV1alpha2().GpuAllocationStates(g.Namespace).Update(ctx, gas, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	*g.GpuAllocationState = *gas

	return nil
}

// Updates only status field of the GpuAllocationState record in the API.
func (g *GpuAllocationState) UpdateStatus(ctx context.Context, status string) error {
	gas := g.GpuAllocationState.DeepCopy()
	gas.Status = status
	gas, err := g.clientset.GpuV1alpha2().GpuAllocationStates(g.Namespace).Update(ctx, gas, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	*g.GpuAllocationState = *gas

	return nil
}

// Fetches existing GpuAllocationState record from the API or returns error.
func (g *GpuAllocationState) Get(ctx context.Context) error {
	gas, err := g.clientset.GpuV1alpha2().GpuAllocationStates(g.Namespace).Get(ctx, g.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	*g.GpuAllocationState = *gas

	return nil
}

// Returns list of existing GpuAllocationState records in the API.
func (g *GpuAllocationState) ListNames(ctx context.Context) ([]string, error) {
	gasnames := []string{}
	gass, err := g.clientset.GpuV1alpha2().GpuAllocationStates(g.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return gasnames, err
	}
	for _, gas := range gass.Items {
		gasnames = append(gasnames, gas.Name)
	}

	return gasnames, nil
}

// UpdateAvailableAndConsumed updates allocatable devices map filtering out devices that have
// VFs provisioned, and calculates consumed resources based on existing allocations.
// Use this method after the fresh contents of GAS.Spec was fetched from cache or API.
// Do not use this after the GAS.Spec was modified and changes not submitted to API.
func (g *GpuAllocationState) UpdateAvailableAndConsumed() {
	available, consumed := g.availableAndConsumedFromAllocatable()

	for claimUID, claimAllocation := range g.Spec.AllocatedClaims {
		klog.V(5).Infof("Claim %v: %+v", claimUID, claimAllocation)
		for _, device := range claimAllocation.Gpus {
			switch device.Type {
			case intelcrd.GpuDeviceType:
				if _, found := consumed[device.UID]; !found {
					klog.Warningf("Allocated device (GPU) %v is not available", device.UID)
					continue
				}
			case intelcrd.VfDeviceType:
				if device.UID == "" { // Schrödinger's VF: can exist in gas.allocatableDevices or be absent.
					// Remove it from available if it is there, can be found by parent UID and VFIndex.
					for _, availableDevice := range available {
						if device.VFIndex == availableDevice.VFIndex && device.ParentUID == availableDevice.ParentUID {
							delete(available, availableDevice.UID)
							break
						}
					}
					// if new allocation was requested faster than kubelet-plugin provisioned this VF,
					// or if GAS is not yet updated - we should not use parent device.
					delete(available, device.ParentUID)
					continue
				}
				// TODO: SR-IOV millicores. Until it is implemented gpuFitsRequest relies on this counter to
				// know if VF was allocated somewhere.
				consumed[device.UID].Maxvfs++
			default:
				klog.Warningf("Unsupported device type %v of device %v", string(device.Type), device.UID)
				continue
			}

			consumed[device.UID].Memory += device.Memory
			consumed[device.UID].Millicores += device.Millicores
		}
	}

	for duid, device := range available {
		klog.V(5).Infof("total available in device %v: %+v", duid, device)
	}

	for duid, device := range consumed {
		klog.V(5).Infof("total consumed in device %v: %+v", duid, device)
	}

	g.Available = available
	g.Consumed = consumed
}

func (g *GpuAllocationState) availableAndConsumedFromAllocatable() (map[string]*intelcrd.AllocatableGpu, map[string]*intelcrd.AllocatableGpu) {
	available := make(map[string]*intelcrd.AllocatableGpu)
	consumed := make(map[string]*intelcrd.AllocatableGpu)

	klog.V(5).Infof(
		"GpuAllocationState spec has %v allocatable devices, %v allocated claims",
		len(g.Spec.AllocatableDevices),
		len(g.Spec.AllocatedClaims))

	// Safest way is two iterations: first - GPUs, then - VFs to prevent nil-exceptions and overwriting.
	for _, device := range g.Spec.AllocatableDevices {
		if device.Type == intelcrd.GpuDeviceType {
			if g.deviceIsTainted(device.UID) {
				continue
			}
			available[device.UID] = device.DeepCopy()
			consumed[device.UID] = &intelcrd.AllocatableGpu{
				UID:       device.UID,
				ParentUID: device.ParentUID,
				Type:      device.Type}
		}
	}

	for _, device := range g.Spec.AllocatableDevices {
		if device.Type == intelcrd.VfDeviceType {
			if g.deviceIsTainted(device.ParentUID) {
				continue
			}
			// Test for presence in consumed, because available entry could have been deleted by preceding child VF.
			if _, found := consumed[device.ParentUID]; !found {
				klog.Errorf("GpuAllocationState %v is broken, parent %v of VF %v is missing", g.Name, device.ParentUID, device.UID)

				return make(map[string]*intelcrd.AllocatableGpu), make(map[string]*intelcrd.AllocatableGpu)
			}
			available[device.UID] = device.DeepCopy()
			consumed[device.UID] = &intelcrd.AllocatableGpu{
				UID:       device.UID,
				ParentUID: device.ParentUID,
				Type:      device.Type,
			}
			consumed[device.ParentUID].Maxvfs++
			consumed[device.ParentUID].Memory += device.Memory
			consumed[device.ParentUID].Millicores += device.Millicores
			delete(available, device.ParentUID)
		}
	}

	return available, consumed
}

// GpuHasVFs returns true if allocatable devices have VFs with parentUID, or
// allocated devices have pending / not yet provisioned VFs with parentUID.
func (g *GpuAllocationState) GpuHasVFs(parentUID string) bool {
	if _, exists := g.Spec.AllocatableDevices[parentUID]; !exists {
		klog.Warningf("Parent device %v does not exist in allocatable devices", parentUID)

		return false
	}
	for deviceUID, device := range g.Spec.AllocatableDevices {
		klog.V(5).Infof("Checking %v: type: %v, parent: %v", deviceUID, string(device.Type), device.ParentUID)
		if device.Type == intelcrd.VfDeviceType && device.ParentUID == parentUID {
			klog.V(5).Infof("Found allocatable VF %v from parent %v", deviceUID, device.ParentUID)

			return true
		}
	}

	for claimUID, claimAllocation := range g.Spec.AllocatedClaims {
		klog.V(5).Infof("Checking claim %v", claimUID)
		for _, device := range claimAllocation.Gpus {
			if device.Type == intelcrd.VfDeviceType && device.ParentUID == parentUID {
				klog.V(5).Infof("Found allocated unprovisioned VF %v from parent %v", device.UID, device.ParentUID)

				return true
			}
		}
	}

	return false
}

// returns true (only) if device is in the TaintedDevices map.
func (g *GpuAllocationState) deviceIsTainted(deviceUID string) bool {
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
func (g *GpuAllocationState) DeviceIsAllocated(deviceUID string) bool {
	if deviceUID == NewVFUID {
		klog.Error("device is a new VF with no UID, cannot check if it is allocated")
	}
	for claimUID, claimAllocation := range g.Spec.AllocatedClaims {
		for _, allocatedDevice := range claimAllocation.Gpus {
			if allocatedDevice.UID == deviceUID {
				klog.V(5).Infof("Device %v is already allocated to claim %v", deviceUID, claimUID)

				return true
			}
		}
	}

	return false
}

// AllocatedFromAllocatable returns AllocatedGpu with relevant fields copied
// from AllocatableGpu.
func AllocatedFromAllocatable(source *AllocatableGpu, claimParams *intelcrd.GpuClaimParametersSpec, shared bool) AllocatedGpu {
	allocated := AllocatedGpu{
		UID:        source.UID,
		Memory:     claimParams.Memory,
		Millicores: claimParams.Millicores,
		ParentUID:  source.ParentUID,
		Type:       source.Type,
	}

	if !shared {
		// For exclusive allocation use all available millicores.
		allocated.Millicores = source.Millicores
		// All of the memory is allocated.
		allocated.Memory = source.Memory
	}

	return allocated
}
