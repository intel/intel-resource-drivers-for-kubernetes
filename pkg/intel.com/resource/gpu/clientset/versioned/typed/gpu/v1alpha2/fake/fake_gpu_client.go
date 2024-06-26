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

// Code generated by client-gen. DO NOT EDIT.

package fake

import (
	v1alpha2 "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gpu/clientset/versioned/typed/gpu/v1alpha2"
	rest "k8s.io/client-go/rest"
	testing "k8s.io/client-go/testing"
)

type FakeGpuV1alpha2 struct {
	*testing.Fake
}

func (c *FakeGpuV1alpha2) GpuAllocationStates(namespace string) v1alpha2.GpuAllocationStateInterface {
	return &FakeGpuAllocationStates{c, namespace}
}

func (c *FakeGpuV1alpha2) GpuClaimParameters(namespace string) v1alpha2.GpuClaimParametersInterface {
	return &FakeGpuClaimParameters{c, namespace}
}

func (c *FakeGpuV1alpha2) GpuClassParameters() v1alpha2.GpuClassParametersInterface {
	return &FakeGpuClassParameters{c}
}

// RESTClient returns a RESTClient that is used to communicate
// with API server by this client implementation.
func (c *FakeGpuV1alpha2) RESTClient() rest.Interface {
	var ret *rest.RESTClient
	return ret
}
