/*
 * Copyright (c) 2023-2024, Intel Corporation.  All Rights Reserved.
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
	"maps"
	"os"
	"reflect"
	"testing"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gpu/clientset/versioned"
	gpucsfake "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gpu/clientset/versioned/fake"
	gpuv1alpha2 "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gpu/v1alpha2"
	intelcrd "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gpu/v1alpha2/api"
	v1 "k8s.io/api/core/v1"
	"k8s.io/api/resource/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	"k8s.io/dynamic-resource-allocation/controller"
	"k8s.io/klog/v2"
)

const (
	testNameSpace = "nsname"
	fakeNodeName  = "fakeNode"
)

type keyValue struct {
	key   string
	value uint64
}

func createMapGpu(keyValues []keyValue, millicoreValue uint64, maxVfs uint64) map[string]*intelcrd.AllocatableGpu {
	m := map[string]*intelcrd.AllocatableGpu{}
	for _, keyValue := range keyValues {
		m[keyValue.key] = &intelcrd.AllocatableGpu{
			Type:       intelcrd.GpuDeviceType,
			UID:        keyValue.key,
			Memory:     keyValue.value,
			Millicores: millicoreValue,
			Maxvfs:     maxVfs,
		}
	}

	return m
}

func createMapVF(parentUID string, keyValues []keyValue) map[string]*intelcrd.AllocatableGpu {
	m := map[string]*intelcrd.AllocatableGpu{}
	for _, keyValue := range keyValues {
		m[keyValue.key] = &intelcrd.AllocatableGpu{
			Type:      intelcrd.VfDeviceType,
			UID:       keyValue.key,
			Memory:    keyValue.value,
			ParentUID: parentUID,
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
			available: createMapGpu([]keyValue{{"gpu100", 100}, {"gpu1", 1}, {"gpu10", 10}, {"gpu20", 20}, {"gpu30", 30}, {"gpu40", 40}}, 1, 0),
			consumed:  createMapGpu([]keyValue{{"gpu100", 1}, {"gpu1", 1}, {"gpu10", 1}, {"gpu20", 15}, {"gpu30", 1}, {"gpu40", 1}}, 0, 0),
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
			available: createMapGpu([]keyValue{{"gpu100", 100}, {"gpu1", 1}, {"gpu10", 10}, {"gpu20", 20}, {"gpu30", 30}, {"gpu40", 40}}, 1, 0),
			consumed:  createMapGpu([]keyValue{{"gpu100", 1}, {"gpu1", 1}, {"gpu10", 1}, {"gpu20", 15}, {"gpu30", 1}, {"gpu40", 1}}, 0, 0),
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
			available: createMapGpu([]keyValue{{"gpu100", 100}, {"gpu1", 1}, {"gpu10", 10}, {"gpu20", 20}, {"gpu30", 30}, {"gpu40", 40}}, 1, 0),
			consumed:  createMapGpu([]keyValue{{"gpu100", 1}, {"gpu1", 1}, {"gpu10", 1}, {"gpu20", 15}, {"gpu30", 1}, {"gpu40", 1}}, 0, 0),
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

		t.Log(testCase.name)

		devices := driver.selectPotentialGpuDevices(testCase.available, testCase.consumed, testCase.claimAllocation)

		result := intelcrd.AllocatedClaim{
			Gpus: devices,
		}

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

func createFakeDriver(t *testing.T, coreclient *kubefake.Clientset, intelclient *gpucsfake.Clientset) *driver {
	return createFakeDriverWithPolicy(t, coreclient, intelclient, "none", "memory")
}

func createFakeDriverWithPolicy(t *testing.T, coreclient *kubefake.Clientset, intelclient *gpucsfake.Clientset, policy string, resource string) *driver {
	t.Helper()

	csconfig := &rest.Config{}

	config := &configType{
		flags: &flagsType{
			preferredAllocationPolicy: &policy,
			allocationPolicyResource:  &resource,
		},
		csconfig:  csconfig,
		namespace: testNameSpace,
		clientset: &clientsetType{
			coreclient,
			intelclient,
		},
	}

	return newDriver(config)
}

func createFakeClaimAllocation(classParam *intelcrd.GpuClassParametersSpec, claimParam *intelcrd.GpuClaimParametersSpec) controller.ClaimAllocation {
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
								Values: []string{fakeNodeName},
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

func createAllocatable(gpus map[string]*intelcrd.AllocatableGpu) map[string]intelcrd.AllocatableGpu {
	allocatable := make(map[string]intelcrd.AllocatableGpu, len(gpus))
	for uid, gpu := range gpus {
		allocatable[uid] = *gpu
	}
	return allocatable
}

func createTaints(uids []string) map[string]intelcrd.TaintedGpu {
	taints := make(map[string]intelcrd.TaintedGpu, len(uids))
	reasons := make(map[string]bool)
	reasons["taintReason"] = true
	for _, uid := range uids {
		taints[uid] = intelcrd.TaintedGpu{Reasons: reasons}
	}
	return taints
}

func mergeMaps(map1, map2 map[string]gpuv1alpha2.AllocatableGpu) map[string]gpuv1alpha2.AllocatableGpu {
	result := map[string]gpuv1alpha2.AllocatableGpu{}
	// shallow (top level) copy/override.
	maps.Copy(result, map1)
	maps.Copy(result, map2)
	return result

}

func TestUnsuitableNodes(t *testing.T) {

	vfParentUID := "gpu-Z"

	availableGPUsWithOneUnpreparedVF := createAllocatable(createMapGpu([]keyValue{
		{"gpu-A", 100}, {"gpu-B", 100}, {"gpu-C", 100}, {"gpu-D", 100},
	}, 1000, 1))

	taintedGPUs := createTaints([]string{"gpu-A", "gpu-C"})

	availableGPUsWithTwoPreparedVFs := createAllocatable(createMapGpu([]keyValue{
		{vfParentUID, 100},
	}, 1000, 2))

	allAvailableGPUsWithVFs := mergeMaps(availableGPUsWithOneUnpreparedVF, availableGPUsWithTwoPreparedVFs)

	availableVFs := createAllocatable(createMapVF(vfParentUID, []keyValue{
		{"gpu-E", 100}, {"gpu-F", 100}}))

	availableNonVFCapableGPUs := createAllocatable(createMapGpu([]keyValue{
		{"gpu-X", 100}, {"gpu-Y", 100},
	}, 1000, 0))

	allAvailableGPUs := mergeMaps(allAvailableGPUsWithVFs, availableNonVFCapableGPUs)

	// neither GPUs with prepared VFs, nor tainted GPUs can be allocated
	allocatableGpuCount := len(availableGPUsWithOneUnpreparedVF) + len(availableNonVFCapableGPUs) - len(taintedGPUs)

	allocatablePreparedVFCount := len(availableVFs)

	// only GPUs in "availableGPUsWithOneUnpreparedVF" are unprepared, only those GPUs are tainted,
	// and because they have just one VF each, len of that & "taintedGPUs" can be used as-is
	allocatableUnpreparedVFCount := len(availableGPUsWithOneUnpreparedVF) - len(taintedGPUs)
	allocatableNonVFCapableGpuCount := len(availableNonVFCapableGPUs)

	allocatableVFCount := allocatablePreparedVFCount + allocatableUnpreparedVFCount
	allocatableAnyCount := allocatablePreparedVFCount + allocatableUnpreparedVFCount + allocatableNonVFCapableGpuCount

	availableAll := mergeMaps(allAvailableGPUs, availableVFs)

	// claim that fits to all available fake GPUs
	gpuClaim := &intelcrd.GpuClaimParametersSpec{
		Type:       intelcrd.GpuDeviceType,
		Millicores: 100,
		Memory:     10,
		Count:      1, // 0 -> get zero GPUs due to "Not enough resources to serve GPU request"
	}

	vfClaim := &intelcrd.GpuClaimParametersSpec{
		Type:   intelcrd.VfDeviceType,
		Memory: 10,
		Count:  1,
	}

	anyClaim := &intelcrd.GpuClaimParametersSpec{
		Type:   intelcrd.AnyDeviceType,
		Memory: 10,
		Count:  1,
	}

	defaultClass := &intelcrd.GpuClassParametersSpec{
		DeviceSelector: []gpuv1alpha2.DeviceSelector{},
		Monitor:        false,
		Shared:         false, // not checked in UnsuitableNodes call-chain
	}

	monitorClass := &intelcrd.GpuClassParametersSpec{
		DeviceSelector: []gpuv1alpha2.DeviceSelector{},
		Monitor:        true,
		Shared:         true,
	}

	type testCase struct {
		testName                string
		gasNs                   string
		gasStatus               string
		potentialNodes          []string
		expectedUnsuitableNodes []string
		class                   *intelcrd.GpuClassParametersSpec
		claim                   *intelcrd.GpuClaimParametersSpec
		claimCount              int
		expectedGpuCount        int
	}

	testCases := []testCase{
		{
			testName:                "all OK (GPU claim) with all available devices",
			gasNs:                   testNameSpace,
			gasStatus:               intelcrd.GpuAllocationStateStatusReady,
			potentialNodes:          []string{fakeNodeName},
			expectedUnsuitableNodes: nil,
			class:                   defaultClass,
			claim:                   gpuClaim,
			claimCount:              allocatableGpuCount,
			expectedGpuCount:        allocatableGpuCount,
		},
		{
			testName:                "all OK (VF claim) with all available devices",
			gasNs:                   testNameSpace,
			gasStatus:               intelcrd.GpuAllocationStateStatusReady,
			potentialNodes:          []string{fakeNodeName},
			expectedUnsuitableNodes: nil,
			class:                   defaultClass,
			claim:                   vfClaim,
			claimCount:              allocatableVFCount,
			expectedGpuCount:        allocatableVFCount,
		},
		{
			testName:                "all OK (Any claim) with all available devices",
			gasNs:                   testNameSpace,
			gasStatus:               intelcrd.GpuAllocationStateStatusReady,
			potentialNodes:          []string{fakeNodeName},
			expectedUnsuitableNodes: nil,
			class:                   defaultClass,
			claim:                   anyClaim,
			claimCount:              allocatableAnyCount,
			expectedGpuCount:        allocatableAnyCount,
		},
		{
			testName:                "requesting more than is untainted (GPU claim)",
			gasNs:                   testNameSpace,
			gasStatus:               intelcrd.GpuAllocationStateStatusReady,
			potentialNodes:          []string{fakeNodeName},
			expectedUnsuitableNodes: []string{fakeNodeName},
			class:                   defaultClass,
			claim:                   gpuClaim,
			claimCount:              allocatableGpuCount + 1,
			expectedGpuCount:        0,
		},
		{
			testName:                "requesting more than is untainted (VF claim)",
			gasNs:                   testNameSpace,
			gasStatus:               intelcrd.GpuAllocationStateStatusReady,
			potentialNodes:          []string{fakeNodeName},
			expectedUnsuitableNodes: []string{fakeNodeName},
			class:                   defaultClass,
			claim:                   vfClaim,
			claimCount:              allocatableVFCount + 1,
			expectedGpuCount:        0,
		},
		{
			testName:                "requesting more than is untainted (Any claim)",
			gasNs:                   testNameSpace,
			gasStatus:               intelcrd.GpuAllocationStateStatusReady,
			potentialNodes:          []string{fakeNodeName},
			expectedUnsuitableNodes: []string{fakeNodeName},
			class:                   defaultClass,
			claim:                   anyClaim,
			claimCount:              allocatableAnyCount + 1,
			expectedGpuCount:        0,
		},
		{
			testName:                "wrong namespace",
			gasNs:                   "wrong " + testNameSpace,
			gasStatus:               intelcrd.GpuAllocationStateStatusReady,
			potentialNodes:          []string{fakeNodeName},
			expectedUnsuitableNodes: []string{fakeNodeName},
			class:                   defaultClass,
			claim:                   gpuClaim,
			claimCount:              1,
			expectedGpuCount:        0,
		},
		{
			testName:                "correct namespace, wrong status",
			gasNs:                   testNameSpace,
			gasStatus:               intelcrd.GpuAllocationStateStatusNotReady,
			potentialNodes:          []string{fakeNodeName},
			expectedUnsuitableNodes: []string{fakeNodeName},
			class:                   defaultClass,
			claim:                   gpuClaim,
			claimCount:              1,
			expectedGpuCount:        0,
		},
		{
			// controller skips allocation checks for monitors, so claim count
			// does not matter and no GPUs are assigned (that's done by kubelet),
			// monitor will get any requested GPU node...
			testName:                "monitor, for all GPUs (including tainted)",
			gasNs:                   testNameSpace,
			gasStatus:               intelcrd.GpuAllocationStateStatusReady,
			potentialNodes:          []string{fakeNodeName},
			expectedUnsuitableNodes: nil,
			class:                   monitorClass,
			claim:                   gpuClaim,
			claimCount:              len(allAvailableGPUs) * 2,
			expectedGpuCount:        0,
		},
		{
			// ...as long as namespace and kubelet plugin GAS status are fine
			testName:                "monitor, wrong namespace",
			gasNs:                   "wrong " + testNameSpace,
			gasStatus:               intelcrd.GpuAllocationStateStatusReady,
			potentialNodes:          []string{fakeNodeName},
			expectedUnsuitableNodes: []string{fakeNodeName},
			class:                   monitorClass,
			claim:                   gpuClaim,
			claimCount:              1,
			expectedGpuCount:        0,
		},
		{
			testName:                "monitor, wrong status",
			gasNs:                   testNameSpace,
			gasStatus:               intelcrd.GpuAllocationStateStatusNotReady,
			potentialNodes:          []string{fakeNodeName},
			expectedUnsuitableNodes: []string{fakeNodeName},
			class:                   monitorClass,
			claim:                   gpuClaim,
			claimCount:              1,
			expectedGpuCount:        0,
		},
	}

	for _, testCase := range testCases {
		fakeGas := &gpuv1alpha2.GpuAllocationState{
			TypeMeta:   metav1.TypeMeta{},
			ObjectMeta: metav1.ObjectMeta{Namespace: testCase.gasNs, Name: fakeNodeName},
			Status:     testCase.gasStatus,
		}
		fakeGas.Spec.AllocatableDevices = availableAll
		fakeGas.Spec.TaintedDevices = taintedGPUs

		fakeDRAClient := gpucsfake.NewSimpleClientset(fakeGas)
		driver := createFakeDriver(t, kubefake.NewSimpleClientset(), fakeDRAClient)

		allcas := []*controller.ClaimAllocation{}
		for i := 0; i < testCase.claimCount; i++ {
			ca := createFakeClaimAllocation(testCase.class, testCase.claim)
			ca.Claim.UID = types.UID(fmt.Sprintf("claim-%d", i))
			allcas = append(allcas, &ca)
		}

		t.Log(testCase.testName)

		fakePod := v1.Pod{}
		ctx := context.TODO()
		err := driver.UnsuitableNodes(ctx, &fakePod, allcas, testCase.potentialNodes)

		if err != nil {
			t.Errorf("ERROR, from UnsuitableNodes(): %v", err)
		}

		resultUnsuitable := allcas[0].UnsuitableNodes
		if !reflect.DeepEqual(resultUnsuitable, testCase.expectedUnsuitableNodes) {
			t.Errorf("ERROR, UnsuitableNodes=%#v, expected=%#v", resultUnsuitable, testCase.expectedUnsuitableNodes)
		}

		klog.V(5).Infof("fakeGas.Spec: %+v", fakeGas.Spec)

		gpuCount := 0
		for _, nodes := range driver.PendingClaimRequests.requests {
			for _, claim := range nodes {
				gpuCount += len(claim.Gpus)
			}
		}

		if gpuCount != testCase.expectedGpuCount {
			klog.V(5).Infof("driver.PendingClaimRequests: %+v", driver.PendingClaimRequests)
			t.Errorf("ERROR, GPU count=%d, expected=%d", gpuCount, testCase.expectedGpuCount)
		}
	}
}

func TestMain(m *testing.M) {
	// To be able to see and set driver logging level, e.g:
	// go test -v -fastfail github.com/intel/intel-resource-drivers-for-kubernetes/cmd/controller -args -v=5
	klog.InitFlags(nil)
	os.Exit(m.Run())
}
