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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AllocatableDevice represents an allocatable Gaudi on a node.
type AllocatableDevice struct {
	// Unique identifier of device: PCI address and PCI Device ID.
	UID string `json:"uid"`
	// PCI ID of the Gaudi device.
	Model string `json:"model"`
}

// TaintedDevice represents a tainted Gaudi on a node.
type TaintedDevice struct {
	// Reasons why device is tainted, which _all_ need to be
	// resolved, before device can be dropped from taints map.
	Reasons map[string]bool `json:"reasons,omitempty"`
}

// AllocatedDevice represents an allocated Gaudi on a node.
type AllocatedDevice struct {
	// Unique identifier of device: PCI address and PCI Device ID.
	UID string `json:"uid"`
}

// AllocatedDevices represents a list of allocated devices on a node.
// +kubebuilder:validation:MaxItems=640
type AllocatedDevices []AllocatedDevice

// Resources that were allocated for the claim by controller.
type AllocatedClaim struct {
	Devices AllocatedDevices `json:"devices"`
}

// Map of resources allocated per claim UID.
type AllocatedClaims map[string]AllocatedClaim

// Map of tainted devices on a node.
type TaintedDevices map[string]TaintedDevice

// GaudiAllocationStateSpec is the spec for the GaudiAllocationState CRD.
type GaudiAllocationStateSpec struct {
	AllocatableDevices map[string]AllocatableDevice `json:"allocatableDevices,omitempty"`
	TaintedDevices     map[string]TaintedDevice     `json:"taintedDevices,omitempty"`
	AllocatedClaims    map[string]AllocatedClaim    `json:"allocatedClaims,omitempty"`
}

// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +k8s:openapi-gen=true
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:resource:singular=gas

// GaudiAllocationState holds the state required for allocation on a node.
type GaudiAllocationState struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GaudiAllocationStateSpec `json:"spec,omitempty"`
	Status string                   `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// GaudiAllocationStateList represents the "plural" of a GaudiAllocationState CRD object.
type GaudiAllocationStateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []GaudiAllocationState `json:"items"`
}
