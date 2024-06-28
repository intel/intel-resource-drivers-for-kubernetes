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
	"encoding/json"
	"fmt"
	"os"
	"path"
	"reflect"
	"testing"

	"github.com/fsnotify/fsnotify"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/kubelet/pkg/apis/dra/v1alpha3"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/fakesysfs"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
	gpucsfake "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gpu/clientset/versioned/fake"
	gpuv1alpha2 "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gpu/v1alpha2"
	intelcrd "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gpu/v1alpha2/api"
	helpers "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/plugintesthelpers"
)

func TestFakeSysfs(t *testing.T) {
	fakeSysfsRoot := "/tmp/fakegpusysfs"

	if err := fakesysfs.FakeSysFsGpuContents(
		t,
		fakeSysfsRoot,
		device.DevicesInfo{
			"0000-00-02-0-0x56c0": {Model: "0x56c0", MemoryMiB: 8192, DeviceType: "gpu", CardIdx: 0, RenderdIdx: 128, UID: "0000-00-02-0-0x56c0", MaxVFs: 16},
		},
	); err != nil {
		t.Errorf("setup error: could not create fake sysfs: %v", err)
		return
	}

	if err := os.RemoveAll(fakeSysfsRoot); err != nil {
		t.Errorf("could not cleanup fake sysfs %v", fakeSysfsRoot)
	}
}

func getFakeDriver(testDirs helpers.TestDirsType) (*driver, error) {

	fakeGas := &gpuv1alpha2.GpuAllocationState{
		TypeMeta:   metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{Namespace: "namespace1", Name: "node1"},
		Status:     "Ready",
		Spec:       gpuv1alpha2.GpuAllocationStateSpec{},
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
		cdiRoot:          testDirs.CdiRoot,
		driverPluginPath: testDirs.DriverPluginRoot,
	}

	os.Setenv("SYSFS_ROOT", testDirs.SysfsRoot)

	return newDriver(context.TODO(), config)
}

