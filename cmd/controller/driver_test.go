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
	"context"
	"reflect"
	"testing"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gpu/clientset/versioned"
	gpudrafake "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gpu/clientset/versioned/fake"
	gpuv1alpha2 "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gpu/v1alpha2"
	intelcrd "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gpu/v1alpha2/api"
	v1 "k8s.io/api/core/v1"
	"k8s.io/api/resource/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	"k8s.io/dynamic-resource-allocation/controller"
)

type keyValue struct {
	key   string
	value uint64
}

func createMap(keyValues []keyValue, millicoreValue uint64) map[string]*intelcrd.AllocatableGpu {
	m := map[string]*intelcrd.AllocatableGpu{}
	for _, keyValue := range keyValues {
		m[keyValue.key] = &intelcrd.AllocatableGpu{
			Type:       intelcrd.GpuDeviceType,
			UID:        keyValue.key,
			Memory:     keyValue.value,
			Millicores: millicoreValue,
		}
	}

	return m
}

func TestSelectPotentialGpuDevices(t *testing.T) {
	type testCase struct {
		name            string
		policy          string
		available       map[string]*intelcrd.AllocatableGpu
		consumed        map[string]*intelcrd.AllocatableGpu
		claimAllocation *controller.ClaimAllocation
		expectedResult  stringSearchMap
	}

	testCases := []testCase{
		{
			name:      "none policy success case",
			policy:    "none",
			available: createMap([]keyValue{{"gpu100", 100}, {"gpu1", 1}, {"gpu10", 10}, {"gpu20", 20}, {"gpu30", 30}, {"gpu40", 40}}, 1),
			consumed:  createMap([]keyValue{{"gpu100", 1}, {"gpu1", 1}, {"gpu10", 1}, {"gpu20", 15}, {"gpu30", 1}, {"gpu40", 1}}, 0),
			claimAllocation: &controller.ClaimAllocation{
				Claim: &v1alpha2.ResourceClaim{},
				ClaimParameters: &intelcrd.GpuClaimParametersSpec{
					Type:   intelcrd.GpuDeviceType,
					Count:  4,
					Memory: 6,
				},
				ClassParameters: &intelcrd.GpuClassParametersSpec{
					Shared: true,
				},
			},
			expectedResult: newSearchMap("gpu100", "gpu10", "gpu30", "gpu40"),
		},
		{
			name:      "balanced policy success case",
			policy:    "balanced",
			available: createMap([]keyValue{{"gpu100", 100}, {"gpu1", 1}, {"gpu10", 10}, {"gpu20", 20}, {"gpu30", 30}, {"gpu40", 40}}, 1),
			consumed:  createMap([]keyValue{{"gpu100", 1}, {"gpu1", 1}, {"gpu10", 1}, {"gpu20", 15}, {"gpu30", 1}, {"gpu40", 1}}, 0),
			claimAllocation: &controller.ClaimAllocation{
				Claim: &v1alpha2.ResourceClaim{},
				ClaimParameters: &intelcrd.GpuClaimParametersSpec{
					Type:   intelcrd.GpuDeviceType,
					Count:  2,
					Memory: 5,
				},
				ClassParameters: &intelcrd.GpuClassParametersSpec{
					Shared: true,
				},
			},
			expectedResult: newSearchMap("gpu100", "gpu40"),
		},
		{
			name:      "packed policy success case",
			policy:    "packed",
			available: createMap([]keyValue{{"gpu100", 100}, {"gpu1", 1}, {"gpu10", 10}, {"gpu20", 20}, {"gpu30", 30}, {"gpu40", 40}}, 1),
			consumed:  createMap([]keyValue{{"gpu100", 1}, {"gpu1", 1}, {"gpu10", 1}, {"gpu20", 15}, {"gpu30", 1}, {"gpu40", 1}}, 0),
			claimAllocation: &controller.ClaimAllocation{
				Claim: &v1alpha2.ResourceClaim{},
				ClaimParameters: &intelcrd.GpuClaimParametersSpec{
					Type:   intelcrd.GpuDeviceType,
					Count:  2,
					Memory: 5,
				},
				ClassParameters: &intelcrd.GpuClassParametersSpec{
					Shared: true,
				},
			},
			expectedResult: newSearchMap("gpu20", "gpu10"),
		},
	}

	for _, testCase := range testCases {
		resource := "memory"
		driver := newDriver(&configType{
			clientset: &clientsetType{intel: versioned.New(nil)},
			flags: &flagsType{
				preferredAllocationPolicy: &testCase.policy,
				allocationPolicyResource:  &resource,
			},
		})

		result := driver.selectPotentialGpuDevices(testCase.available, testCase.consumed, testCase.claimAllocation)

		if len(result.Gpus) != len(testCase.expectedResult) {
			t.Errorf("bad length in %v for %v", result, testCase.name)
		}

		for _, gpu := range result.Gpus {
			if !testCase.expectedResult[gpu.UID] {
				t.Errorf("bad result %v for %v, UID %v not found in list of expected %v", result, testCase.name, gpu.UID, testCase.expectedResult)
			}
		}
	}
}

