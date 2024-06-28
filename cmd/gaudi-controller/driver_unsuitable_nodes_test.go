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
	"fmt"
	"reflect"
	"testing"

	fakecs "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gaudi/clientset/versioned/fake"
	gaudiv1alpha1 "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gaudi/v1alpha1"
	intelcrd "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gaudi/v1alpha1/api"
	v1 "k8s.io/api/core/v1"
	"k8s.io/api/resource/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	"k8s.io/dynamic-resource-allocation/controller"
)

const (
	testNamespace = "nsname"
	fakeNodename  = "fakeNode"
)

func createFakeDriver(t *testing.T, coreclient *kubefake.Clientset, intelclient *fakecs.Clientset) *driver {
	t.Helper()

	csconfig := &rest.Config{}

	config := &configType{
		csconfig:  csconfig,
		namespace: testNamespace,
		clientsets: &clientsetType{
			coreclient,
			intelclient,
		},
	}

	return newDriver(config)
}

func createFakeClaimAllocation(classParam *intelcrd.GaudiClassParametersSpec, claimParam *intelcrd.GaudiClaimParametersSpec) controller.ClaimAllocation {
	return controller.ClaimAllocation{
		PodClaimName: "fakeClaim",
		Claim: &v1alpha2.ResourceClaim{
			TypeMeta:   metav1.TypeMeta{},
			ObjectMeta: metav1.ObjectMeta{},
			Spec:       v1alpha2.ResourceClaimSpec{},
			Status:     v1alpha2.ResourceClaimStatus{},
		},
		Class: &v1alpha2.ResourceClass{
			TypeMeta:      metav1.TypeMeta{},
			ObjectMeta:    metav1.ObjectMeta{},
			DriverName:    "fakeDriver",
			ParametersRef: &v1alpha2.ResourceClassParametersReference{},
			SuitableNodes: &v1.NodeSelector{
				NodeSelectorTerms: []v1.NodeSelectorTerm{
					{
						MatchExpressions: []v1.NodeSelectorRequirement{
							{
								Key:    "kubernetes.io/hostname",
								Values: []string{fakeNodename},
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
	}
}

func createAllocatable(gaudiUIDs []string) map[string]intelcrd.AllocatableDevice {
	allocatable := make(map[string]intelcrd.AllocatableDevice, len(gaudiUIDs))
	for _, uid := range gaudiUIDs {
		allocatable[uid] = intelcrd.AllocatableDevice{UID: uid}
	}
	return allocatable
}

func createTaints(uids []string) map[string]intelcrd.TaintedDevice {
	taints := make(map[string]intelcrd.TaintedDevice, len(uids))
	reasons := make(map[string]bool)
	reasons["taintReason"] = true
	for _, uid := range uids {
		taints[uid] = intelcrd.TaintedDevice{Reasons: reasons}
	}
	return taints
}

func TestUnsuitableNodes(t *testing.T) {
	availableGaudis := createAllocatable([]string{"gaudi-A", "gaudi-B", "gaudi-C", "gaudi-D"})
	taintedGaudis := createTaints([]string{"gaudi-A", "gaudi-C"})

	AllocatableDeviceCount := len(availableGaudis) - len(taintedGaudis)

	// claim that fits to all available fake devices
	basicClaim := &intelcrd.GaudiClaimParametersSpec{Count: 1}

	defaultClass := &intelcrd.GaudiClassParametersSpec{
		DeviceSelector: []gaudiv1alpha1.DeviceSelector{},
		Monitor:        false,
	}

	monitorClass := &intelcrd.GaudiClassParametersSpec{
		DeviceSelector: []gaudiv1alpha1.DeviceSelector{},
		Monitor:        true,
	}

	type testCase struct {
		name                    string
		gasNs                   string
		gasStatus               string
		potentialNodes          []string
		expectedUnsuitableNodes []string
		class                   *intelcrd.GaudiClassParametersSpec
		claim                   *intelcrd.GaudiClaimParametersSpec
		claimCount              int
		expectedDeviceCount     int
	}

	testCases := []testCase{
		{
			name:                    "all OK with all available devices",
			gasNs:                   testNamespace,
			gasStatus:               intelcrd.GaudiAllocationStateStatusReady,
			potentialNodes:          []string{fakeNodename},
			expectedUnsuitableNodes: nil,
			class:                   defaultClass,
			claim:                   basicClaim,
			claimCount:              AllocatableDeviceCount,
			expectedDeviceCount:     AllocatableDeviceCount,
		},
		{
			name:                    "requesting more than is untainted",
			gasNs:                   testNamespace,
			gasStatus:               intelcrd.GaudiAllocationStateStatusReady,
			potentialNodes:          []string{fakeNodename},
			expectedUnsuitableNodes: []string{fakeNodename},
			class:                   defaultClass,
			claim:                   basicClaim,
			claimCount:              AllocatableDeviceCount + 1,
			expectedDeviceCount:     0,
		},
		{
			name:                    "wrong namespace",
			gasNs:                   "wrong " + testNamespace,
			gasStatus:               intelcrd.GaudiAllocationStateStatusReady,
			potentialNodes:          []string{fakeNodename},
			expectedUnsuitableNodes: []string{fakeNodename},
			class:                   defaultClass,
			claim:                   basicClaim,
			claimCount:              1,
			expectedDeviceCount:     0,
		},
		{
			name:                    "correct namespace, wrong status",
			gasNs:                   testNamespace,
			gasStatus:               intelcrd.GaudiAllocationStateStatusNotReady,
			potentialNodes:          []string{fakeNodename},
			expectedUnsuitableNodes: []string{fakeNodename},
			class:                   defaultClass,
			claim:                   basicClaim,
			claimCount:              1,
			expectedDeviceCount:     0,
		},
		{
			// controller skips allocation checks for monitors, so claim count
			// does not matter and no GPUs are assigned (that's done by kubelet),
			// monitor will get any requested GPU node...
			name:                    "monitor, for all devices including tainted",
			gasNs:                   testNamespace,
			gasStatus:               intelcrd.GaudiAllocationStateStatusReady,
			potentialNodes:          []string{fakeNodename},
			expectedUnsuitableNodes: nil,
			class:                   monitorClass,
			claim:                   basicClaim,
			claimCount:              len(availableGaudis) * 2,
			expectedDeviceCount:     0,
		},
		{
			// ...as long as namespace and kubelet plugin GAS status are fine
			name:                    "monitor, wrong namespace",
			gasNs:                   "wrong " + testNamespace,
			gasStatus:               intelcrd.GaudiAllocationStateStatusReady,
			potentialNodes:          []string{fakeNodename},
			expectedUnsuitableNodes: []string{fakeNodename},
			class:                   monitorClass,
			claim:                   basicClaim,
			claimCount:              1,
			expectedDeviceCount:     0,
		},
		{
			name:                    "monitor, wrong status",
			gasNs:                   testNamespace,
			gasStatus:               intelcrd.GaudiAllocationStateStatusNotReady,
			potentialNodes:          []string{fakeNodename},
			expectedUnsuitableNodes: []string{fakeNodename},
			class:                   monitorClass,
			claim:                   basicClaim,
			claimCount:              1,
			expectedDeviceCount:     0,
		},
	}

	for _, testcase := range testCases {
		fakeGas := &gaudiv1alpha1.GaudiAllocationState{
			TypeMeta:   metav1.TypeMeta{},
			ObjectMeta: metav1.ObjectMeta{Namespace: testcase.gasNs, Name: fakeNodename},
			Status:     testcase.gasStatus,
		}
		fakeGas.Spec.AllocatableDevices = availableGaudis
		fakeGas.Spec.TaintedDevices = taintedGaudis

		fakeDRAClient := fakecs.NewSimpleClientset(fakeGas)
		driver := createFakeDriver(t, kubefake.NewSimpleClientset(), fakeDRAClient)

		allcas := []*controller.ClaimAllocation{}
		for i := 0; i < testcase.claimCount; i++ {
			ca := createFakeClaimAllocation(testcase.class, testcase.claim)
			ca.Claim.UID = types.UID(fmt.Sprintf("claim-%d", i))
			allcas = append(allcas, &ca)
		}

		t.Log(testcase.name)

		fakePod := v1.Pod{}
		ctx := context.TODO()
		if err := driver.UnsuitableNodes(ctx, &fakePod, allcas, testcase.potentialNodes); err != nil {
			t.Errorf("%v: ERROR, from UnsuitableNodes(): %v", testcase.name, err)
			continue
		}

		resultUnsuitable := allcas[0].UnsuitableNodes
		if !reflect.DeepEqual(resultUnsuitable, testcase.expectedUnsuitableNodes) {
			t.Errorf("%v: ERROR, UnsuitableNodes=%#v, expected=%#v", testcase.name, resultUnsuitable, testcase.expectedUnsuitableNodes)
			continue
		}

		devicesCount := 0
		for _, nodes := range driver.PendingClaimRequests.requests {
			for _, claim := range nodes {
				devicesCount += len(claim.Devices)
			}
		}

		if devicesCount != testcase.expectedDeviceCount {
			t.Logf("driver.PendingClaimRequests: %+v", driver.PendingClaimRequests)
			t.Errorf("ERROR, devices count=%d, expected=%d", devicesCount, testcase.expectedDeviceCount)
		}
	}
}
