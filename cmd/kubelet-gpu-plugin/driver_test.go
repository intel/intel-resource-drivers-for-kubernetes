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
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/fsnotify/fsnotify"
	gpucsfake "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gpu/clientset/versioned/fake"
	gpuv1alpha2 "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gpu/v1alpha2"
	intelcrd "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gpu/v1alpha2/api"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/kubelet/pkg/apis/dra/v1alpha3"
)

func TestFakeSysfs(t *testing.T) {
	fakeSysfsRoot := "/tmp/fakesysfs2"

	fakeSysFsContents(
		t,
		fakeSysfsRoot,
		DevicesInfo{
			"0000:00:02.0-0x56c0": {model: "0x56c0", memoryMiB: 8192, deviceType: "gpu", cardidx: 0, renderdidx: 128, uid: "0000:00:02.0-0x56c0", maxvfs: 16},
		},
	)

	if err := os.RemoveAll(fakeSysfsRoot); err != nil {
		t.Errorf("could not cleanup fake sysfs %v", fakeSysfsRoot)
	}
}

func getFakeDriver(sysfsRoot string, gasPreparedClaims map[string]gpuv1alpha2.PreparedClaim) (*driver, error) {

	fakeGas := &gpuv1alpha2.GpuAllocationState{
		TypeMeta:   metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{Namespace: "namespace1", Name: "node1"},
		Status:     "Ready",
		Spec: gpuv1alpha2.GpuAllocationStateSpec{
			PreparedClaims: gasPreparedClaims,
		},
	}
	fakeDRAClient := gpucsfake.NewSimpleClientset(fakeGas)

	config := &configType{
		crdconfig: &intelcrd.GpuAllocationStateConfig{
			Name:      "node1",
			Namespace: "namespace1",
		},
		clientset: &clientsetType{
			kubefake.NewSimpleClientset(),
			fakeDRAClient,
		},
		cdiRoot: "/tmp/fakecdiroot",
	}

	os.Setenv("SYSFS_ROOT", sysfsRoot)

	return newDriver(context.TODO(), config)
}

