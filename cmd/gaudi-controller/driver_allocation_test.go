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
	"context"
	"reflect"
	"testing"

	resourcev1 "k8s.io/api/resource/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/dynamic-resource-allocation/controller"

	helpers "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/controllerhelpers"
	fakeclient "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gaudi/clientset/versioned/fake"
	GaudiV1alpha1 "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gaudi/v1alpha1"
	intelcrd "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gaudi/v1alpha1/api"
)

func newGAS2xGaudi() *intelcrd.GaudiAllocationStateSpec {
	return &intelcrd.GaudiAllocationStateSpec{
		AllocatableDevices: map[string]intelcrd.AllocatableDevice{
			"duuid1": {UID: "duuid1", Model: "Gaudi2"},
			"duuid2": {UID: "duuid2", Model: "Gaudi2"},
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
		gasspecs            map[string]*intelcrd.GaudiAllocationStateSpec
		expectedAllocations map[string]intelcrd.AllocatedClaims
		expectedErrors      map[string]string
	}

	testcases := []testcaseType{
		{
			name: "successful allocation",
			claims: []*controller.ClaimAllocation{
				{
					Claim:           createFakeClaim("cuuid1"),
					ClaimParameters: &intelcrd.GaudiClaimParametersSpec{Count: 1},
					Class:           &resourcev1.ResourceClass{},
					ClassParameters: &intelcrd.GaudiClassParametersSpec{},
				},
			},
			gasspecs: map[string]*intelcrd.GaudiAllocationStateSpec{
				fakeNodename: newGAS2xGaudi(),
			},
			expectedAllocations: map[string]intelcrd.AllocatedClaims{
				fakeNodename: {
					"cuuid1": {
						Devices: intelcrd.AllocatedDevices{
							{UID: "duuid1"},
						},
					},
				},
			},
			expectedErrors: map[string]string{},
		},
		{
			name: "failed allocation",
			claims: []*controller.ClaimAllocation{
				{
					Claim:           createFakeClaim("cuuid1"),
					ClaimParameters: &intelcrd.GaudiClaimParametersSpec{Count: 3},
					Class:           &resourcev1.ResourceClass{},
					ClassParameters: &intelcrd.GaudiClassParametersSpec{},
				},
			},
			gasspecs: map[string]*intelcrd.GaudiAllocationStateSpec{
				fakeNodename: newGAS2xGaudi(),
			},
			expectedAllocations: map[string]intelcrd.AllocatedClaims{},
			expectedErrors: map[string]string{
				"cuuid1": "no suitable node found",
			},
		},
	}

	for _, testcase := range testcases {
		t.Log(testcase.name)

		fakeDRAClient := fakeclient.NewSimpleClientset()
		driver := createFakeDriver(t, kubefake.NewSimpleClientset(), fakeDRAClient)

		// create GAS for all nodes for test
		for nodename := range testcase.gasspecs {
			fakeGAS := &GaudiV1alpha1.GaudiAllocationState{
				TypeMeta:   metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: nodename},
				Status:     intelcrd.GaudiAllocationStateStatusReady,
			}
			fakeGAS.Spec = *testcase.gasspecs[fakeNodename]
			_, err := fakeDRAClient.GaudiV1alpha1().GaudiAllocationStates(testNamespace).Create(context.TODO(), fakeGAS, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("Could not create GpuClaimParameters for test: %v", err)
			}
		}

		driver.Allocate(context.TODO(), testcase.claims, "")

		for claimUID, expectedError := range testcase.expectedErrors {
			for _, ca := range testcase.claims {
				if string(ca.Claim.UID) == claimUID {
					if ca.Error == nil || ca.Error.Error() != expectedError {
						t.Errorf("Unexpected claim allocation error: %v, expected %v", ca.Error, expectedError)
					}
					break
				}
			}

		}

		// check results by comparing GAS' contents with expectations
		for nodename := range testcase.expectedAllocations {
			gas, err := fakeDRAClient.GaudiV1alpha1().GaudiAllocationStates(testNamespace).Get(
				context.TODO(), nodename, metav1.GetOptions{},
			)
			if err != nil {
				t.Errorf("Could not get GAS: %v", err)
			}
			if !reflect.DeepEqual(
				testcase.expectedAllocations[nodename]["cuuid1"].Devices,
				gas.Spec.AllocatedClaims["cuuid1"].Devices) {
				t.Errorf(
					"wrong result\nexpected allocatedClaims %+v\ngot %+v",
					testcase.expectedAllocations[nodename]["cuuid1"].Devices,
					gas.Spec.AllocatedClaims["cuuid1"].Devices,
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
		gasspecs            map[string]*intelcrd.GaudiAllocationStateSpec
		pendingClaims       []pendingClaim
		expectedAllocations map[string]intelcrd.AllocatedClaims
		expectedErrors      map[string]string
	}

	testcases := []testcaseType{
		{
			name: "successful allocation of pending claim",
			claims: []*controller.ClaimAllocation{
				{
					Claim:           createFakeClaim("cuuid1"),
					ClaimParameters: &intelcrd.GaudiClaimParametersSpec{Count: 1},
					Class:           &resourcev1.ResourceClass{},
					ClassParameters: &intelcrd.GaudiClassParametersSpec{},
				},
			},
			pendingClaims: []pendingClaim{
				{
					claimUID: "cuuid1",
					nodeName: fakeNodename,
					claimAllocation: intelcrd.AllocatedClaim{
						Devices: intelcrd.AllocatedDevices{
							{UID: "duuid1"},
						},
					},
				},
			},
			gasspecs: map[string]*intelcrd.GaudiAllocationStateSpec{
				fakeNodename: newGAS2xGaudi(),
			},
			expectedAllocations: map[string]intelcrd.AllocatedClaims{
				fakeNodename: {
					"cuuid1": {
						Devices: intelcrd.AllocatedDevices{
							{UID: "duuid1"},
						},
					},
				},
			},
			expectedErrors: map[string]string{},
		},
		{
			name: "pending claim invalid, resource occupied, successful new allocation",
			claims: []*controller.ClaimAllocation{
				{
					Claim:           createFakeClaim("cuuid1"),
					ClaimParameters: &intelcrd.GaudiClaimParametersSpec{Count: 1},
					Class:           &resourcev1.ResourceClass{},
					ClassParameters: &intelcrd.GaudiClassParametersSpec{},
				},
			},
			pendingClaims: []pendingClaim{
				{
					claimUID: "cuuid1",
					nodeName: fakeNodename,
					claimAllocation: intelcrd.AllocatedClaim{
						Devices: intelcrd.AllocatedDevices{
							{UID: "duuid1"},
						},
					},
				},
			},
			gasspecs: map[string]*intelcrd.GaudiAllocationStateSpec{
				fakeNodename: {
					AllocatableDevices: map[string]intelcrd.AllocatableDevice{
						"duuid1": {UID: "duuid1"},
						"duuid2": {UID: "duuid2"},
					},
					AllocatedClaims: intelcrd.AllocatedClaims{
						"cuuid2": GaudiV1alpha1.AllocatedClaim{
							Devices: GaudiV1alpha1.AllocatedDevices{
								{UID: "duuid1"},
							},
						},
					},
				},
			},
			expectedAllocations: map[string]intelcrd.AllocatedClaims{
				fakeNodename: {
					"cuuid1": {
						Devices: intelcrd.AllocatedDevices{
							{UID: "duuid2"},
						},
					},
				},
			},
			expectedErrors: map[string]string{},
		},
		{
			name: "pending claim invalid, no resources left",
			claims: []*controller.ClaimAllocation{
				{
					Claim:           createFakeClaim("cuuid1"),
					ClaimParameters: &intelcrd.GaudiClaimParametersSpec{Count: 1},
					Class:           &resourcev1.ResourceClass{},
					ClassParameters: &intelcrd.GaudiClassParametersSpec{},
				},
			},
			pendingClaims: []pendingClaim{
				{
					claimUID: "cuuid1",
					nodeName: fakeNodename,
					claimAllocation: intelcrd.AllocatedClaim{
						Devices: intelcrd.AllocatedDevices{
							{UID: "duuid1"},
						},
					},
				},
			},
			gasspecs: map[string]*intelcrd.GaudiAllocationStateSpec{
				fakeNodename: {
					AllocatableDevices: map[string]intelcrd.AllocatableDevice{
						"duuid1": {UID: "duuid1"},
						"duuid2": {UID: "duuid2"},
					},
					AllocatedClaims: intelcrd.AllocatedClaims{
						"cuuid2": GaudiV1alpha1.AllocatedClaim{
							Devices: GaudiV1alpha1.AllocatedDevices{
								{UID: "duuid1"}, {UID: "duuid2"},
							},
						},
					},
				},
			},
			expectedAllocations: map[string]intelcrd.AllocatedClaims{},
			expectedErrors: map[string]string{
				"cuuid1": "insufficient resources for claim cuuid1 on node fakeNode",
			},
		},
	}

	for _, testcase := range testcases {
		t.Log(testcase.name)

		fakeDRAClient := fakeclient.NewSimpleClientset()
		driver := createFakeDriver(t, kubefake.NewSimpleClientset(), fakeDRAClient)

		for _, pendingClaim := range testcase.pendingClaims {
			driver.PendingClaimRequests.set(pendingClaim.claimUID, pendingClaim.nodeName, pendingClaim.claimAllocation)
		}

		// create GAS for all nodes for test
		for nodename := range testcase.gasspecs {
			fakeGAS := &GaudiV1alpha1.GaudiAllocationState{
				TypeMeta:   metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: nodename},
				Status:     intelcrd.GaudiAllocationStateStatusReady,
				Spec:       *testcase.gasspecs[fakeNodename],
			}
			_, err := fakeDRAClient.GaudiV1alpha1().GaudiAllocationStates(testNamespace).Create(context.TODO(), fakeGAS, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("Could not create GpuClaimParameters for test: %v", err)
			}
		}

		driver.Allocate(context.TODO(), testcase.claims, fakeNodename)

		for claimUID, expectedError := range testcase.expectedErrors {
			claimFound := false
			for _, ca := range testcase.claims {
				if string(ca.Claim.UID) == claimUID {
					if ca.Error == nil || ca.Error.Error() != expectedError {
						t.Errorf("Unexpected error: %v, expected error: %v", ca.Error, expectedError)
					}
					claimFound = true
					break
				}
			}

			if !claimFound {
				t.Errorf("Expected claim %v with error not found in testcase.claims after allocations", claimUID)
			}
		}

		// check results by comparing GAS' contents with expectations
		for nodename := range testcase.expectedAllocations {
			gas, err := fakeDRAClient.GaudiV1alpha1().GaudiAllocationStates(testNamespace).Get(
				context.TODO(), nodename, metav1.GetOptions{},
			)
			if err != nil {
				t.Errorf("Could not get GAS: %v", err)
			}
			if !reflect.DeepEqual(
				testcase.expectedAllocations[nodename]["cuuid1"].Devices,
				gas.Spec.AllocatedClaims["cuuid1"].Devices) {
				t.Errorf(
					"wrong result\nexpected allocatedClaims %+v\ngot %+v",
					testcase.expectedAllocations[nodename]["cuuid1"].Devices,
					gas.Spec.AllocatedClaims["cuuid1"].Devices,
				)
			}
		}
	}
}

func TestDeallocateClaim(t *testing.T) {

	claim := createFakeClaim("cuuid1")
	claim.Status.Allocation = helpers.BuildAllocationResult(fakeNodename, false)

	fakeGAS := &GaudiV1alpha1.GaudiAllocationState{
		TypeMeta:   metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: fakeNodename},
		Status:     intelcrd.GaudiAllocationStateStatusReady,
		Spec:       *newGAS2xGaudi(),
	}
	fakeGAS.Spec.AllocatedClaims = intelcrd.AllocatedClaims{
		"cuuid1": intelcrd.AllocatedClaim{},
	}

	fakeDRAClient := fakeclient.NewSimpleClientset(fakeGAS)
	driver := createFakeDriver(t, kubefake.NewSimpleClientset(), fakeDRAClient)

	if err := driver.Deallocate(context.TODO(), claim); err != nil {
		t.Errorf("Could not Deallocate: %v", err)
	}

	gas, err := fakeDRAClient.GaudiV1alpha1().GaudiAllocationStates(testNamespace).Get(
		context.TODO(), fakeNodename, metav1.GetOptions{},
	)
	if err != nil {
		t.Errorf("Could not get GAS: %v", err)
	}

	if len(gas.Spec.AllocatedClaims["cuuid1"].Devices) != 0 {
		t.Errorf(
			"wrong result, expected no allocatedClaims in GAS, got %v",
			gas.Spec.AllocatedClaims,
		)
	}
}
