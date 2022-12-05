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

package main

import (
	"sync"

	intelcrd "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/crd/intel/v1alpha/api"
	"k8s.io/klog/v2"
)

/*
	 requests is a map {
		claim-uuid : map {
			nodename : intelcrd.RequestedDevices{
							Spec    GpuClaimParametersSpec `json:"spec"`
							Devices []RequestedGpu         `json:"devices"`
						}
			}
		}
	}
*/
type PerNodeClaimRequests struct {
	sync.RWMutex
	requests map[string]map[string]intelcrd.RequestedDevices
}

func NewPerNodeClaimRequests() *PerNodeClaimRequests {
	return &PerNodeClaimRequests{
		requests: make(map[string]map[string]intelcrd.RequestedDevices),
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

func (p *PerNodeClaimRequests) Get(claimUID, node string) intelcrd.RequestedDevices {
	p.RLock()
	defer p.RUnlock()

	if !p.Exists(claimUID, node) {
		return intelcrd.RequestedDevices{}
	}
	return p.requests[claimUID][node]
}

func (p *PerNodeClaimRequests) CleanupNode(gas *intelcrd.GpuAllocationState) {
	p.RLock()
	for claimUID := range p.requests {
		if request, exists := p.requests[claimUID][gas.Name]; exists {
			klog.V(5).Infof("Cleaning up resource requests for node %v", gas.Name)
			// cleanup processed claim requests
			if _, exists := gas.Spec.ResourceClaimRequests[claimUID]; exists {
				delete(p.requests, claimUID)
			} else {
				gas.Spec.ResourceClaimRequests[claimUID] = request
			}
		}
	}
	p.RUnlock()
}

func (p *PerNodeClaimRequests) Set(claimUID, node string, devices intelcrd.RequestedDevices) {
	p.Lock()
	defer p.Unlock()

	_, exists := p.requests[claimUID]
	if !exists {
		p.requests[claimUID] = make(map[string]intelcrd.RequestedDevices)
	}

	p.requests[claimUID][node] = devices
}

func (p *PerNodeClaimRequests) Remove(claimUID string) {
	p.Lock()
	defer p.Unlock()

	delete(p.requests, claimUID)
}