func TestNodePrepareResources(t *testing.T) {
	type testCase struct {
		name               string
		request            *v1alpha3.NodePrepareResourcesRequest
		expectedResponse   *v1alpha3.NodePrepareResourcesResponse
		gasSpecAllocations map[string]gpuv1alpha2.AllocatedClaim
		preparedClaims     ClaimPreparations
		updateFakeSysfs    bool
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
			preparedClaims:  ClaimPreparations{},
			updateFakeSysfs: false,
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
					"uid1": {CDIDevices: []string{"intel.com/gpu=0000-00-02-0-0x56c0"}},
				},
			},
			gasSpecAllocations: map[string]gpuv1alpha2.AllocatedClaim{
				"uid1": {Gpus: []gpuv1alpha2.AllocatedGpu{{UID: "0000-00-02-0-0x56c0", Type: "gpu", Memory: 4096}}},
			},
			preparedClaims:  ClaimPreparations{},
			updateFakeSysfs: false,
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
					"uid2": {CDIDevices: []string{"intel.com/gpu=0000-00-03-1-0x56c0"}},
				},
			},
			gasSpecAllocations: map[string]gpuv1alpha2.AllocatedClaim{
				"uid2": {Gpus: []gpuv1alpha2.AllocatedGpu{{UID: "0000-00-03-1-0x56c0", Type: "vf", Memory: 8064, VFIndex: 0, ParentUID: "0000-00-03-0-0x56c0"}}},
			},
			preparedClaims:  ClaimPreparations{},
			updateFakeSysfs: false,
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
				"uid3": {Gpus: []gpuv1alpha2.AllocatedGpu{{UID: "", Type: "vf", Memory: 4096, VFIndex: 0, ParentUID: "0000-00-02-0-0x56c0"}}},
				"uid4": {Gpus: []gpuv1alpha2.AllocatedGpu{{UID: "", Type: "vf", Memory: 4096, VFIndex: 1, ParentUID: "0000-00-02-0-0x56c0"}}},
			},
			preparedClaims:  ClaimPreparations{},
			updateFakeSysfs: false,
		},
		{
			name: "single new VF failed creation, no tiles",
			request: &v1alpha3.NodePrepareResourcesRequest{
				Claims: []*v1alpha3.Claim{
					{Name: "claim5", Namespace: "namespace5", Uid: "uid5"},
				},
			},
			expectedResponse: &v1alpha3.NodePrepareResourcesResponse{
				Claims: map[string]*v1alpha3.NodePrepareResourceResponse{
					"uid5": {Error: "error preparing resource: failed to validate provisioned VFs: vf 0 of GPU 0000:00:04.0 is NOT OK, did not check the rest of new VFs, cleaned up successfully"},
				},
			},
			gasSpecAllocations: map[string]gpuv1alpha2.AllocatedClaim{
				"uid5": {Gpus: []gpuv1alpha2.AllocatedGpu{{UID: "", Type: "vf", Memory: 4096, VFIndex: 0, ParentUID: "0000-00-04-0-0x0000"}}},
			},
			preparedClaims:  ClaimPreparations{},
			updateFakeSysfs: false,
		},
		{
			name: "monitoring claim",
			request: &v1alpha3.NodePrepareResourcesRequest{
				Claims: []*v1alpha3.Claim{
					{Name: "monitor", Namespace: "namespace1", Uid: "uid1", ResourceHandle: "monitor"},
				},
			},
			expectedResponse: &v1alpha3.NodePrepareResourcesResponse{
				Claims: map[string]*v1alpha3.NodePrepareResourceResponse{
					"uid1": {
						CDIDevices: []string{
							"intel.com/gpu=0000-00-02-0-0x56c0",
							"intel.com/gpu=0000-00-03-0-0x56c0",
							"intel.com/gpu=0000-00-03-1-0x56c0",
							"intel.com/gpu=0000-00-04-0-0x0000",
						},
					},
				},
			},
			gasSpecAllocations: map[string]gpuv1alpha2.AllocatedClaim{},
			preparedClaims:     ClaimPreparations{},
			updateFakeSysfs:    false,
		},
		{
			name: "single GPU, already prepared claim",
			request: &v1alpha3.NodePrepareResourcesRequest{
				Claims: []*v1alpha3.Claim{
					{Name: "claim1", Namespace: "namespace1", Uid: "uid1"},
				},
			},
			expectedResponse: &v1alpha3.NodePrepareResourcesResponse{
				Claims: map[string]*v1alpha3.NodePrepareResourceResponse{
					"uid1": {CDIDevices: []string{"intel.com/gpu=0000-00-02-0-0x56c0"}},
				},
			},
			gasSpecAllocations: map[string]gpuv1alpha2.AllocatedClaim{
				"uid1": {Gpus: []gpuv1alpha2.AllocatedGpu{{UID: "0000-00-02-0-0x56c0", Type: "gpu", Memory: 4096}}},
			},
			preparedClaims: ClaimPreparations{
				"uid1": {{UID: "0000-00-02-0-0x56c0", DeviceType: "gpu", MemoryMiB: 4096, Millicores: 1}},
			},
			updateFakeSysfs: false,
		},
		{
			name: "single new VF success",
			request: &v1alpha3.NodePrepareResourcesRequest{
				Claims: []*v1alpha3.Claim{
					{Name: "claim3", Namespace: "namespace3", Uid: "uid3"},
				},
			},
			expectedResponse: &v1alpha3.NodePrepareResourcesResponse{
				Claims: map[string]*v1alpha3.NodePrepareResourceResponse{
					"uid3": {CDIDevices: []string{"intel.com/gpu=0000-00-02-1-0x56c0"}},
				},
			},
			gasSpecAllocations: map[string]gpuv1alpha2.AllocatedClaim{
				"uid3": {Gpus: []gpuv1alpha2.AllocatedGpu{{UID: "", Type: "vf", Memory: 4096, VFIndex: 0, ParentUID: "0000-00-02-0-0x56c0"}}},
			},
			preparedClaims:  ClaimPreparations{},
			updateFakeSysfs: true,
		},
	}

	var watcher *fsnotify.Watcher
	for _, testcase := range testcases {
		t.Log(testcase.name)

		testDirs, err := helpers.NewTestDirs()
		defer helpers.CleanupTest(t, testcase.name, testDirs.TestRoot)
		if err != nil {
			t.Errorf("%v: setup error: %v", testcase.name, err)
			return
		}

		if err := fakesysfs.FakeSysFsGpuContents(
			t,
			testDirs.SysfsRoot,
			device.DevicesInfo{
				"0000-00-02-0-0x56c0": {Model: "0x56c0", MemoryMiB: 16256, DeviceType: "gpu", CardIdx: 0, RenderdIdx: 128, UID: "0000-00-02-0-0x56c0", MaxVFs: 16},
				"0000-00-03-0-0x56c0": {Model: "0x56c0", MemoryMiB: 16256, DeviceType: "gpu", CardIdx: 1, RenderdIdx: 129, UID: "0000-00-03-0-0x56c0", MaxVFs: 16},
				"0000-00-03-1-0x56c0": {Model: "0x56c0", MemoryMiB: 8064, DeviceType: "vf", CardIdx: 2, RenderdIdx: 130, UID: "0000-00-03-1-0x56c0", VFIndex: 0, VFProfile: "flex170_m2", ParentUID: "0000-00-03-0-0x56c0"},
				// dummy, no SR-IOV tiles
				"0000-00-04-0-0x0000": {Model: "0x0000", MemoryMiB: 14248, DeviceType: "gpu", CardIdx: 3, RenderdIdx: 131, UID: "0000-00-04-0-0x0000", MaxVFs: 16},
			},
		); err != nil {
			t.Errorf("setup error: could not create fake sysfs: %v", err)
			return
		}

		preparedClaimFilePath := path.Join(testDirs.DriverPluginRoot, device.PreparedClaimsFileName)
		if err := writePreparedClaimsToFile(preparedClaimFilePath, testcase.preparedClaims); err != nil {
			t.Errorf("%v: error %v, writing prepared claims to file", testcase.name, err)
		}

		driver, driverErr := getFakeDriver(testDirs)
		if driverErr != nil {
			t.Errorf("could not create kubelet-plugin: %v\n", driverErr)
			continue
		}

		// dynamically add and remove fake sysfs SR-IOV VFs
		if testcase.updateFakeSysfs {
			watcher = fakesysfs.WatchNumvfs(t, testDirs.SysfsRoot)
			defer watcher.Close()
		}

		// cleanup and setup GAS
		gasspec := driver.gas.Spec.DeepCopy()
		gasspec.AllocatedClaims = testcase.gasSpecAllocations
		if err := driver.gas.Update(context.TODO(), gasspec); err != nil {
			t.Error("setup error: could not prepare GAS")
			continue
		}

		response, err := driver.NodePrepareResources(context.TODO(), testcase.request)
		if err != nil {
			t.Errorf("%v: error %v, expected no error", testcase.name, err)
		}

		if !compareNodePrepareResourcesResponses(testcase.expectedResponse, response) {
			t.Errorf("%v: unexpected response: %+v, expected response: %v", testcase.name, response, testcase.expectedResponse)
		}
	}
}

