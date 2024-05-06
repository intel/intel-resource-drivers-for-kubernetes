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

	resourcev1 "k8s.io/api/resource/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/dynamic-resource-allocation/controller"

	helpers "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/controllerhelpers"
	fakeclient "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gpu/clientset/versioned/fake"
	gpuv1alpha2 "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gpu/v1alpha2"
	intelcrd "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gpu/v1alpha2/api"
)

func newAllocatableFlex140(duuid string) intelcrd.AllocatableGpu {
	return intelcrd.AllocatableGpu{
		Ecc:        true,
		Maxvfs:     12,
		Memory:     5068,
		Millicores: 1000,
		Model:      "0x56c1",
		ParentUID:  "",
		Type:       "gpu",
		UID:        duuid,
		VFIndex:    0,
	}
}

func newGAS2xFlex140() *intelcrd.GpuAllocationStateSpec {
	return &intelcrd.GpuAllocationStateSpec{
		AllocatableDevices: map[string]intelcrd.AllocatableGpu{
			"duuid1": newAllocatableFlex140("duuid1"),
			"duuid2": newAllocatableFlex140("duuid2"),
		},
	}
}

func createFakeClaim(cuuid string) *resourcev1.ResourceClaim {
	newClaim := &resourcev1.ResourceClaim{}
	newClaim.UID = types.UID(cuuid)
	return newClaim
}

func TestAllocateImmediate(t *testing.T) {

	type testcaseType struct {
		name                string
		claims              []*controller.ClaimAllocation
		gasspecs            map[string]*intelcrd.GpuAllocationStateSpec
		expectedAllocations map[string]intelcrd.AllocatedClaims
	}

	testcases := []testcaseType{
		{
			name: "one",
			claims: []*controller.ClaimAllocation{
				{
					Claim: createFakeClaim("cuuid1"),
					ClaimParameters: &intelcrd.GpuClaimParametersSpec{
						Count: 1,
						Type:  "gpu",
					},
					Class:           &resourcev1.ResourceClass{},
					ClassParameters: &intelcrd.GpuClassParametersSpec{},
				},
			},
			gasspecs: map[string]*intelcrd.GpuAllocationStateSpec{
				fakeNodeName: newGAS2xFlex140(),
			},
			expectedAllocations: map[string]intelcrd.AllocatedClaims{
				fakeNodeName: {
					"cuuid1": {
						Gpus: intelcrd.AllocatedGpus{
							{UID: "duuid1", Memory: 5068, Millicores: 1000, Type: "gpu"},
						},
					},
				},
			},
		},
	}

	for _, testcase := range testcases {
		t.Log(testcase.name)

		fakeDRAClient := fakeclient.NewSimpleClientset()
		driver := createFakeDriver(t, kubefake.NewSimpleClientset(), fakeDRAClient)

		// create GAS for all nodes for test
		for nodename := range testcase.gasspecs {
			fakeGAS := &gpuv1alpha2.GpuAllocationState{
				TypeMeta:   metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{Namespace: testNameSpace, Name: nodename},
				Status:     intelcrd.GpuAllocationStateStatusReady,
			}
			fakeGAS.Spec = *testcase.gasspecs[fakeNodeName]
			_, err := fakeDRAClient.GpuV1alpha2().GpuAllocationStates(testNameSpace).Create(context.TODO(), fakeGAS, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("Could not create GpuClaimParameters for test: %v", err)
			}
		}

		driver.Allocate(context.TODO(), testcase.claims, "")

		// check results by comparing GAS' contents with expectations
		for nodename := range testcase.expectedAllocations {
			gas, err := fakeDRAClient.GpuV1alpha2().GpuAllocationStates(testNameSpace).Get(
				context.TODO(), nodename, metav1.GetOptions{},
			)
			if err != nil {
				t.Errorf("Could not get GAS: %v", err)
			}
			if !reflect.DeepEqual(
				testcase.expectedAllocations[nodename]["cuuid1"].Gpus,
				gas.Spec.AllocatedClaims["cuuid1"].Gpus) {
				t.Errorf(
					"wrong result\nexpected allocatedClaims %+v\ngot %+v",
					testcase.expectedAllocations[nodename]["cuuid1"].Gpus,
					gas.Spec.AllocatedClaims["cuuid1"].Gpus,
				)
			}
		}
	}
}

