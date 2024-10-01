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

	resourcev1 "k8s.io/api/resource/v1alpha3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"

	drav1 "k8s.io/kubelet/pkg/apis/dra/v1alpha4"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/fakesysfs"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
	helpers "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/plugintesthelpers"
)

func TestFakeSysfs(t *testing.T) {
	testDirs, err := helpers.NewTestDirs(device.DriverName)
	if err != nil {
		t.Errorf("could not create fake system dirs: %v", err)
		return
	}

	if err := fakesysfs.FakeSysFsGpuContents(
		testDirs.SysfsRoot,
		testDirs.DevfsRoot,
		device.DevicesInfo{
			"0000-00-02-0-0x56c0": {Model: "0x56c0", MemoryMiB: 8192, DeviceType: "gpu", CardIdx: 0, RenderdIdx: 128, UID: "0000-00-02-0-0x56c0", MaxVFs: 16},
		},
		false,
	); err != nil {
		t.Errorf("setup error: could not create fake sysfs: %v", err)
		return
	}

	if err := os.RemoveAll(testDirs.TestRoot); err != nil {
		t.Errorf("could not cleanup fake sysfs %v", testDirs.TestRoot)
	}
}

func getFakeDriver(testDirs helpers.TestDirsType) (*driver, error) {

	config := &configType{
		nodeName:                  "node1",
		clientset:                 kubefake.NewSimpleClientset(),
		cdiRoot:                   testDirs.CdiRoot,
		kubeletPluginDir:          testDirs.KubeletPluginDir,
		kubeletPluginsRegistryDir: testDirs.KubeletPluginRegistryDir,
	}

	if err := os.MkdirAll(config.kubeletPluginDir, 0755); err != nil {
		return nil, fmt.Errorf("failed creating fake driver plugin dir: %v", err)
	}
	if err := os.MkdirAll(config.kubeletPluginsRegistryDir, 0755); err != nil {
		return nil, fmt.Errorf("failed creating fake driver plugin dir: %v", err)
	}

	os.Setenv("SYSFS_ROOT", testDirs.SysfsRoot)

	return newDriver(context.TODO(), config)
}

