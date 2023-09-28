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

package v1alpha2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GpuClaimParametersSpec is the spec for the GpuClaimParameters CRD.
type GpuClaimParametersSpec struct {
	// How many items of the Type are being requested. 10 PCIe devices x 64 SR-IOV VFs each = 640 items maximum on one Node.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=640
	Count uint64 `json:"count"`
	// Per GPU memory request, in MiB, maximum 1048576 (1 TiB)
	// +kubebuilder:validation:Minimum=8
	// +kubebuilder:validation:Maximum=1048576
	Memory uint64 `json:"memory,omitempty"`
	// Per GPU millicores request.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=1000
	Millicores uint64 `json:"millicores,omitempty"`

	// +kubebuilder:validation:
	Type GpuType `json:"type,omitempty"`

	// True if the same ResourceClaim can be shared by multiple Pods.
	Shareable bool `json:"shareable,omitempty"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +k8s:openapi-gen=true
// +kubebuilder:resource:scope=Namespaced

// GpuClaimParameters holds the set of parameters provided when creating a resource claim for a GPU.
type GpuClaimParameters struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec GpuClaimParametersSpec `json:"spec,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// GpuClaimParametersList represents the "plural" of a GpuClaimParameters CRD object.
type GpuClaimParametersList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []GpuClaimParameters `json:"items"`
}
