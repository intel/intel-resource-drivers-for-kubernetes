/*
 * Copyright (c) 2025, Intel Corporation.  All Rights Reserved.
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

	resourcev1 "k8s.io/api/resource/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
	drav1 "k8s.io/kubelet/pkg/apis/dra/v1beta1"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/fakesysfs"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gaudi/device"
	helpers "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/helpers"
	testhelpers "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/plugintesthelpers"
)

const (
	NoHealthcare   = false
	WithHealthcare = true
)

func TestGaudiFakeSysfs(t *testing.T) {
	testDirs, err := testhelpers.NewTestDirs(device.DriverName)
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

func getFakeDriver(testDirs testhelpers.TestDirsType, healthcare bool) (*driver, error) {

	gaudiFlags := GaudiFlags{
		Healthcare:         healthcare,
		HealthcareInterval: 1,
	}

	config := &helpers.Config{
		CommonFlags: &helpers.Flags{
			NodeName:                  "node1",
			CdiRoot:                   testDirs.CdiRoot,
			KubeletPluginDir:          testDirs.KubeletPluginDir,
			KubeletPluginsRegistryDir: testDirs.KubeletPluginRegistryDir,
		},
		Coreclient:  kubefake.NewSimpleClientset(),
		DriverFlags: &gaudiFlags,
	}

	os.Setenv("SYSFS_ROOT", testDirs.SysfsRoot)

	helperDriver, err := newDriver(context.TODO(), config)
	if err != nil {
		return nil, err
	}

	driver, ok := helperDriver.(*driver)
	if !ok {
		return nil, fmt.Errorf("type assertion failed: expected driver, got %T", helperDriver)
	}
	return driver, err
}

func TestGaudiNodePrepareResources(t *testing.T) {
	type testCase struct {
		name                   string
		claims                 []*resourcev1.ResourceClaim
		request                *drav1.NodePrepareResourcesRequest
		expectedResponse       *drav1.NodePrepareResourcesResponse
		preparedClaims         helpers.ClaimPreparations
		expectedPreparedClaims helpers.ClaimPreparations
		noDetectedDevices      bool
	}

	testcases := []testCase{
		{
			name: "one Gaudi success",
			claims: []*resourcev1.ResourceClaim{
				testhelpers.NewClaim("default", "claim1", "uid1", "request1", "gaudi.intel.com", "node1", []string{"0000-00-02-0-0x1020"}),
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
			expectedPreparedClaims: helpers.ClaimPreparations{
				"uid1": {{RequestNames: []string{"request1"}, PoolName: "node1", DeviceName: "0000-00-02-0-0x1020", CDIDeviceIDs: []string{"intel.com/gaudi=0000-00-02-0-0x1020", "intel.com/gaudi=uid1"}}},
			},
		},
		{
			name: "single Gaudi, already prepared claim",
			claims: []*resourcev1.ResourceClaim{
				testhelpers.NewClaim("namespace2", "claim2", "uid2", "request2", "gaudi.intel.com", "node1", []string{"0000-00-02-0-0x1020"}),
			},
			request: &drav1.NodePrepareResourcesRequest{
				Claims: []*drav1.Claim{{Name: "claim2", Namespace: "namespace2", UID: "uid2"}},
			},
			expectedResponse: &drav1.NodePrepareResourcesResponse{
				Claims: map[string]*drav1.NodePrepareResourceResponse{
					"uid2": {Devices: []*drav1.Device{{RequestNames: []string{"request2"}, PoolName: "node1", DeviceName: "0000-00-02-0-0x1020", CDIDeviceIDs: []string{"intel.com/gaudi=0000-00-02-0-0x1020", "intel.com/gaudi=uid2"}}}},
				},
			},
			preparedClaims: helpers.ClaimPreparations{
				"uid2": {{RequestNames: []string{"request2"}, PoolName: "node1", DeviceName: "0000-00-02-0-0x1020", CDIDeviceIDs: []string{"intel.com/gaudi=0000-00-02-0-0x1020", "intel.com/gaudi=uid2"}}},
			},
			expectedPreparedClaims: helpers.ClaimPreparations{
				"uid2": {{RequestNames: []string{"request2"}, PoolName: "node1", DeviceName: "0000-00-02-0-0x1020", CDIDeviceIDs: []string{"intel.com/gaudi=0000-00-02-0-0x1020", "intel.com/gaudi=uid2"}}},
			},
		},
		{
			name: "single unavailable device",
			claims: []*resourcev1.ResourceClaim{
				testhelpers.NewClaim("namespace3", "claim3", "uid3", "request3", "gaudi.intel.com", "node1", []string{"0000-00-05-0-0x1020"}),
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
		{
			name: "wrong namespace in claims",
			claims: []*resourcev1.ResourceClaim{
				testhelpers.NewClaim("wrong-namespace", "claim4", "uid4", "request4", "gaudi.intel.com", "node1", []string{"0000-00-05-0-0x1020"}),
			},
			request: &drav1.NodePrepareResourcesRequest{
				Claims: []*drav1.Claim{{Name: "claim4", Namespace: "namespace4", UID: "uid4"}},
			},
			expectedResponse: &drav1.NodePrepareResourcesResponse{
				Claims: map[string]*drav1.NodePrepareResourceResponse{
					"uid4": {Error: "could not find ResourceClaim claim4 in namespace namespace4: resourceclaims.resource.k8s.io \"claim4\" not found"},
				},
			},
		},
		{
			name:              "no devices detected",
			noDetectedDevices: true,
			claims: []*resourcev1.ResourceClaim{
				testhelpers.NewClaim("default", "claim5", "uid5", "request5", "gaudi.intel.com", "node1", []string{"0000-00-02-0-0x1020"}),
			},
			request: &drav1.NodePrepareResourcesRequest{
				Claims: []*drav1.Claim{{UID: "uid5", Name: "claim5", Namespace: "default"}},
			},
			expectedResponse: &drav1.NodePrepareResourcesResponse{
				Claims: map[string]*drav1.NodePrepareResourceResponse{
					"uid5": {Error: "could not find allocatable device 0000-00-02-0-0x1020 (pool node1)"},
				},
			},
		},
	}

	for _, testcase := range testcases {
		t.Log(testcase.name)

		testDirs, err := testhelpers.NewTestDirs(device.DriverName)
		defer testhelpers.CleanupTest(t, testcase.name, testDirs.TestRoot)
		if err != nil {
			t.Errorf("%v: setup error: %v", testcase.name, err)
			return
		}

		fakeGaudis := device.DevicesInfo{
			"0000-00-02-0-0x1020": {Model: "0x1020", DeviceIdx: 0, PCIAddress: "0000:00:02.0", UID: "0000-00-02-0-0x1020"},
			"0000-00-03-0-0x1020": {Model: "0x1020", DeviceIdx: 1, PCIAddress: "0000:00:03.0", UID: "0000-00-03-0-0x1020"},
			"0000-00-04-0-0x1020": {Model: "0x1020", DeviceIdx: 2, PCIAddress: "0000:00:04.0", UID: "0000-00-04-0-0x1020"},
		}

		if testcase.noDetectedDevices {
			fakeGaudis = device.DevicesInfo{}
		}

		if err := fakesysfs.FakeSysFsGaudiContents(testDirs.SysfsRoot, testDirs.DevfsRoot, fakeGaudis, false); err != nil {
			t.Errorf("setup error: could not create fake sysfs: %v", err)
			return
		}

		preparedClaimFilePath := path.Join(testDirs.KubeletPluginDir, "preparedClaims.json")
		if err := helpers.WritePreparedClaimsToFile(preparedClaimFilePath, testcase.preparedClaims); err != nil {
			t.Errorf("%v: error %v, writing prepared claims to file", testcase.name, err)
			continue
		}

		driver, driverErr := getFakeDriver(testDirs, NoHealthcare)
		if driverErr != nil {
			t.Errorf("could not create kubelet-plugin: %v\n", driverErr)
			continue
		}

		for _, testClaim := range testcase.claims {
			createdClaim, err := driver.client.ResourceV1beta1().ResourceClaims(testClaim.Namespace).Create(context.TODO(), testClaim, metav1.CreateOptions{})
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

		preparedClaims, err := helpers.ReadPreparedClaimsFromFile(preparedClaimFilePath)
		if err != nil {
			t.Errorf("%v: error %v, expected no error", testcase.name, err)
			continue
		}

		expectedPreparedClaims := testcase.expectedPreparedClaims
		if expectedPreparedClaims == nil {
			expectedPreparedClaims = helpers.ClaimPreparations{}
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

func TestGaudiNodeUnprepareResources(t *testing.T) {
	type testCase struct {
		name                   string
		request                *drav1.NodeUnprepareResourcesRequest
		expectedResponse       *drav1.NodeUnprepareResourcesResponse
		preparedClaims         helpers.ClaimPreparations
		expectedPreparedClaims helpers.ClaimPreparations
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
			preparedClaims:         helpers.ClaimPreparations{},
			expectedPreparedClaims: helpers.ClaimPreparations{},
		},
		{
			name: "single claim",
			request: &drav1.NodeUnprepareResourcesRequest{
				Claims: []*drav1.Claim{
					{Name: "claim1", Namespace: "namespace1", UID: "uid1"},
				},
			},
			expectedResponse: &drav1.NodeUnprepareResourcesResponse{
				Claims: map[string]*drav1.NodeUnprepareResourceResponse{"uid1": {}},
			},
			preparedClaims: helpers.ClaimPreparations{
				"uid1": {{RequestNames: []string{"request1"}, PoolName: "node1", DeviceName: "0000-00-02-0-0x1020", CDIDeviceIDs: []string{"intel.com/gaudi=0000-00-02-0-0x1020", "intel.com/gaudi=uid1"}}},
			},
			expectedPreparedClaims: helpers.ClaimPreparations{},
		},
		{
			name: "subset of claims",
			request: &drav1.NodeUnprepareResourcesRequest{
				Claims: []*drav1.Claim{
					{Name: "claim2", Namespace: "namespace2", UID: "uid2"},
				},
			},
			expectedResponse: &drav1.NodeUnprepareResourcesResponse{
				Claims: map[string]*drav1.NodeUnprepareResourceResponse{"uid2": {}},
			},
			preparedClaims: helpers.ClaimPreparations{
				"uid1": {{RequestNames: []string{"request1"}, PoolName: "node1", DeviceName: "0000-af-00-0-0x1020", CDIDeviceIDs: []string{"intel.com/gaudi=0000-af-00-0-0x1020", "intel.com/gaudi=uid1"}}},
				"uid2": {{RequestNames: []string{"request2"}, PoolName: "node1", DeviceName: "0000-b3-00-0-0x1020", CDIDeviceIDs: []string{"intel.com/gaudi=0000-b3-00-0-0x1020", "intel.com/gaudi=uid2"}}},
			},
			expectedPreparedClaims: helpers.ClaimPreparations{
				"uid1": {{RequestNames: []string{"request1"}, PoolName: "node1", DeviceName: "0000-af-00-0-0x1020", CDIDeviceIDs: []string{"intel.com/gaudi=0000-af-00-0-0x1020", "intel.com/gaudi=uid1"}}},
			},
		},
		{
			name: "non-existent claim success",
			request: &drav1.NodeUnprepareResourcesRequest{
				Claims: []*drav1.Claim{
					{Name: "claim1", Namespace: "namespace1", UID: "uid1"},
				},
			},
			expectedResponse: &drav1.NodeUnprepareResourcesResponse{
				Claims: map[string]*drav1.NodeUnprepareResourceResponse{"uid1": {}},
			},
			preparedClaims: helpers.ClaimPreparations{
				"uid2": {{RequestNames: []string{"request2"}, PoolName: "node1", DeviceName: "0000-b3-00-0-0x1020", CDIDeviceIDs: []string{"intel.com/gaudi=0000-b3-00-0-0x1020", "intel.com/gaudi=uid2"}}},
			},
			expectedPreparedClaims: helpers.ClaimPreparations{
				"uid2": {{RequestNames: []string{"request2"}, PoolName: "node1", DeviceName: "0000-b3-00-0-0x1020", CDIDeviceIDs: []string{"intel.com/gaudi=0000-b3-00-0-0x1020", "intel.com/gaudi=uid2"}}},
			},
		},
	}

	for _, testcase := range testcases {
		t.Log(testcase.name)

		testDirs, err := testhelpers.NewTestDirs(device.DriverName)
		defer testhelpers.CleanupTest(t, testcase.name, testDirs.TestRoot)
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
			false,
		); err != nil {
			t.Errorf("setup error: could not create fake sysfs: %v", err)
			return
		}

		preparedClaimFilePath := path.Join(testDirs.KubeletPluginDir, "preparedClaims.json")
		if err := helpers.WritePreparedClaimsToFile(preparedClaimFilePath, testcase.preparedClaims); err != nil {
			t.Errorf("%v: error %v, writing prepared claims to file", testcase.name, err)
			continue
		}

		driver, driverErr := getFakeDriver(testDirs, NoHealthcare)
		if driverErr != nil {
			t.Errorf("could not create kubelet-plugin: %v\n", driverErr)
			continue
		}

		response, err := driver.NodeUnprepareResources(context.TODO(), testcase.request)
		if err != nil {
			t.Errorf("%v: error %v, expected no error", testcase.name, err)
			continue
		}

		preparedClaims, err := helpers.ReadPreparedClaimsFromFile(preparedClaimFilePath)
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
				testcase.name, string(preparedClaimsJSON), string(expectedPreparedClaimsJSON),
			)
			break
		}
	}
}

func TestGaudiShutdown(t *testing.T) {
	testDirs, err := testhelpers.NewTestDirs(device.DriverName)
	if err != nil {
		t.Fatalf("could not create fake system dirs: %v", err)
	}

	driver, err := getFakeDriver(testDirs, NoHealthcare)
	if err != nil {
		t.Fatalf("could not create driver: %v", err)
	}

	err = driver.Shutdown(context.TODO())
	if err != nil {
		t.Errorf("Shutdown() error = %v, wantErr %v", err, nil)
	}
}
