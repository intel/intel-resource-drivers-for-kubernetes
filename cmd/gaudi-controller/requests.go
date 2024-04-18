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

package main

import (
	"sync"

	intelcrd "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gaudi/v1alpha1/api"
	"k8s.io/klog/v2"
)

/*
	perNodeClaimRequests is a map {
		claim-uid : map {
			nodename : intelcrd.AllocatedClaim {
				Devices AllocatedDevices `json:"devices"`
			}
		}
	}
*/
type perNodeClaimRequests struct {
	sync.RWMutex
	requests map[string]map[string]intelcrd.AllocatedClaim
}

func newPerNodeClaimRequests() *perNodeClaimRequests {
	return &perNodeClaimRequests{
		requests: make(map[string]map[string]intelcrd.AllocatedClaim),
	}
}

func (p *perNodeClaimRequests) exists(claimUID, node string) bool {
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

func (p *perNodeClaimRequests) get(claimUID, node string) intelcrd.AllocatedClaim {
	p.RLock()
	defer p.RUnlock()

	if !p.exists(claimUID, node) {
		return intelcrd.AllocatedClaim{}
	}
	return p.requests[claimUID][node]
}

func (p *perNodeClaimRequests) cleanupNode(gas *intelcrd.GaudiAllocationState) {
	p.RLock()
	klog.V(5).Infof("Cleaning up resource requests for node %v", gas.Name)
	for claimUID := range p.requests {
		// if the resource claim was suitable for GAS node
		if _, exists := p.requests[claimUID][gas.Name]; exists {
			// cleanup processed claim requests
			if _, exists := gas.Spec.AllocatedClaims[claimUID]; exists {
				delete(p.requests, claimUID)
			}
		}
	}
	p.RUnlock()
}

func (p *perNodeClaimRequests) set(claimUID, node string, devices intelcrd.AllocatedClaim) {
	p.Lock()
	defer p.Unlock()

	_, exists := p.requests[claimUID]
	if !exists {
		p.requests[claimUID] = make(map[string]intelcrd.AllocatedClaim)
	}

	p.requests[claimUID][node] = devices
}

func (p *perNodeClaimRequests) remove(claimUID string) {
	p.Lock()
	defer p.Unlock()

	delete(p.requests, claimUID)
}
