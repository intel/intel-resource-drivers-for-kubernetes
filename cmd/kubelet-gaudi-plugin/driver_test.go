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
	"os"
	"path"
	"reflect"
	"testing"

	resourcev1 "k8s.io/api/resource/v1alpha3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"

	drav1 "k8s.io/kubelet/pkg/apis/dra/v1alpha4"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/fakesysfs"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gaudi/device"
	helpers "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/plugintesthelpers"
)

func TestFakeSysfs(t *testing.T) {
	testDirs, err := helpers.NewTestDirs(device.DriverName)
	if err != nil {
		t.Errorf("could not create fake system dirs: %v", err)
		return
	}

	if err := fakesysfs.FakeSysFsGaudiContents(
		testDirs.SysfsRoot,
		testDirs.DevfsRoot,
		device.DevicesInfo{
			"0000-0f-00-0-0x1020": {Model: "0x1020", PCIAddress: "0000:0f:00.0", DeviceIdx: 0, UID: "0000-0f-00-0-0x1020"},
		},
		false,
	); err != nil {
		t.Errorf("setup error: could not create fake sysfs: %v", err)
		return
	}

	if err := os.RemoveAll(testDirs.TestRoot); err != nil {
		t.Errorf("could not cleanup test root %v: %v", testDirs.TestRoot, err)
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
			name: "one Gaudi success",
			claims: []*resourcev1.ResourceClaim{
				helpers.NewClaim("default", "claim1", "uid1", "request1", "gaudi.intel.com", "node1", []string{"0000-00-02-0-0x1020"}),
			},
			request: &drav1.NodePrepareResourcesRequest{
				Claims: []*drav1.Claim{{UID: "uid1", Name: "claim1", Namespace: "default"}},
			},
			expectedResponse: &drav1.NodePrepareResourcesResponse{
				Claims: map[string]*drav1.NodePrepareResourceResponse{
					"uid1": {Devices: []*drav1.Device{{RequestNames: []string{"request1"}, PoolName: "node1", DeviceName: "0000-00-02-0-0x1020", CDIDeviceIDs: []string{"intel.com/gaudi=0000-00-02-0-0x1020", "intel.com/gaudi=uid1"}}}},
				},
			},
			preparedClaims: nil,
			expectedPreparedClaims: ClaimPreparations{
				"uid1": {{RequestNames: []string{"request1"}, PoolName: "node1", DeviceName: "0000-00-02-0-0x1020", CDIDeviceIDs: []string{"intel.com/gaudi=0000-00-02-0-0x1020", "intel.com/gaudi=uid1"}}},
			},
		},
		{
			name: "single Gaudi, already prepared claim",
			claims: []*resourcev1.ResourceClaim{
				helpers.NewClaim("namespace2", "claim2", "uid2", "request2", "gaudi.intel.com", "node1", []string{"0000-00-02-0-0x1020"}),
			},
			request: &drav1.NodePrepareResourcesRequest{
				Claims: []*drav1.Claim{{Name: "claim2", Namespace: "namespace2", UID: "uid2"}},
			},
			expectedResponse: &drav1.NodePrepareResourcesResponse{
				Claims: map[string]*drav1.NodePrepareResourceResponse{
					"uid2": {Devices: []*drav1.Device{{RequestNames: []string{"request2"}, PoolName: "node1", DeviceName: "0000-00-02-0-0x1020", CDIDeviceIDs: []string{"intel.com/gaudi=0000-00-02-0-0x1020", "intel.com/gaudi=uid2"}}}},
				},
			},
			preparedClaims: ClaimPreparations{
				"uid2": {{RequestNames: []string{"request2"}, PoolName: "node1", DeviceName: "0000-00-02-0-0x1020", CDIDeviceIDs: []string{"intel.com/gaudi=0000-00-02-0-0x1020", "intel.com/gaudi=uid2"}}},
			},
			expectedPreparedClaims: ClaimPreparations{
				"uid2": {{RequestNames: []string{"request2"}, PoolName: "node1", DeviceName: "0000-00-02-0-0x1020", CDIDeviceIDs: []string{"intel.com/gaudi=0000-00-02-0-0x1020", "intel.com/gaudi=uid2"}}},
			},
		},
		{
			name: "single unavailable device",
			claims: []*resourcev1.ResourceClaim{
				helpers.NewClaim("namespace3", "claim3", "uid3", "request3", "gaudi.intel.com", "node1", []string{"0000-00-05-0-0x1020"}),
			},
			request: &drav1.NodePrepareResourcesRequest{
				Claims: []*drav1.Claim{{Name: "claim3", Namespace: "namespace3", UID: "uid3"}},
			},
			expectedResponse: &drav1.NodePrepareResourcesResponse{
				Claims: map[string]*drav1.NodePrepareResourceResponse{
					"uid3": {Error: "could not find allocatable device 0000-00-05-0-0x1020 (pool node1)"},
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

		if err := fakesysfs.FakeSysFsGaudiContents(
			testDirs.SysfsRoot,
			testDirs.DevfsRoot,
			device.DevicesInfo{
				"0000-00-02-0-0x1020": {Model: "0x1020", DeviceIdx: 0, PCIAddress: "0000:00:02.0", UID: "0000-00-02-0-0x1020"},
				"0000-00-03-0-0x1020": {Model: "0x1020", DeviceIdx: 1, PCIAddress: "0000:00:03.0", UID: "0000-00-03-0-0x1020"},
				"0000-00-04-0-0x1020": {Model: "0x1020", DeviceIdx: 2, PCIAddress: "0000:00:04.0", UID: "0000-00-04-0-0x1020"},
			},
			false,
		); err != nil {
			t.Errorf("setup error: could not create fake sysfs: %v", err)
			return
		}

		preparedClaimFilePath := path.Join(testDirs.KubeletPluginDir, "preparedClaims.json")
		if err := writePreparedClaimsToFile(preparedClaimFilePath, testcase.preparedClaims); err != nil {
			t.Errorf("%v: error %v, writing prepared claims to file", testcase.name, err)
			continue
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
				continue
			}
			t.Logf("created test claim: %+v", createdClaim)
		}

		response, err := driver.NodePrepareResources(context.TODO(), testcase.request)
		if err != nil {
			t.Errorf("%v: error %v, expected no error", testcase.name, err)
			continue
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

/*
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
			name: "single claim",
			request: &drav1.NodeUnprepareResourcesRequest{
				Claims: []*drav1.Claim{
					{Name: "claim1", Namespace: "namespace1", Uid: "cuid1"},
				},
			},
			expectedResponse: &drav1.NodeUnprepareResourcesResponse{
				Claims: map[string]*drav1.NodeUnprepareResourceResponse{"cuid1": {}},
			},
			preparedClaims: ClaimPreparations{
				"cuid1": {{UID: "0000-b3-00-0-0x1020"}},
			},
			expectedPreparedClaims: ClaimPreparations{},
		},
		{
			name: "subset of claims",
			request: &drav1.NodeUnprepareResourcesRequest{
				Claims: []*drav1.Claim{
					{Name: "claim2", Namespace: "namespace2", Uid: "cuid2"},
				},
			},
			expectedResponse: &drav1.NodeUnprepareResourcesResponse{
				Claims: map[string]*drav1.NodeUnprepareResourceResponse{"cuid2": {}},
			},
			preparedClaims: ClaimPreparations{
				"cuid1": {{UID: "0000-af-00-0-0x1020"}},
				"cuid2": {{UID: "0000-b3-00-0-0x1020"}},
			},
			expectedPreparedClaims: ClaimPreparations{
				"cuid1": {{UID: "0000-af-00-0-0x1020", PCIAddress: "0000:af:00.0", DeviceIdx: 1, ModuleIdx: 1, Model: "0x1020"}},
			},
		},
		{
			name: "non-existent claim success",
			request: &drav1.NodeUnprepareResourcesRequest{
				Claims: []*drav1.Claim{
					{Name: "claim1", Namespace: "namespace1", Uid: "cuid1"},
				},
			},
			expectedResponse: &drav1.NodeUnprepareResourcesResponse{
				Claims: map[string]*drav1.NodeUnprepareResourceResponse{"cuid1": {}},
			},
			preparedClaims: ClaimPreparations{
				"cuid2": {{UID: "0000-b3-00-0-0x1020"}},
			},
			expectedPreparedClaims: ClaimPreparations{
				"cuid2": {{UID: "0000-b3-00-0-0x1020"}},
			},
		},
	}

	for _, testcase := range testcases {
		t.Log(testcase.name)

		testDirs, err := helpers.NewTestDirs()
		defer helpers.CleanupTest(t, testcase.name, testDirs.TestRoot)
		if err != nil {
			t.Errorf("%v: setup error: %v", testcase.name, err)
			return
		}

		if err := fakesysfs.FakeSysFsGaudiContents(
			testDirs.SysfsRoot,
			testDirs.DevfsRoot,
			device.DevicesInfo{
				"0000-b3-00-0-0x1020": {Model: "0x1020", PCIAddress: "0000:b3:00.0", DeviceIdx: 0, UID: "0000-b3-00-0-0x1020"},
				"0000-af-00-0-0x1020": {Model: "0x1020", PCIAddress: "0000:af:00.0", DeviceIdx: 1, UID: "0000-af-00-0-0x1020"},
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

		response, err := driver.NodeUnprepareResources(context.TODO(), testcase.request)
		if err != nil {
			t.Errorf("%v: error %v, expected no error", testcase.name, err)
		}

		preparedClaims, err := readPreparedClaimsFromFile(preparedClaimFilePath)
		if err != nil {
			t.Errorf("%v: error %v, expected no error", testcase.name, err)
		}

		if !reflect.DeepEqual(response, testcase.expectedResponse) {
			t.Errorf("%v: unexpected response: %+v, expected response: %v", testcase.name, response, testcase.expectedResponse)
		}

		if !reflect.DeepEqual(testcase.expectedPreparedClaims, preparedClaims) {
			preparedClaimsJSON, _ := json.MarshalIndent(preparedClaims, "", "\t")
			expectedPreparedClaimsJSON, _ := json.MarshalIndent(testcase.expectedPreparedClaims, "", "\t")
			t.Errorf(
				"unexpected PreparedClaims:\n%s\nexpected PreparedClaims:\n%s",
				preparedClaimsJSON, expectedPreparedClaimsJSON,
			)
			break
		}
	}
}

*/
