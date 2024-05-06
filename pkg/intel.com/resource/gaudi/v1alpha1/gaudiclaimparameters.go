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

// GaudiClaimParametersSpec is the spec for the GaudiClaimParameters CRD.
type GaudiClaimParametersSpec struct {
	// How many devices is requested.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=8
	Count uint64 `json:"count"`
	// No other parameters needed, Gaudi does not support more than one process per accelerator
	// https://docs.habana.ai/en/latest/PyTorch/Reference/PT_Multiple_Tenants_on_HPU/index.html
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +k8s:openapi-gen=true
// +kubebuilder:resource:scope=Namespaced

// GaudiClaimParameters holds the set of parameters provided when creating a resource claim for a device.
type GaudiClaimParameters struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec GaudiClaimParametersSpec `json:"spec,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// GaudiClaimParametersList represents the "plural" of a GaudiClaimParameters CRD object.
type GaudiClaimParametersList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []GaudiClaimParameters `json:"items"`
}
