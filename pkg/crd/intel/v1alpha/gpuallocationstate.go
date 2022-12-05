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

package v1alpha

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Types of Devices that can be allocated
const (
	GpuDeviceType     = "gpu"
	UnknownDeviceType = "unknown"
)

// AllocatableGpu represents an allocatable GPU on a node
type AllocatableGpu struct {
	CDIDevice string `json:"cdiDevice"`
	Memory    int    `json:"memory"`
	Model     string `json:"model"`
	Type      string `json:"type"` // gpu, vf, pf, tile, vgpu
	UUID      string `json:"uuid"`
}

// AllocatedGpu represents an allocated GPU on a node
type AllocatedGpu struct {
	CDIDevice string `json:"cdiDevice"`
	Memory    int    `json:"memory"`
	Type      string `json:"type"` // gpu, vf, pf, tile, vgpu
	UUID      string `json:"uuid"`
}

// AllocatedDevices represents a list of allocated devices on a node
// +kubebuilder:validation:MaxItems=8
type AllocatedDevices []AllocatedGpu

// RequestedGpu represents a GPU being requested for allocation
type RequestedGpu struct {
	UUID string `json:"uuid,omitempty"`
}

// RequestedDevices represents a set of request spec and devices requested for allocation
type RequestedDevices struct {
	Spec GpuClaimParametersSpec `json:"spec"`
	// +kubebuilder:validation:MaxItems=8
	GPUs []RequestedGpu `json:"devices"`
}

// GpuAllocationStateSpec is the spec for the GpuAllocationState CRD
type GpuAllocationStateSpec struct {
	AllocatableGpus          map[string]AllocatableGpu   `json:"allocatableGpus,omitempty"`
	ResourceClaimAllocations map[string]AllocatedDevices `json:"resourceClaimAllocations,omitempty"`
	ResourceClaimRequests    map[string]RequestedDevices `json:"resourceClaimRequests,omitempty"`
}

// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +k8s:openapi-gen=true
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:resource:singular=gas

// GpuAllocationState holds the state required for allocation on a node
type GpuAllocationState struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GpuAllocationStateSpec `json:"spec,omitempty"`
	Status string                 `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// GpuAllocationStateList represents the "plural" of a GpuAllocationState CRD object
type GpuAllocationStateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []GpuAllocationState `json:"items"`
}