func TestNodePrepareResources(t *testing.T) {
	type testCase struct {
		name                   string
		claims                 []*resourcev1.ResourceClaim
		request                *drav1.NodePrepareResourcesRequest
		expectedResponse       *drav1.NodePrepareResourcesResponse
		preparedClaims         ClaimPreparations
		expectedPreparedClaims ClaimPreparations
	}

	testcases := []testCase{
		{
			name: "blank request",
			request: &drav1.NodePrepareResourcesRequest{
				Claims: []*drav1.Claim{},
			},
			expectedResponse: &drav1.NodePrepareResourcesResponse{
				Claims: map[string]*drav1.NodePrepareResourceResponse{},
			},
			preparedClaims: ClaimPreparations{},
		},
		{
			name: "single GPU",
			claims: []*resourcev1.ResourceClaim{
				helpers.NewClaim("namespace1", "claim1", "uid1", "request1", "gpu.intel.com", "node1", []string{"0000-00-02-0-0x56c0"}),
			},
			request: &drav1.NodePrepareResourcesRequest{
				Claims: []*drav1.Claim{
					{UID: "uid1", Name: "claim1", Namespace: "namespace1"},
				},
			},
			expectedResponse: &drav1.NodePrepareResourcesResponse{
				Claims: map[string]*drav1.NodePrepareResourceResponse{
					"uid1": {
						Devices: []*drav1.Device{
							{RequestNames: []string{"request1"}, PoolName: "node1", DeviceName: "0000-00-02-0-0x56c0", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-02-0-0x56c0"}},
						},
					},
				},
			},
			preparedClaims: ClaimPreparations{},
			expectedPreparedClaims: ClaimPreparations{
				"uid1": {
					{
						RequestNames: []string{"request1"},
						PoolName:     "node1",
						DeviceName:   "0000-00-02-0-0x56c0",
						CDIDeviceIDs: []string{"intel.com/gpu=0000-00-02-0-0x56c0"},
					},
				},
			},
		},
		{
			name: "single existing VF",
			claims: []*resourcev1.ResourceClaim{
				helpers.NewClaim("namespace2", "claim2", "uid2", "request2", "gpu.intel.com", "node1", []string{"0000-00-03-1-0x56c0"}),
			},
			request: &drav1.NodePrepareResourcesRequest{
				Claims: []*drav1.Claim{
					{Name: "claim2", Namespace: "namespace2", UID: "uid2"},
				},
			},
			expectedResponse: &drav1.NodePrepareResourcesResponse{
				Claims: map[string]*drav1.NodePrepareResourceResponse{
					"uid2": {
						Devices: []*drav1.Device{
							{RequestNames: []string{"request2"}, PoolName: "node1", DeviceName: "0000-00-03-1-0x56c0", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-1-0x56c0"}},
						},
					},
				},
			},
			preparedClaims: ClaimPreparations{},
			expectedPreparedClaims: ClaimPreparations{
				"uid2": {
					{
						RequestNames: []string{"request2"},
						PoolName:     "node1",
						DeviceName:   "0000-00-03-1-0x56c0",
						CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-1-0x56c0"},
					},
				},
			},
		},
		{
			name: "monitoring claim",
			claims: []*resourcev1.ResourceClaim{
				helpers.NewMonitoringClaim(
					"namespace3", "monitor", "uid3", "monitor", "gpu.intel.com", "node1", []string{"0000-00-02-0-0x56c0", "0000-00-03-0-0x56c0", "0000-00-03-1-0x56c0", "0000-00-04-0-0x0000"}),
			},
			request: &drav1.NodePrepareResourcesRequest{
				Claims: []*drav1.Claim{
					{Name: "monitor", Namespace: "namespace3", UID: "uid3"},
				},
			},
			expectedResponse: &drav1.NodePrepareResourcesResponse{
				Claims: map[string]*drav1.NodePrepareResourceResponse{
					"uid3": {
						Devices: []*drav1.Device{
							{RequestNames: []string{"monitor"}, PoolName: "node1", DeviceName: "0000-00-02-0-0x56c0", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-02-0-0x56c0"}},
							{RequestNames: []string{"monitor"}, PoolName: "node1", DeviceName: "0000-00-03-0-0x56c0", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-0-0x56c0"}},
							{RequestNames: []string{"monitor"}, PoolName: "node1", DeviceName: "0000-00-03-1-0x56c0", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-1-0x56c0"}},
							{RequestNames: []string{"monitor"}, PoolName: "node1", DeviceName: "0000-00-04-0-0x0000", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-04-0-0x0000"}},
						},
					},
				},
			},
			preparedClaims: ClaimPreparations{},
			expectedPreparedClaims: ClaimPreparations{
				"uid3": {
					{RequestNames: []string{"monitor"}, PoolName: "node1", DeviceName: "0000-00-02-0-0x56c0", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-02-0-0x56c0"}},
					{RequestNames: []string{"monitor"}, PoolName: "node1", DeviceName: "0000-00-03-0-0x56c0", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-0-0x56c0"}},
					{RequestNames: []string{"monitor"}, PoolName: "node1", DeviceName: "0000-00-03-1-0x56c0", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-1-0x56c0"}},
					{RequestNames: []string{"monitor"}, PoolName: "node1", DeviceName: "0000-00-04-0-0x0000", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-04-0-0x0000"}},
				},
			},
		},
		{
			name: "single GPU, already prepared claim",
			claims: []*resourcev1.ResourceClaim{
				helpers.NewMonitoringClaim("namespace4", "claim4", "uid4", "request4", "gpu.intel.com", "node1", []string{"0000-00-03-1-0x56c0"}),
			},
			request: &drav1.NodePrepareResourcesRequest{
				Claims: []*drav1.Claim{
					{Name: "claim4", Namespace: "namespace4", UID: "uid4"},
				},
			},
			expectedResponse: &drav1.NodePrepareResourcesResponse{
				Claims: map[string]*drav1.NodePrepareResourceResponse{
					"uid4": {
						Devices: []*drav1.Device{
							{
								RequestNames: []string{"request4"}, PoolName: "node1", DeviceName: "0000-00-03-1-0x56c0", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-1-0x56c0"},
							},
						},
					},
				},
			},
			preparedClaims: ClaimPreparations{
				"uid4": {
					{
						RequestNames: []string{"request4"},
						PoolName:     "node1",
						DeviceName:   "0000-00-03-1-0x56c0",
						CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-1-0x56c0"},
					},
				},
			},
			expectedPreparedClaims: ClaimPreparations{
				"uid4": {
					{
						RequestNames: []string{"request4"},
						PoolName:     "node1",
						DeviceName:   "0000-00-03-1-0x56c0",
						CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-1-0x56c0"},
					},
				},
			},
		},
	}

	for _, testcase := range testcases {
		t.Log(testcase.name)

		testDirs, err := helpers.NewTestDirs(device.DriverName)
		defer helpers.CleanupTest(t, testcase.name, testDirs.TestRoot)
		if err != nil {
			t.Errorf("%v: setup error: %v", testcase.name, err)
			return
		}

		if err := fakesysfs.FakeSysFsGpuContents(
			testDirs.SysfsRoot,
			testDirs.DevfsRoot,
			device.DevicesInfo{
				"0000-00-02-0-0x56c0": {Model: "0x56c0", MemoryMiB: 16256, DeviceType: "gpu", CardIdx: 0, RenderdIdx: 128, UID: "0000-00-02-0-0x56c0", MaxVFs: 16},
				"0000-00-03-0-0x56c0": {Model: "0x56c0", MemoryMiB: 16256, DeviceType: "gpu", CardIdx: 1, RenderdIdx: 129, UID: "0000-00-03-0-0x56c0", MaxVFs: 16},
				"0000-00-03-1-0x56c0": {Model: "0x56c0", MemoryMiB: 8064, DeviceType: "vf", CardIdx: 2, RenderdIdx: 130, UID: "0000-00-03-1-0x56c0", VFIndex: 0, VFProfile: "flex170_m2", ParentUID: "0000-00-03-0-0x56c0"},
				// dummy, no SR-IOV tiles
				"0000-00-04-0-0x0000": {Model: "0x0000", MemoryMiB: 14248, DeviceType: "gpu", CardIdx: 3, RenderdIdx: 131, UID: "0000-00-04-0-0x0000", MaxVFs: 16},
			},
			false,
		); err != nil {
			t.Errorf("setup error: could not create fake sysfs: %v", err)
			return
		}

		preparedClaimFilePath := path.Join(testDirs.KubeletPluginDir, device.PreparedClaimsFileName)
		if err := writePreparedClaimsToFile(preparedClaimFilePath, testcase.preparedClaims); err != nil {
			t.Errorf("%v: error %v, writing prepared claims to file", testcase.name, err)
		}

		driver, driverErr := getFakeDriver(testDirs)
		if driverErr != nil {
			t.Errorf("could not create kubelet-plugin: %v\n", driverErr)
			continue
		}

		for _, testClaim := range testcase.claims {
			createdClaim, err := driver.client.ResourceV1alpha3().ResourceClaims(testClaim.Namespace).Create(context.TODO(), testClaim, metav1.CreateOptions{})
			if err != nil {
				t.Errorf("could not create test claim: %v", err)
			}
			t.Logf("created test claim: %+v", createdClaim)
		}

		response, err := driver.NodePrepareResources(context.TODO(), testcase.request)
		if err != nil {
			t.Errorf("%v: error %v, expected no error", testcase.name, err)
		}

		if !reflect.DeepEqual(testcase.expectedResponse, response) {
			responseJSON, _ := json.MarshalIndent(response, "", "\t")
			expectedResponseJSON, _ := json.MarshalIndent(testcase.expectedResponse, "", "\t")
			t.Errorf("%v: unexpected response: %+v, expected response: %v", testcase.name, string(responseJSON), string(expectedResponseJSON))
		}

		preparedClaims, err := readPreparedClaimsFromFile(preparedClaimFilePath)
		if err != nil {
			t.Errorf("%v: error %v, expected no error", testcase.name, err)
			continue
		}

		expectedPreparedClaims := testcase.expectedPreparedClaims
		if expectedPreparedClaims == nil {
			expectedPreparedClaims = ClaimPreparations{}
		}

		if !reflect.DeepEqual(expectedPreparedClaims, preparedClaims) {
			preparedClaimsJSON, _ := json.MarshalIndent(preparedClaims, "", "\t")
			expectedPreparedClaimsJSON, _ := json.MarshalIndent(testcase.expectedPreparedClaims, "", "\t")
			t.Errorf(
				"%v: unexpected PreparedClaims:\n%s\nexpected PreparedClaims:\n%s",
				testcase.name, string(preparedClaimsJSON), string(expectedPreparedClaimsJSON),
			)
		}
	}
}

func TestNodeUnprepareResources(t *testing.T) {
	type testCase struct {
		name                   string
		request                *drav1.NodeUnprepareResourcesRequest
		expectedResponse       *drav1.NodeUnprepareResourcesResponse
		preparedClaims         ClaimPreparations
		expectedPreparedClaims ClaimPreparations
	}

	testcases := []testCase{
		{
			name: "blank request",
			request: &drav1.NodeUnprepareResourcesRequest{
				Claims: []*drav1.Claim{},
			},
			expectedResponse: &drav1.NodeUnprepareResourcesResponse{
				Claims: map[string]*drav1.NodeUnprepareResourceResponse{},
			},
			preparedClaims:         ClaimPreparations{},
			expectedPreparedClaims: ClaimPreparations{},
		},
		{
			name: "single GPU",
			request: &drav1.NodeUnprepareResourcesRequest{
				Claims: []*drav1.Claim{
					{Name: "claim1", Namespace: "namespace1", UID: "uid1"},
				},
			},
			expectedResponse: &drav1.NodeUnprepareResourcesResponse{
				Claims: map[string]*drav1.NodeUnprepareResourceResponse{"uid1": {}},
			},
			preparedClaims: ClaimPreparations{
				"uid1": {{RequestNames: []string{"request1"}, PoolName: "node1", DeviceName: "0000-b3-00-0-0x0bda", CDIDeviceIDs: []string{"intel.com/gpu=0000-b3-00-0-0x0bda"}}},
			},
			expectedPreparedClaims: ClaimPreparations{},
		},
		{
			name: "single VF without cleanup",
			request: &drav1.NodeUnprepareResourcesRequest{
				Claims: []*drav1.Claim{
					{Name: "claim2", Namespace: "namespace2", UID: "uid2"},
				},
			},
			expectedResponse: &drav1.NodeUnprepareResourcesResponse{
				Claims: map[string]*drav1.NodeUnprepareResourceResponse{"uid2": {}},
			},
			preparedClaims: ClaimPreparations{
				"uid2": {{RequestNames: []string{"request2"}, PoolName: "node1", DeviceName: "0000-af-00-1-0x0bda", CDIDeviceIDs: []string{"intel.com/gpu=0000-af-00-1-0x0bda"}}},
				"uid3": {{RequestNames: []string{"request3"}, PoolName: "node1", DeviceName: "0000-af-00-2-0x0bda", CDIDeviceIDs: []string{"intel.com/gpu=0000-af-00-2-0x0bda"}}},
			},
			expectedPreparedClaims: ClaimPreparations{
				"uid3": {{RequestNames: []string{"request3"}, PoolName: "node1", DeviceName: "0000-af-00-2-0x0bda", CDIDeviceIDs: []string{"intel.com/gpu=0000-af-00-2-0x0bda"}}},
			},
		},
	}

	for _, testcase := range testcases {
		t.Log(testcase.name)

		testDirs, err := helpers.NewTestDirs(device.DriverName)
		defer helpers.CleanupTest(t, "TestNodeUnprepareResources", testDirs.TestRoot)
		if err != nil {
			t.Errorf("%v: setup error: %v", testcase.name, err)
			return
		}

		if err := fakesysfs.FakeSysFsGpuContents(
			testDirs.SysfsRoot,
			testDirs.DevfsRoot,
			device.DevicesInfo{
				"0000-b3-00-0-0x0bda": {Model: "0x0bda", MemoryMiB: 49136, DeviceType: "gpu", CardIdx: 0, UID: "0000-b3-00-0-0x0bda", MaxVFs: 63},
				"0000-af-00-0-0x0bda": {Model: "0x0bda", MemoryMiB: 49136, DeviceType: "gpu", CardIdx: 1, UID: "0000-af-00-0-0x0bda", MaxVFs: 63},
				"0000-af-00-1-0x0bda": {Model: "0x0bda", MemoryMiB: 22528, Millicores: 500, DeviceType: "vf", CardIdx: 2, UID: "0000-af-00-1-0x0bda", VFIndex: 0, VFProfile: "max_47g_c2", ParentUID: "0000-af-00-0-0x0bda"},
				"0000-af-00-2-0x0bda": {Model: "0x0bda", MemoryMiB: 22528, Millicores: 500, DeviceType: "vf", CardIdx: 3, UID: "0000-af-00-2-0x0bda", VFIndex: 1, VFProfile: "max_47g_c2", ParentUID: "0000-af-00-0-0x0bda"},
			},
			false,
		); err != nil {
			t.Errorf("%v: setup error: could not create fake sysfs: %v", testcase.name, err)
			return
		}

		preparedClaimsFilePath := path.Join(testDirs.KubeletPluginDir, device.PreparedClaimsFileName)
		if err := writePreparedClaimsToFile(preparedClaimsFilePath, testcase.preparedClaims); err != nil {
			t.Errorf("%v: error %v, writing prepared claims to file", testcase.name, err)
			continue
		}

		driver, driverErr := getFakeDriver(testDirs)
		if driverErr != nil {
			t.Errorf("could not create kubelet-plugin: %v\n", driverErr)
			continue
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
