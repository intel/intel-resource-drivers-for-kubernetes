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
	"sync"

	intelcrd "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/crd/intel/v1alpha/api"
	"k8s.io/klog/v2"
)

/*
	PerNodeClaimRequests is a map {
		claim-uid : map {
			nodename : intelcrd.ResourceClaimAllocation {
				Request GPUClaimParametersSpec `json:"request"`
				GPUs    AllocatedGPUs          `json:"gpus"`
			}
		}
	}
*/
type PerNodeClaimRequests struct {
	sync.RWMutex
	requests map[string]map[string]intelcrd.ResourceClaimAllocation
}

func NewPerNodeClaimRequests() *PerNodeClaimRequests {
	return &PerNodeClaimRequests{
		requests: make(map[string]map[string]intelcrd.ResourceClaimAllocation),
	}
}

func (p *PerNodeClaimRequests) Exists(claimUID, node string) bool {
	p.RLock()
	defer p.RUnlock()

	if _, exists := p.requests[claimUID]; !exists {
		return false
	}

	if _, exists := p.requests[claimUID][node]; !exists {
		return false
	}

	return true
}

func (p *PerNodeClaimRequests) Get(claimUID, node string) intelcrd.ResourceClaimAllocation {
	p.RLock()
	defer p.RUnlock()

	if !p.Exists(claimUID, node) {
		return intelcrd.ResourceClaimAllocation{}
	}
	return p.requests[claimUID][node]
}

func (p *PerNodeClaimRequests) CleanupNode(gas *intelcrd.GpuAllocationState) {
	p.RLock()
	klog.V(5).Infof("Cleaning up resource requests for node %v", gas.Name)
	for claimUID := range p.requests {
		// if the resource claim was suitable for GAS node
		if _, exists := p.requests[claimUID][gas.Name]; exists {
			// cleanup processed claim requests
			if _, exists := gas.Spec.ResourceClaimAllocations[claimUID]; exists {
				delete(p.requests, claimUID)
			}
		}
	}
	p.RUnlock()
}

func (p *PerNodeClaimRequests) Set(claimUID, node string, devices intelcrd.ResourceClaimAllocation) {
	p.Lock()
	defer p.Unlock()

	_, exists := p.requests[claimUID]
	if !exists {
		p.requests[claimUID] = make(map[string]intelcrd.ResourceClaimAllocation)
	}

	p.requests[claimUID][node] = devices
}

func (p *PerNodeClaimRequests) Remove(claimUID string) {
	p.Lock()
	defer p.Unlock()

	delete(p.requests, claimUID)
}