func TestGPUFitsRequest(t *testing.T) {
	type testCase struct {
		testName       string
		request        *intelcrd.GpuClaimParametersSpec
		deviceRefSpec  *intelcrd.AllocatableGpu
		consumed       *intelcrd.AllocatableGpu
		shared         bool
		expectedResult bool
	}

	testCases := []testCase{
		{
			testName: "enough memory",
			request: &intelcrd.GpuClaimParametersSpec{
				Memory: 500,
			},
			deviceRefSpec: &intelcrd.AllocatableGpu{
				Memory: 1000,
			},
			consumed: &intelcrd.AllocatableGpu{
				Memory: 500,
			},
			expectedResult: true,
		},
		{
			testName: "not enough memory",
			request: &intelcrd.GpuClaimParametersSpec{
				Memory: 501,
			},
			deviceRefSpec: &intelcrd.AllocatableGpu{
				Memory: 1000,
			},
			consumed: &intelcrd.AllocatableGpu{
				Memory: 500,
			},
			expectedResult: false,
		},
		{
			testName: "enough millicores",
			request: &intelcrd.GpuClaimParametersSpec{
				Millicores: 500,
			},
			deviceRefSpec: &intelcrd.AllocatableGpu{
				Millicores: 1000,
			},
			consumed: &intelcrd.AllocatableGpu{
				Millicores: 500,
			},
			expectedResult: true,
			shared:         true,
		},
		{
			testName: "not enough millicores",
			request: &intelcrd.GpuClaimParametersSpec{
				Millicores: 501,
			},
			deviceRefSpec: &intelcrd.AllocatableGpu{
				Millicores: 1000,
			},
			consumed: &intelcrd.AllocatableGpu{
				Millicores: 500,
			},
			expectedResult: false,
			shared:         true,
		},
		{
			testName: "Memory ok but Maxvfs != 0",
			request: &intelcrd.GpuClaimParametersSpec{
				Memory: 500,
			},
			deviceRefSpec: &intelcrd.AllocatableGpu{
				Memory: 1000,
			},
			consumed: &intelcrd.AllocatableGpu{
				Memory: 500,
				Maxvfs: 1,
			},
			expectedResult: false,
		},
		{
			testName: "Memory ok but wrong type",
			request: &intelcrd.GpuClaimParametersSpec{
				Memory: 500,
				Type:   "gpu",
			},
			deviceRefSpec: &intelcrd.AllocatableGpu{
				Memory: 1000,
				Type:   "vf",
			},
			consumed: &intelcrd.AllocatableGpu{
				Memory: 500,
			},
			expectedResult: false,
		},
		{
			testName: "Memory ok and vf would fit",
			request: &intelcrd.GpuClaimParametersSpec{
				Memory: 500,
				Type:   "vf",
			},
			deviceRefSpec: &intelcrd.AllocatableGpu{
				Memory: 1000,
				Type:   "gpu",
				Maxvfs: 2,
			},
			consumed: &intelcrd.AllocatableGpu{
				Memory: 0,
				Maxvfs: 0,
			},
			expectedResult: true,
		},
	}

	for _, testCase := range testCases {
		result := gpuFitsRequest(testCase.request, testCase.deviceRefSpec, testCase.consumed, testCase.shared)
		if testCase.expectedResult != result {
			t.Errorf("unexpected validateRequest result %v for test %v. Expected %v.", result, testCase.testName, testCase.expectedResult)
		}
	}
}

func createFakeDriver(t *testing.T, coreclient *kubefake.Clientset, intelclient *gpudrafake.Clientset) *driver {
	t.Helper()

	csconfig := &rest.Config{}
	policy := "none"
	resource := "memory"

	config := &configType{
		flags: &flagsType{
			preferredAllocationPolicy: &policy,
			allocationPolicyResource:  &resource,
		},
		csconfig:  csconfig,
		namespace: "nsname",
		clientset: &clientsetType{
			coreclient,
			intelclient,
		},
	}

	return newDriver(config)
}