func TestAllocatePending(t *testing.T) {

	type pendingClaim struct {
		claimUID        string
		nodeName        string
		claimAllocation intelcrd.AllocatedClaim
	}
	type testcaseType struct {
		name                string
		claims              []*controller.ClaimAllocation
		gasspecs            map[string]*intelcrd.GpuAllocationStateSpec
		pendingClaims       []pendingClaim
		expectedAllocations map[string]intelcrd.AllocatedClaims
		policy              string
		resource            string
	}

	testcases := []testcaseType{
		{
			name: "successful allocation of pending claim",
			claims: []*controller.ClaimAllocation{
				{
					Claim:           createFakeClaim("cuuid1"),
					ClaimParameters: &intelcrd.GpuClaimParametersSpec{Count: 1, Type: "gpu"},
					Class:           &resourcev1.ResourceClass{},
					ClassParameters: &intelcrd.GpuClassParametersSpec{},
				},
			},
			pendingClaims: []pendingClaim{
				{
					claimUID: "cuuid1",
					nodeName: fakeNodeName,
					claimAllocation: intelcrd.AllocatedClaim{
						Gpus: intelcrd.AllocatedGpus{
							{UID: "duuid1", Memory: 5068, Millicores: 1000, Type: "gpu"},
						},
					},
				},
			},
			gasspecs: map[string]*intelcrd.GpuAllocationStateSpec{
				fakeNodeName: newGAS2xFlex140(),
			},
			expectedAllocations: map[string]intelcrd.AllocatedClaims{
				fakeNodeName: {
					"cuuid1": {
						Gpus: intelcrd.AllocatedGpus{
							{UID: "duuid1", Memory: 5068, Millicores: 1000, Type: "gpu"},
						},
					},
				},
			},
			policy:   "none",
			resource: "memory",
		},
		{
			name: "pending claim invalid, resource occupied, successful new allocation",
			claims: []*controller.ClaimAllocation{
				{
					Claim:           createFakeClaim("cuuid1"),
					ClaimParameters: &intelcrd.GpuClaimParametersSpec{Count: 1, Type: "gpu"},
					Class:           &resourcev1.ResourceClass{},
					ClassParameters: &intelcrd.GpuClassParametersSpec{},
				},
			},
			pendingClaims: []pendingClaim{
				{
					claimUID: "cuuid1",
					nodeName: fakeNodeName,
					claimAllocation: intelcrd.AllocatedClaim{
						Gpus: intelcrd.AllocatedGpus{
							{UID: "duuid1", Memory: 5068, Millicores: 1000, Type: "gpu"},
						},
					},
				},
			},
			gasspecs: map[string]*intelcrd.GpuAllocationStateSpec{
				fakeNodeName: {
					AllocatableDevices: map[string]intelcrd.AllocatableGpu{
						"duuid1": newAllocatableFlex140("duuid1"),
						"duuid2": newAllocatableFlex140("duuid2"),
					},
					AllocatedClaims: intelcrd.AllocatedClaims{
						"cuuid2": gpuv1alpha2.AllocatedClaim{
							Gpus: gpuv1alpha2.AllocatedGpus{
								{UID: "duuid1", Memory: 5068, Millicores: 1000, Type: "gpu"},
							},
						},
					},
				},
			},
			expectedAllocations: map[string]intelcrd.AllocatedClaims{
				fakeNodeName: {
					"cuuid1": {
						Gpus: intelcrd.AllocatedGpus{
							{UID: "duuid2", Memory: 5068, Millicores: 1000, Type: "gpu"},
						},
					},
				},
			},
			policy:   "none",
			resource: "memory",
		},
		{
			name: "pending claim invalid, not matching balanced policy, successful new allocation",
			claims: []*controller.ClaimAllocation{
				{
					Claim:           createFakeClaim("cuuid1"),
					ClaimParameters: &intelcrd.GpuClaimParametersSpec{Count: 1, Type: "gpu", Memory: 1024, Millicores: 100},
					Class:           &resourcev1.ResourceClass{},
					ClassParameters: &intelcrd.GpuClassParametersSpec{Shared: true},
				},
			},
			pendingClaims: []pendingClaim{
				{
					claimUID: "cuuid1",
					nodeName: fakeNodeName,
					claimAllocation: intelcrd.AllocatedClaim{
						Gpus: intelcrd.AllocatedGpus{
							{UID: "duuid1", Memory: 1024, Millicores: 100, Type: "gpu"},
						},
					},
				},
			},
			gasspecs: map[string]*intelcrd.GpuAllocationStateSpec{
				fakeNodeName: {
					AllocatableDevices: map[string]intelcrd.AllocatableGpu{
						"duuid1": newAllocatableFlex140("duuid1"),
						"duuid2": newAllocatableFlex140("duuid2"),
					},
					AllocatedClaims: intelcrd.AllocatedClaims{
						"cuuid2": gpuv1alpha2.AllocatedClaim{
							Gpus: gpuv1alpha2.AllocatedGpus{
								{UID: "duuid1", Memory: 1024, Millicores: 100, Type: "gpu"},
							},
						},
					},
				},
			},
			expectedAllocations: map[string]intelcrd.AllocatedClaims{
				fakeNodeName: {
					"cuuid1": {
						Gpus: intelcrd.AllocatedGpus{
							{UID: "duuid2", Memory: 1024, Millicores: 100, Type: "gpu"},
						},
					},
				},
			},
			policy:   "balanced",
			resource: "memory",
		},
		{
			name: "pending claim invalid, not matching packed policy, successful new allocation",
			claims: []*controller.ClaimAllocation{
				{
					Claim:           createFakeClaim("cuuid1"),
					ClaimParameters: &intelcrd.GpuClaimParametersSpec{Count: 1, Type: "gpu", Memory: 1024, Millicores: 100},
					Class:           &resourcev1.ResourceClass{},
					ClassParameters: &intelcrd.GpuClassParametersSpec{Shared: true},
				},
			},
			pendingClaims: []pendingClaim{
				{
					claimUID: "cuuid1",
					nodeName: fakeNodeName,
					claimAllocation: intelcrd.AllocatedClaim{
						Gpus: intelcrd.AllocatedGpus{
							{UID: "duuid1", Memory: 1024, Millicores: 100, Type: "gpu"},
						},
					},
				},
			},
			gasspecs: map[string]*intelcrd.GpuAllocationStateSpec{
				fakeNodeName: {
					AllocatableDevices: map[string]intelcrd.AllocatableGpu{
						"duuid1": newAllocatableFlex140("duuid1"),
						"duuid2": newAllocatableFlex140("duuid2"),
					},
					AllocatedClaims: intelcrd.AllocatedClaims{
						"cuuid2": gpuv1alpha2.AllocatedClaim{
							Gpus: gpuv1alpha2.AllocatedGpus{
								{UID: "duuid2", Memory: 1024, Millicores: 100, Type: "gpu"},
							},
						},
					},
				},
			},
			expectedAllocations: map[string]intelcrd.AllocatedClaims{
				fakeNodeName: {
					"cuuid1": {
						Gpus: intelcrd.AllocatedGpus{
							{UID: "duuid2", Memory: 1024, Millicores: 100, Type: "gpu"},
						},
					},
				},
			},
			policy:   "packed",
			resource: "memory",
		},
	}

	for _, testcase := range testcases {
		t.Log(testcase.name)

		fakeDRAClient := fakeclient.NewSimpleClientset()
		driver := createFakeDriverWithPolicy(t, kubefake.NewSimpleClientset(), fakeDRAClient, testcase.policy, testcase.resource)

		for _, pendingClaim := range testcase.pendingClaims {
			driver.PendingClaimRequests.set(pendingClaim.claimUID, pendingClaim.nodeName, pendingClaim.claimAllocation)
		}

		// create GAS for all nodes for test
		for nodename := range testcase.gasspecs {
			fakeGAS := &gpuv1alpha2.GpuAllocationState{
				TypeMeta:   metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{Namespace: testNameSpace, Name: nodename},
				Status:     intelcrd.GpuAllocationStateStatusReady,
				Spec:       *testcase.gasspecs[fakeNodeName],
			}
			_, err := fakeDRAClient.GpuV1alpha2().GpuAllocationStates(testNameSpace).Create(context.TODO(), fakeGAS, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("Could not create GpuClaimParameters for test: %v", err)
			}
		}

		driver.Allocate(context.TODO(), testcase.claims, fakeNodeName)

		// check results by comparing GAS' contents with expectations
		for nodename := range testcase.expectedAllocations {
			gas, err := fakeDRAClient.GpuV1alpha2().GpuAllocationStates(testNameSpace).Get(
				context.TODO(), nodename, metav1.GetOptions{},
			)
			if err != nil {
				t.Errorf("Could not get GAS: %v", err)
			}
			if !reflect.DeepEqual(
				testcase.expectedAllocations[nodename]["cuuid1"].Gpus,
				gas.Spec.AllocatedClaims["cuuid1"].Gpus) {
				t.Errorf(
					"wrong result\nexpected allocatedClaims %+v\ngot %+v",
					testcase.expectedAllocations[nodename]["cuuid1"].Gpus,
					gas.Spec.AllocatedClaims["cuuid1"].Gpus,
				)
			}
		}
	}
}

