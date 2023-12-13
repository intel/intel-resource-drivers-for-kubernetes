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

package main

import (
	"sort"

	intelcrd "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gpu/v1alpha2/api"
)

const (
	descending = true
	ascending  = false
)

func (d *driver) initPolicy(config *configType) {
	switch *config.flags.preferredAllocationPolicy {
	case "balanced":
		d.preferredOrder = d.balancedOrder
	case "packed":
		d.preferredOrder = d.packedOrder
	default:
		d.preferredOrder = d.noneOrder
	}

	switch *config.flags.allocationPolicyResource {
	case "millicores":
		d.policyResourceValue = func(allocatable *intelcrd.AllocatableGpu) uint64 {
			return allocatable.Millicores
		}
	case "memory":
		fallthrough
	default:
		d.policyResourceValue = func(allocatable *intelcrd.AllocatableGpu) uint64 {
			return allocatable.Memory
		}
	}
}

// preferredOrder returns the keys from the available map in the preferred
// order of the selected allocation policy (packed, balanced or none).
type preferredOrder func(available map[string]*intelcrd.AllocatableGpu,
	consumed map[string]*intelcrd.AllocatableGpu) []string

// getResourceSortedKeys returns the keys from the available map in either
// ascending or descending order.
func (d *driver) getResourceSortedKeys(available map[string]*intelcrd.AllocatableGpu,
	consumed map[string]*intelcrd.AllocatableGpu, descend bool) []string {
	keys := mapKeys(available)

	sort.SliceStable(keys, func(i, j int) bool {
		iResourceLeft := d.policyResourceValue(available[keys[i]]) - d.policyResourceValue(consumed[keys[i]])
		jResourceLeft := d.policyResourceValue(available[keys[j]]) - d.policyResourceValue(consumed[keys[j]])

		if descend {
			return jResourceLeft < iResourceLeft
		}

		return iResourceLeft < jResourceLeft
	})

	return keys
}

// noneOrder is the default mode where map keys and therefore allocations are random.
func (d *driver) noneOrder(available map[string]*intelcrd.AllocatableGpu,
	consumed map[string]*intelcrd.AllocatableGpu) []string {
	return mapKeys(available)
}

// balancedOrder returns the available map keys in descending order sorted by
// the amount of preferred resource left.
func (d *driver) balancedOrder(available map[string]*intelcrd.AllocatableGpu,
	consumed map[string]*intelcrd.AllocatableGpu) []string {
	return d.getResourceSortedKeys(available, consumed, descending)
}

// packedOrder returns the available map keys in ascending order sorted by
// the amount of preferred resource left.
func (d *driver) packedOrder(available map[string]*intelcrd.AllocatableGpu,
	consumed map[string]*intelcrd.AllocatableGpu) []string {
	return d.getResourceSortedKeys(available, consumed, ascending)
}

// policyResourceValue returns the policy resource value for the GPU.
type policyResourceValue func(*intelcrd.AllocatableGpu) uint64