func TestReuseLeftoverSRIOVResources(t *testing.T) {
	testDirs, err := helpers.NewTestDirs()
	defer helpers.CleanupTest(t, "TestReuseLeftoverSRIOVResources", testDirs.TestRoot)
	if err != nil {
		t.Errorf("setup error: %v", err)
		return
	}
	if err := fakesysfs.FakeSysFsGpuContents(
		t,
		testDirs.SysfsRoot,
		device.DevicesInfo{
			"0000-00-02-0-0x56c0": {Model: "0x56c0", MemoryMiB: 14248, DeviceType: "gpu", CardIdx: 0, RenderdIdx: 128, UID: "0000-00-02-0-0x56c0", MaxVFs: 16},
			"0000-00-03-0-0x56c0": {Model: "0x56c0", MemoryMiB: 14248, DeviceType: "gpu", CardIdx: 1, RenderdIdx: 129, UID: "0000-00-03-0-0x56c0", MaxVFs: 16},
		},
	); err != nil {
		t.Errorf("setup error: could not create fake sysfs: %v", err)
		return
	}

	driver, driverErr := getFakeDriver(testDirs)
	if driverErr != nil {
		t.Errorf("could not create kubelet-plugin: %v\n", driverErr)
	}

	expectedToProvision := map[string][]*device.DeviceInfo{
		"0000-00-03-0-0x56c0": {
			{
				UID:        "",
				MemoryMiB:  0,
				Model:      "0x56c0",
				DeviceType: "vf",
				VFIndex:    0,
				VFProfile:  "flex170_m2",
				ParentUID:  "0000-00-03-0-0x56c0",
			},
			{
				UID:        "", // uid is populated after provisioning
				MemoryMiB:  0,  // memory is not populated until VF is provisioned. Because not needed.
				Model:      "0x56c0",
				DeviceType: "vf",
				VFIndex:    1,
				VFProfile:  "flex170_m2",
				ParentUID:  "0000-00-03-0-0x56c0",
			},
		},
	}

	toProvision := map[string][]*device.DeviceInfo{
		"0000-00-03-0-0x56c0": {
			{
				DeviceType: "vf",
				VFIndex:    0,
				Model:      "0x56c0",
				VFProfile:  "flex170_m2",
				ParentUID:  "0000-00-03-0-0x56c0",
			},
		},
	}
	driver.reuseLeftoverSRIOVResources(toProvision)

	if !reflect.DeepEqual(toProvision, expectedToProvision) {
		for _, vf := range toProvision["0000-00-03-0-0x56c0"] {
			fmt.Printf("toProvision VF: %+v\n", vf)
		}
		for _, vf := range expectedToProvision["0000-00-03-0-0x56c0"] {
			fmt.Printf("expectedtoProvision VF: %+v\n", vf)
		}
		t.Errorf("unexpected result after reusing leftovers: %+v; expected: %+v", toProvision, expectedToProvision)
	}
}