func createFakeClaimAllocations(classParam *intelcrd.GpuClassParametersSpec, claimParam *intelcrd.GpuClaimParametersSpec) []*controller.ClaimAllocation {
	return []*controller.ClaimAllocation{
		{
			PodClaimName: "foo",
			Claim: &v1alpha2.ResourceClaim{
				TypeMeta:   metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{},
				Spec:       v1alpha2.ResourceClaimSpec{},
				Status:     v1alpha2.ResourceClaimStatus{},
			},
			Class: &v1alpha2.ResourceClass{
				TypeMeta:      metav1.TypeMeta{},
				ObjectMeta:    metav1.ObjectMeta{},
				DriverName:    "",
				ParametersRef: &v1alpha2.ResourceClassParametersReference{},
				SuitableNodes: &v1.NodeSelector{
					NodeSelectorTerms: []v1.NodeSelectorTerm{
						{
							MatchExpressions: []v1.NodeSelectorRequirement{
								{
									Key:    "kubernetes.io/hostname",
									Values: []string{"foo"},
								},
							},
						},
					},
				},
			},

			ClaimParameters: claimParam,
			ClassParameters: classParam,
			UnsuitableNodes: []string{},
			Allocation: &v1alpha2.AllocationResult{
				ResourceHandles:  []v1alpha2.ResourceHandle{},
				AvailableOnNodes: &v1.NodeSelector{},
				Shareable:        false,
			},
			Error: nil,
		},
	}
}

func TestUnsuitableNodes(t *testing.T) {

	type testCase struct {
		testName                string
		gasNs                   string
		gasStatus               string
		potentialNodes          []string
		expectedUnsuitableNodes []string
		class                   *intelcrd.GpuClassParametersSpec
		claim                   *intelcrd.GpuClaimParametersSpec
	}
	testCases := []testCase{
		{
			testName:                "aok",
			gasNs:                   "nsname",
			gasStatus:               "Ready",
			potentialNodes:          []string{"foo"},
			expectedUnsuitableNodes: nil,
			class: &intelcrd.GpuClassParametersSpec{
				DeviceSelector: []gpuv1alpha2.DeviceSelector{},
				Monitor:        false,
				Shared:         false,
			},
			claim: &intelcrd.GpuClaimParametersSpec{},
		},
		{
			testName:                "wrong namespace",
			gasNs:                   "wrong nsname",
			gasStatus:               intelcrd.GpuAllocationStateStatusReady,
			potentialNodes:          []string{"foo"},
			expectedUnsuitableNodes: []string{"foo"},
			class: &intelcrd.GpuClassParametersSpec{
				DeviceSelector: []gpuv1alpha2.DeviceSelector{},
				Monitor:        false,
				Shared:         false,
			},
			claim: &intelcrd.GpuClaimParametersSpec{},
		},
		{
			testName:                "correct namespace, wrong status",
			gasNs:                   "nsname",
			gasStatus:               intelcrd.GpuAllocationStateStatusNotReady,
			potentialNodes:          []string{"foo"},
			expectedUnsuitableNodes: []string{"foo"},
			class: &intelcrd.GpuClassParametersSpec{
				DeviceSelector: []gpuv1alpha2.DeviceSelector{},
				Monitor:        false,
				Shared:         false,
			},
			claim: &intelcrd.GpuClaimParametersSpec{},
		},
	}

	for _, testCase := range testCases {
		fakeGas := &gpuv1alpha2.GpuAllocationState{
			TypeMeta:   metav1.TypeMeta{},
			ObjectMeta: metav1.ObjectMeta{Namespace: testCase.gasNs, Name: "foo"},
			Status:     testCase.gasStatus,
		}
		fakeDRAClient := gpudrafake.NewSimpleClientset(fakeGas)
		driver := createFakeDriver(t, kubefake.NewSimpleClientset(), fakeDRAClient)
		allcas := createFakeClaimAllocations(testCase.class, testCase.claim)

		fakePod := v1.Pod{}

		err := driver.UnsuitableNodes(context.TODO(), &fakePod, allcas, testCase.potentialNodes)

		if err != nil {
			t.Errorf("unexpected result to UnsuitableNodes:%v (expected nil)", err)
		}

		resultUnsuitable := allcas[0].UnsuitableNodes
		if !reflect.DeepEqual(resultUnsuitable, testCase.expectedUnsuitableNodes) {
			t.Errorf("unexpected result to UnsuitableNodes:%v expected:%v", resultUnsuitable, testCase.expectedUnsuitableNodes)
		}
	}
}