func TestNodePrepareResources(t *testing.T) {
	type testCase struct {
		name               string
		request            *v1alpha3.NodePrepareResourcesRequest
		expectedResponse   *v1alpha3.NodePrepareResourcesResponse
		gasSpecAllocations map[string]gpuv1alpha2.AllocatedClaim
	}

	testcases := []testCase{
		{
			name: "blank request",
			request: &v1alpha3.NodePrepareResourcesRequest{
				Claims: []*v1alpha3.Claim{},
			},
			expectedResponse: &v1alpha3.NodePrepareResourcesResponse{
				Claims: map[string]*v1alpha3.NodePrepareResourceResponse{},
			},
		},
		{
			name: "single GPU",
			request: &v1alpha3.NodePrepareResourcesRequest{
				Claims: []*v1alpha3.Claim{
					{Name: "claim1", Namespace: "namespace1", Uid: "uid1"},
				},
			},
			expectedResponse: &v1alpha3.NodePrepareResourcesResponse{
				Claims: map[string]*v1alpha3.NodePrepareResourceResponse{
					"uid1": {CDIDevices: []string{"intel.com/gpu=0000:00:02.0-0x56c0"}},
				},
			},
			gasSpecAllocations: map[string]gpuv1alpha2.AllocatedClaim{
				"uid1": {Gpus: []gpuv1alpha2.AllocatedGpu{{UID: "0000:00:02.0-0x56c0", Type: "gpu", Memory: 4096}}},
			},
		},
		{
			name: "single existing VF",
			request: &v1alpha3.NodePrepareResourcesRequest{
				Claims: []*v1alpha3.Claim{
					{Name: "claim2", Namespace: "namespace2", Uid: "uid2"},
				},
			},
			expectedResponse: &v1alpha3.NodePrepareResourcesResponse{
				Claims: map[string]*v1alpha3.NodePrepareResourceResponse{
					"uid2": {CDIDevices: []string{"intel.com/gpu=0000:00:03.1-0x56c0"}},
				},
			},
			gasSpecAllocations: map[string]gpuv1alpha2.AllocatedClaim{
				"uid2": {Gpus: []gpuv1alpha2.AllocatedGpu{{UID: "0000:00:03.1-0x56c0", Type: "vf", Memory: 8064, VFIndex: 0, ParentUID: "0000:00:03.0-0x56c0"}}},
			},
		},
		// this is a slow test case - validation of created VF is timing out as expected
		{
			name: "single new VF failed post-creation validation",
			request: &v1alpha3.NodePrepareResourcesRequest{
				Claims: []*v1alpha3.Claim{
					{Name: "claim3", Namespace: "namespace3", Uid: "uid3"},
				},
			},
			expectedResponse: &v1alpha3.NodePrepareResourcesResponse{
				Claims: map[string]*v1alpha3.NodePrepareResourceResponse{
					"uid3": {Error: "error preparing resource: failed to validate provisioned VFs: vf 0 of GPU 0000:00:02.0 is NOT OK, did not check the rest of new VFs, cleaned up successfully"},
				},
			},
			gasSpecAllocations: map[string]gpuv1alpha2.AllocatedClaim{
				"uid3": {Gpus: []gpuv1alpha2.AllocatedGpu{{UID: "", Type: "vf", Memory: 4096, VFIndex: 0, ParentUID: "0000:00:02.0-0x56c0"}}},
				"uid4": {Gpus: []gpuv1alpha2.AllocatedGpu{{UID: "", Type: "vf", Memory: 4096, VFIndex: 1, ParentUID: "0000:00:02.0-0x56c0"}}},
			},
		},
	}

	fakeSysfsRoot := "/tmp/fakesysfs"
	fakeSysFsContents(
		t,
		fakeSysfsRoot,
		DevicesInfo{
			"0000:00:02.0-0x56c0": {model: "0x56c0", memoryMiB: 16256, deviceType: "gpu", cardidx: 0, renderdidx: 128, uid: "0000:00:02.0-0x56c0", maxvfs: 16},
			"0000:00:03.0-0x56c0": {model: "0x56c0", memoryMiB: 16256, deviceType: "gpu", cardidx: 1, renderdidx: 129, uid: "0000:00:03.0-0x56c0", maxvfs: 16},
			"0000:00:03.1-0x56c0": {model: "0x56c0", memoryMiB: 8064, deviceType: "vf", cardidx: 2, renderdidx: 130, uid: "0000:00:03.1-0x56c0", vfindex: 0, vfprofile: "flex170_m2", parentuid: "0000:00:03.0-0x56c0"},
		},
	)

	driver, driverErr := getFakeDriver(fakeSysfsRoot, nil)
	if driverErr != nil {
		t.Errorf("could not create kubelet-plugin: %v\n", driverErr)
	}

	for _, testcase := range testcases {
		t.Log(testcase.name)

		// cleanup and setup GAS
		gasspec := driver.gas.Spec.DeepCopy()
		gasspec.AllocatedClaims = testcase.gasSpecAllocations
		gasspec.PreparedClaims = nil

		if err := driver.gas.Update(context.TODO(), gasspec); err != nil {
			t.Error("setup error: could not prepare GAS")
		}

		response, err := driver.NodePrepareResources(context.TODO(), testcase.request)

		if err != nil {
			t.Errorf("%v: error %v, expected no error", testcase.name, err)
		}

		if !reflect.DeepEqual(response, testcase.expectedResponse) {
			t.Errorf("%v: unexpected response: %+v, expected response: %v", testcase.name, response, testcase.expectedResponse)
		}
	}
	if err := os.RemoveAll(fakeSysfsRoot); err != nil {
		t.Errorf("could not cleanup fake sysfs %v", fakeSysfsRoot)
	}
}

func TestReuseLeftoverSRIOVResources(t *testing.T) {
	fakeSysfsRoot := "/tmp/fakesysfs2"
	fakeSysFsContents(
		t,
		fakeSysfsRoot,
		DevicesInfo{
			"0000:00:02.0-0x56c0": {model: "0x56c0", memoryMiB: 14248, deviceType: "gpu", cardidx: 0, renderdidx: 128, uid: "0000:00:02.0-0x56c0", maxvfs: 16},
			"0000:00:03.0-0x56c0": {model: "0x56c0", memoryMiB: 14248, deviceType: "gpu", cardidx: 1, renderdidx: 129, uid: "0000:00:03.0-0x56c0", maxvfs: 16},
		},
	)

	driver, driverErr := getFakeDriver(fakeSysfsRoot, nil)
	if driverErr != nil {
		t.Errorf("could not create kubelet-plugin: %v\n", driverErr)
	}

	expectedToProvision := map[string][]*DeviceInfo{
		"0000:00:03.0-0x56c0": {
			{
				uid:        "",
				memoryMiB:  0,
				model:      "",
				deviceType: "vf",
				vfindex:    0,
				vfprofile:  "flex170_m2",
				parentuid:  "0000:00:03.0-0x56c0",
			},
			{
				uid:        "", // uid is populated after provisioning
				memoryMiB:  0,  // memory is not populated until VF is provisioned. Because not needed.
				model:      "",
				deviceType: "vf",
				vfindex:    1,
				vfprofile:  "flex170_m2",
				parentuid:  "0000:00:03.0-0x56c0",
			},
		},
	}

	toProvision := map[string][]*DeviceInfo{
		"0000:00:03.0-0x56c0": {
			{
				deviceType: "vf",
				vfindex:    0,
				vfprofile:  "flex170_m2",
				parentuid:  "0000:00:03.0-0x56c0",
			},
		},
	}
	driver.reuseLeftoverSRIOVResources(toProvision)

	if !reflect.DeepEqual(toProvision, expectedToProvision) {
		for _, vf := range toProvision["0000:00:03.0-0x56c0"] {
			fmt.Printf("toProvision VF: %+v\n", vf)
		}
		for _, vf := range expectedToProvision["0000:00:03.0-0x56c0"] {
			fmt.Printf("toProvision VF: %+v\n", vf)
		}
		t.Errorf("unexpected result after reusing leftovers: %+v; expected: %+v", toProvision, expectedToProvision)
	}
	if err := os.RemoveAll(fakeSysfsRoot); err != nil {
		t.Errorf("could not cleanup fake sysfs %v", fakeSysfsRoot)
	}
}