func TestNodeUnprepareResources(t *testing.T) {
	type testCase struct {
		name                   string
		request                *v1alpha3.NodeUnprepareResourcesRequest
		expectedResponse       *v1alpha3.NodeUnprepareResourcesResponse
		preparedClaims         ClaimPreparations
		expectedPreparedClaims ClaimPreparations
		updateFakeSysfs        bool
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
			preparedClaims:         ClaimPreparations{},
			expectedPreparedClaims: ClaimPreparations{},
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
			preparedClaims: ClaimPreparations{
				"uid1": {{UID: "0000-b3-00-0-0x0bda", DeviceType: "gpu", MemoryMiB: 4096}},
			},
			expectedPreparedClaims: ClaimPreparations{},
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
			preparedClaims: ClaimPreparations{
				"uid2": {{UID: "0000-af-00-1-0x0bda", PCIAddress: "0000:af:00.1", DeviceType: "vf", MemoryMiB: 22528, Millicores: 500, VFIndex: 0, ParentUID: "0000-af-00-0-0x0bda"}},
				"uid3": {{UID: "0000-af-00-2-0x0bda", PCIAddress: "0000:af:00.2", DeviceType: "vf", MemoryMiB: 22528, Millicores: 500, VFIndex: 1, ParentUID: "0000-af-00-0-0x0bda"}},
			},
			expectedPreparedClaims: ClaimPreparations{
				"uid3": {{UID: "0000-af-00-2-0x0bda", PCIAddress: "0000:af:00.2", Model: "0x0bda", CardIdx: 3, DeviceType: "vf", MemoryMiB: 22528, Millicores: 500, VFIndex: 1, ParentUID: "0000-af-00-0-0x0bda"}},
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
					"uid3": {Error: "error unpreparing resource: failed to remove VFs: 0000-af-00-0-0x0bda: failed removing VFs: timeout waiting for VFs to be disabled on device"},
				},
			},
			preparedClaims: ClaimPreparations{
				"uid3": {{UID: "0000-af-00-2-0x0bda", DeviceType: "vf", MemoryMiB: 22528, Millicores: 500, VFIndex: 1, ParentUID: "0000-af-00-0-0x0bda"}},
			},
			expectedPreparedClaims: ClaimPreparations{},
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
			preparedClaims: ClaimPreparations{
				"uid3": {{UID: "0000-af-00-2-0x0bda", DeviceType: "vf", MemoryMiB: 22528, Millicores: 500, VFIndex: 1, ParentUID: "0000-af-00-0-0x0bda"}},
			},
			expectedPreparedClaims: ClaimPreparations{},
			updateFakeSysfs:        true,
		},
	}

	var watcher *fsnotify.Watcher
	for _, testcase := range testcases {
		t.Log(testcase.name)

		testDirs, err := helpers.NewTestDirs()
		defer helpers.CleanupTest(t, "TestNodeUnprepareResources", testDirs.TestRoot)
		if err != nil {
			t.Errorf("setup error: %v", err)
			return
		}

		if err := fakesysfs.FakeSysFsGpuContents(
			t,
			testDirs.SysfsRoot,
			device.DevicesInfo{
				"0000-b3-00-0-0x0bda": {Model: "0x0bda", MemoryMiB: 49136, DeviceType: "gpu", CardIdx: 0, UID: "0000-b3-00-0-0x0bda", MaxVFs: 63},
				"0000-af-00-0-0x0bda": {Model: "0x0bda", MemoryMiB: 49136, DeviceType: "gpu", CardIdx: 1, UID: "0000-af-00-0-0x0bda", MaxVFs: 63},
				"0000-af-00-1-0x0bda": {Model: "0x0bda", MemoryMiB: 22528, Millicores: 500, DeviceType: "vf", CardIdx: 2, UID: "0000-af-00-1-0x0bda", VFIndex: 0, VFProfile: "max_47g_c2", ParentUID: "0000-af-00-0-0x0bda"},
				"0000-af-00-2-0x0bda": {Model: "0x0bda", MemoryMiB: 22528, Millicores: 500, DeviceType: "vf", CardIdx: 3, UID: "0000-af-00-2-0x0bda", VFIndex: 1, VFProfile: "max_47g_c2", ParentUID: "0000-af-00-0-0x0bda"},
			},
		); err != nil {
			t.Errorf("setup error: could not create fake sysfs: %v", err)
			return
		}

		preparedClaimsFilePath := path.Join(testDirs.DriverPluginRoot, device.PreparedClaimsFileName)
		if err := writePreparedClaimsToFile(preparedClaimsFilePath, testcase.preparedClaims); err != nil {
			t.Errorf("%v: error %v, writing prepared claims to file", testcase.name, err)
			continue
		}

		driver, driverErr := getFakeDriver(testDirs)
		if driverErr != nil {
			t.Errorf("could not create kubelet-plugin: %v\n", driverErr)
			continue
		}

		// dynamically add and remove fake sysfs SR-IOV VFs
		if testcase.updateFakeSysfs {
			watcher = fakesysfs.WatchNumvfs(t, testDirs.SysfsRoot)
			defer watcher.Close()
		}

		response, err := driver.NodeUnprepareResources(context.TODO(), testcase.request)
		if err != nil {
			t.Errorf("%v: error %v, expected no error", testcase.name, err)
			continue
		}

		preparedClaims, err := readPreparedClaimsFromFile(preparedClaimsFilePath)
		if err != nil {
			t.Errorf("%v: error %v, expected no error", testcase.name, err)
			continue
		}

		if !reflect.DeepEqual(response, testcase.expectedResponse) {
			t.Errorf("%v: unexpected response: %+v, expected response: %v", testcase.name, response, testcase.expectedResponse)
		}

		if !reflect.DeepEqual(testcase.expectedPreparedClaims, preparedClaims) {
			preparedClaimsJSON, _ := json.MarshalIndent(preparedClaims, "", "\t")
			expectedPreparedClaimsJSON, _ := json.MarshalIndent(testcase.expectedPreparedClaims, "", "\t")
			t.Errorf(
				"%v: unexpected PreparedClaims:\n%s\nexpected PreparedClaims:\n%s",
				testcase.name, preparedClaimsJSON, expectedPreparedClaimsJSON,
			)
		}
	}
}

func compareNodePrepareResourcesResponses(expectedResponse, response *v1alpha3.NodePrepareResourcesResponse) bool {
	if len(response.Claims) != len(expectedResponse.Claims) {
		return false
	}

	for expClaimUID, expClaim := range expectedResponse.Claims {
		claim, found := response.Claims[expClaimUID]
		if !found {
			return false
		}

		if expClaim.Error != claim.Error || len(expClaim.CDIDevices) != len(claim.CDIDevices) {
			return false
		}

		for _, expGPU := range expClaim.CDIDevices {
			found := false
			for _, gpu := range claim.CDIDevices {
				if gpu == expGPU {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}
	return true
}