func TestDeallocateClaim(t *testing.T) {

	claim := createFakeClaim("cuuid1")
	claim.Status.Allocation = helpers.BuildAllocationResult(fakeNodeName, false)

	fakeGAS := &gpuv1alpha2.GpuAllocationState{
		TypeMeta:   metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{Namespace: testNameSpace, Name: fakeNodeName},
		Status:     intelcrd.GpuAllocationStateStatusReady,
		Spec:       *newGAS2xFlex140(),
	}
	fakeGAS.Spec.AllocatedClaims = intelcrd.AllocatedClaims{
		"cuuid1": intelcrd.AllocatedClaim{},
	}

	fakeDRAClient := fakeclient.NewSimpleClientset(fakeGAS)
	driver := createFakeDriver(t, kubefake.NewSimpleClientset(), fakeDRAClient)

	if err := driver.Deallocate(context.TODO(), claim); err != nil {
		t.Errorf("Could not Deallocate: %v", err)
	}

	gas, err := fakeDRAClient.GpuV1alpha2().GpuAllocationStates(testNameSpace).Get(
		context.TODO(), fakeNodeName, metav1.GetOptions{},
	)
	if err != nil {
		t.Errorf("Could not get GAS: %v", err)
	}

	if len(gas.Spec.AllocatedClaims["cuuid1"].Gpus) != 0 {
		t.Errorf(
			"wrong result, expected no allocatedClaims in GAS, got %v",
			gas.Spec.AllocatedClaims,
		)
	}
}