func TestNodeUnprepareResources(t *testing.T) {
	type testCase struct {
		name                        string
		request                     *v1alpha3.NodeUnprepareResourcesRequest
		expectedResponse            *v1alpha3.NodeUnprepareResourcesResponse
		gasSpecPreparations         map[string]gpuv1alpha2.PreparedClaim
		expectedGasSpecPreparations map[string]gpuv1alpha2.PreparedClaim
		updateFakeSysfs             bool
	}

	testcases := []testCase{
		{
			name: "blank request",
			request: &v1alpha3.NodeUnprepareResourcesRequest{
				Claims: []*v1alpha3.Claim{},
			},
			expectedResponse: &v1alpha3.NodeUnprepareResourcesResponse{
				Claims: map[string]*v1alpha3.NodeUnprepareResourceResponse{},
			},
			gasSpecPreparations:         map[string]gpuv1alpha2.PreparedClaim{},
			expectedGasSpecPreparations: map[string]gpuv1alpha2.PreparedClaim{},
		},
		{
			name: "single GPU",
			request: &v1alpha3.NodeUnprepareResourcesRequest{
				Claims: []*v1alpha3.Claim{
					{Name: "claim1", Namespace: "namespace1", Uid: "uid1"},
				},
			},
			expectedResponse: &v1alpha3.NodeUnprepareResourcesResponse{
				Claims: map[string]*v1alpha3.NodeUnprepareResourceResponse{"uid1": {}},
			},
			gasSpecPreparations: map[string]gpuv1alpha2.PreparedClaim{
				"uid1": []gpuv1alpha2.AllocatedGpu{{UID: "0000:b3:00.0-0x0bda", Type: "gpu", Memory: 4096}},
			},
			expectedGasSpecPreparations: map[string]gpuv1alpha2.PreparedClaim{},
		},
		{
			name: "single VF without cleanup",
			request: &v1alpha3.NodeUnprepareResourcesRequest{
				Claims: []*v1alpha3.Claim{
					{Name: "claim2", Namespace: "namespace2", Uid: "uid2"},
				},
			},
			expectedResponse: &v1alpha3.NodeUnprepareResourcesResponse{
				Claims: map[string]*v1alpha3.NodeUnprepareResourceResponse{"uid2": {}},
			},
			gasSpecPreparations: map[string]gpuv1alpha2.PreparedClaim{
				"uid2": []gpuv1alpha2.AllocatedGpu{{UID: "0000:af:00.1-0x0bda", Type: "vf", Memory: 22528, Millicores: 500, VFIndex: 0, ParentUID: "0000:af:00.0-0x0bda"}},
				"uid3": []gpuv1alpha2.AllocatedGpu{{UID: "0000:af:00.2-0x0bda", Type: "vf", Memory: 22528, Millicores: 500, VFIndex: 1, ParentUID: "0000:af:00.0-0x0bda"}},
			},
			expectedGasSpecPreparations: map[string]gpuv1alpha2.PreparedClaim{
				"uid3": []gpuv1alpha2.AllocatedGpu{{UID: "0000:af:00.2-0x0bda", Type: "vf", Memory: 22528, Millicores: 500, VFIndex: 1, ParentUID: "0000:af:00.0-0x0bda"}},
			},
		},
		// This test is a bit slow because kubelet-plugin waits for VFs to go away, and they never do.
		{
			name: "single VF failed cleanup",
			request: &v1alpha3.NodeUnprepareResourcesRequest{
				Claims: []*v1alpha3.Claim{
					{Name: "claim3", Namespace: "namespace3", Uid: "uid3"},
				},
			},
			expectedResponse: &v1alpha3.NodeUnprepareResourcesResponse{
				Claims: map[string]*v1alpha3.NodeUnprepareResourceResponse{
					"uid3": {Error: "error unpreparing resource: failed to remove VFs: ; failed removing VFs"},
				},
			},
			gasSpecPreparations: map[string]gpuv1alpha2.PreparedClaim{
				"uid3": []gpuv1alpha2.AllocatedGpu{{UID: "0000:af:00.2-0x0bda", Type: "vf", Memory: 22528, Millicores: 500, VFIndex: 1, ParentUID: "0000:af:00.0-0x0bda"}},
			},
			expectedGasSpecPreparations: map[string]gpuv1alpha2.PreparedClaim{
				"uid3": []gpuv1alpha2.AllocatedGpu{{UID: "0000:af:00.2-0x0bda", Type: "vf", Memory: 22528, Millicores: 500, VFIndex: 1, ParentUID: "0000:af:00.0-0x0bda"}},
			},
		},
		{
			name: "single VF successful cleanup",
			request: &v1alpha3.NodeUnprepareResourcesRequest{
				Claims: []*v1alpha3.Claim{
					{Name: "claim3", Namespace: "namespace3", Uid: "uid3"},
				},
			},
			expectedResponse: &v1alpha3.NodeUnprepareResourcesResponse{
				Claims: map[string]*v1alpha3.NodeUnprepareResourceResponse{"uid3": {}},
			},
			gasSpecPreparations: map[string]gpuv1alpha2.PreparedClaim{
				"uid3": []gpuv1alpha2.AllocatedGpu{{UID: "0000:af:00.2-0x0bda", Type: "vf", Memory: 22528, Millicores: 500, VFIndex: 1, ParentUID: "0000:af:00.0-0x0bda"}},
			},
			expectedGasSpecPreparations: map[string]gpuv1alpha2.PreparedClaim{},
			updateFakeSysfs:             true,
		},
	}

	fakeSysfsRoot := "/tmp/fakesysfs"
	fakeSysFsContents(
		t,
		fakeSysfsRoot,
		DevicesInfo{
			"0000:b3:00.0-0x0bda": {model: "0x0bda", memoryMiB: 49136, deviceType: "gpu", cardidx: 0, uid: "0000:b3:00.0-0x0bda", maxvfs: 63},
			"0000:af:00.0-0x0bda": {model: "0x0bda", memoryMiB: 49136, deviceType: "gpu", cardidx: 1, uid: "0000:af:00.0-0x0bda", maxvfs: 63},
			"0000:af:00.1-0x0bda": {model: "0x0bda", memoryMiB: 22528, millicores: 500, deviceType: "vf", cardidx: 2, uid: "0000:af:00.1-0x0bda", vfindex: 0, vfprofile: "max_47g_c2", parentuid: "0000:af:00.0-0x0bda"},
			"0000:af:00.2-0x0bda": {model: "0x0bda", memoryMiB: 22528, millicores: 500, deviceType: "vf", cardidx: 3, uid: "0000:af:00.2-0x0bda", vfindex: 1, vfprofile: "max_47g_c2", parentuid: "0000:af:00.0-0x0bda"},
		},
	)

	var watcher *fsnotify.Watcher
	for _, testcase := range testcases {
		t.Log(testcase.name)

		driver, driverErr := getFakeDriver(fakeSysfsRoot, testcase.gasSpecPreparations)
		if driverErr != nil {
			t.Errorf("could not create kubelet-plugin: %v\n", driverErr)
			continue
		}

		// dynamically add and remove fske sysfs SR-IOV VFs
		if testcase.updateFakeSysfs {
			watcher = watchNumvfs(t, fakeSysfsRoot)
		}

		response, err := driver.NodeUnprepareResources(context.TODO(), testcase.request)

		if err != nil {
			t.Errorf("%v: error %v, expected no error", testcase.name, err)
		}

		if !reflect.DeepEqual(response, testcase.expectedResponse) {
			t.Errorf("%v: unexpected response: %+v, expected response: %v", testcase.name, response, testcase.expectedResponse)
		}

		if !reflect.DeepEqual(testcase.expectedGasSpecPreparations, driver.gas.Spec.PreparedClaims) {
			t.Errorf(
				"unexpected GAS.Spec.PreparedClaims:\n%+v\nexpected GAS.SpecPreparedClaims:\n%+v",
				driver.gas.Spec.PreparedClaims,
				testcase.expectedGasSpecPreparations,
			)
		}

		// dynamically add and remove fske sysfs SR-IOV VFs
		if testcase.updateFakeSysfs && watcher != nil {
			watcher.Close()
		}

	}
	if err := os.RemoveAll(fakeSysfsRoot); err != nil {
		t.Errorf("could not cleanup fake sysfs %v", fakeSysfsRoot)
	}
}
