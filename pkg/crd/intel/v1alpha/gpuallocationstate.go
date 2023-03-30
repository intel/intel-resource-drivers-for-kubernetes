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

package v1alpha

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Types of Devices that can be allocated.
const (
	GpuDeviceType     = "gpu"
	VfDeviceType      = "vf"
	AnyDeviceType     = "any"
	UnknownDeviceType = "unknown"
)

// AllocatableGpu represents an allocatable Gpu on a node.
type AllocatableGpu struct {
	// Unique identifier of device: PCI address and PCI Device ID.
	UID string `json:"uid"`
	// Amount of local memory in MiB.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1048576
	Memory int `json:"memory"`
	// pci-id of the Gpu device.
	Model string `json:"model"`
	// Type of the device: bare-metal Gpu or SR-IOV Virtual Function (VF).
	Type GpuType `json:"type"` // gpu, vf
	// Device where VF should be / is provisioned.
	ParentUID string `json:"parentuid"`
	// Greater than 0 if SR-IOV is supported / enabled.
	Maxvfs int `json:"maxvfs"`
}

// AllocatedGpu represents an allocated Gpu on a node.
type AllocatedGpu struct {
	// Unique identifier of device: PCI address and PCI Device ID.
	UID string `json:"uid"`
	// Amount of local memory in MiB.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1048576
	Memory int `json:"memory"`
	// Type of the device: bare-metal Gpu or SR-IOV Virtual Function (VF).
	Type GpuType `json:"type"` // gpu, vf
	// Device where VF should be / is provisioned.
	ParentUID string `json:"parentuid"`
	// Virtual Function profile defines amount of local memory and time slice VF gets.
	Profile string `json:"profile"`
}

// AllocatedDevices represents a list of allocated devices on a node.
// +kubebuilder:validation:MaxItems=8
type AllocatedGpus []AllocatedGpu

// +kubebuilder:validation:Enum=gpu;vf;any
type GpuType string

// RequestedDevices represents a set of request spec and devices requested for allocation.
type ResourceClaimAllocation struct {
	Request GpuClaimParametersSpec `json:"request"`
	Gpus    AllocatedGpus          `json:"gpus"`
	// Pod UID, for delayed allocation to match Resource Claims of same Pod when allocating VFs.
	Owner string `json:"owner"`
}

type ResourceClaimAllocations map[string]ResourceClaimAllocation

// GpuAllocationStateSpec is the spec for the GpuAllocationState CRD.
type GpuAllocationStateSpec struct {
	Allocatable              map[string]AllocatableGpu          `json:"allocatable,omitempty"`
	Prepared                 map[string]AllocatedGpus           `json:"prepared,omitempty"`
	ResourceClaimAllocations map[string]ResourceClaimAllocation `json:"resourceClaimAllocations,omitempty"`
}

// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +k8s:openapi-gen=true
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:resource:singular=gas

// GpuAllocationState holds the state required for allocation on a node.
type GpuAllocationState struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GpuAllocationStateSpec `json:"spec,omitempty"`
	Status string                 `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// GpuAllocationStateList represents the "plural" of a GpuAllocationState CRD object.
type GpuAllocationStateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []GpuAllocationState `json:"items"`
}
