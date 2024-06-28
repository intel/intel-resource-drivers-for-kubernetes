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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/kubelet/pkg/apis/dra/v1alpha3"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/fakesysfs"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gaudi/device"
	gaudicsfake "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gaudi/clientset/versioned/fake"
	gaudiv1alpha1 "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gaudi/v1alpha1"
	intelcrd "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gaudi/v1alpha1/api"
	helpers "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/plugintesthelpers"
)

func TestFakeSysfs(t *testing.T) {
	fakeSysfsRoot := "/tmp/fakegaudisysfs"

	if err := fakesysfs.FakeSysFsGaudiContents(
		fakeSysfsRoot,
		device.DevicesInfo{
			"0000-0f-00-0-0x1020": {Model: "0x1020", PCIAddress: "0000:0f:00.0", DeviceIdx: 0, UID: "0000-0f-00-0-0x1020"},
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

	fakeGas := &gaudiv1alpha1.GaudiAllocationState{
		TypeMeta:   metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{Namespace: "namespace1", Name: "node1"},
		Status:     "Ready",
		Spec:       gaudiv1alpha1.GaudiAllocationStateSpec{},
	}
	fakeDRAClient := gaudicsfake.NewSimpleClientset(fakeGas)

	config := &configType{
		crdconfig: &intelcrd.GaudiAllocationStateConfig{
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
		gasSpecAllocations map[string]gaudiv1alpha1.AllocatedClaim
		preparedClaims     ClaimPreparations
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
			preparedClaims: nil,
		},
		{
			name: "single device",
			request: &v1alpha3.NodePrepareResourcesRequest{
				Claims: []*v1alpha3.Claim{
					{Name: "claim1", Namespace: "namespace1", Uid: "uid1"},
				},
			},
			expectedResponse: &v1alpha3.NodePrepareResourcesResponse{
				Claims: map[string]*v1alpha3.NodePrepareResourceResponse{
					"uid1": {CDIDevices: []string{"intel.com/gaudi=0000-00-02-0-0x1020"}},
				},
			},
			gasSpecAllocations: map[string]gaudiv1alpha1.AllocatedClaim{
				"uid1": {Devices: []gaudiv1alpha1.AllocatedDevice{{UID: "0000-00-02-0-0x1020"}}},
			},
			preparedClaims: nil,
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
							"intel.com/gaudi=0000-00-02-0-0x1020",
							"intel.com/gaudi=0000-00-03-0-0x1020",
						},
					},
				},
			},
			gasSpecAllocations: map[string]gaudiv1alpha1.AllocatedClaim{},
			preparedClaims:     nil,
		},
		{
			name: "single Gaudi, already prepared claim",
			request: &v1alpha3.NodePrepareResourcesRequest{
				Claims: []*v1alpha3.Claim{
					{Name: "claim1", Namespace: "namespace1", Uid: "uid1"},
				},
			},
			expectedResponse: &v1alpha3.NodePrepareResourcesResponse{
				Claims: map[string]*v1alpha3.NodePrepareResourceResponse{
					"uid1": {CDIDevices: []string{"intel.com/gaudi=0000-00-02-0-0x1020"}},
				},
			},
			gasSpecAllocations: map[string]gaudiv1alpha1.AllocatedClaim{
				"uid1": {Devices: []gaudiv1alpha1.AllocatedDevice{{UID: "0000-00-02-0-0x1020"}}},
			},
			preparedClaims: ClaimPreparations{
				"uid1": {{UID: "0000-00-02-0-0x1020"}},
			},
		},
		{
			name: "single unavailable device",
			request: &v1alpha3.NodePrepareResourcesRequest{
				Claims: []*v1alpha3.Claim{
					{Name: "claim1", Namespace: "namespace1", Uid: "uid1"},
				},
			},
			expectedResponse: &v1alpha3.NodePrepareResourcesResponse{
				Claims: map[string]*v1alpha3.NodePrepareResourceResponse{
					"uid1": {Error: "failed validating devices to prepare: allocated device 0000-00-04-0-0x1020 not found in API"},
				},
			},
			gasSpecAllocations: map[string]gaudiv1alpha1.AllocatedClaim{
				"uid1": {Devices: []gaudiv1alpha1.AllocatedDevice{{UID: "0000-00-04-0-0x1020"}}},
			},
			preparedClaims: nil,
		},
		{
			name: "missing claim allocation",
			request: &v1alpha3.NodePrepareResourcesRequest{
				Claims: []*v1alpha3.Claim{
					{Name: "claim2", Namespace: "namespace2", Uid: "uid2"},
				},
			},
			expectedResponse: &v1alpha3.NodePrepareResourcesResponse{
				Claims: map[string]*v1alpha3.NodePrepareResourceResponse{
					"uid2": {Error: "failed validating devices to prepare: no allocation found for claim uid2 in API"},
				},
			},
			gasSpecAllocations: map[string]gaudiv1alpha1.AllocatedClaim{
				"uid1": {Devices: []gaudiv1alpha1.AllocatedDevice{{UID: "0000-00-04-0-0x1020"}}},
			},
			preparedClaims: nil,
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

		preparedClaimsFilePath := path.Join(testDirs.DriverPluginRoot, device.PreparedClaimsFileName)

		if err := fakesysfs.FakeSysFsGaudiContents(
			testDirs.SysfsRoot,
			device.DevicesInfo{
				"0000-00-02-0-0x1020": {Model: "0x1020", PCIAddress: "0000:00:02.0", DeviceIdx: 0, UID: "0000-00-02-0-0x1020"},
				"0000-00-03-0-0x1020": {Model: "0x1020", PCIAddress: "0000:00:03.0", DeviceIdx: 1, UID: "0000-00-03-0-0x1020"},
			},
		); err != nil {
			t.Errorf("setup error: could not create fake sysfs: %v", err)
			return
		}

		if err := writePreparedClaimsToFile(preparedClaimsFilePath, testcase.preparedClaims); err != nil {
			t.Errorf("%v: error %v, writing prepared claims to file", testcase.name, err)
		}

		driver, driverErr := getFakeDriver(testDirs)
		if driverErr != nil {
			t.Errorf("could not create kubelet-plugin: %v\n", driverErr)
		}

		// cleanup and setup GAS
		gasspec := driver.gas.Spec.DeepCopy()
		gasspec.AllocatedClaims = testcase.gasSpecAllocations

		if err := driver.gas.Update(context.TODO(), gasspec); err != nil {
			t.Error("setup error: could not prepare GAS")
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

func TestNodeUnprepareResources(t *testing.T) {
	type testCase struct {
		name                   string
		request                *v1alpha3.NodeUnprepareResourcesRequest
		expectedResponse       *v1alpha3.NodeUnprepareResourcesResponse
		preparedClaims         ClaimPreparations
		expectedPreparedClaims ClaimPreparations
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
			name: "single claim",
			request: &v1alpha3.NodeUnprepareResourcesRequest{
				Claims: []*v1alpha3.Claim{
					{Name: "claim1", Namespace: "namespace1", Uid: "cuid1"},
				},
			},
			expectedResponse: &v1alpha3.NodeUnprepareResourcesResponse{
				Claims: map[string]*v1alpha3.NodeUnprepareResourceResponse{"cuid1": {}},
			},
			preparedClaims: ClaimPreparations{
				"cuid1": {{UID: "0000-b3-00-0-0x1020"}},
			},
			expectedPreparedClaims: ClaimPreparations{},
		},
		{
			name: "subset of claims",
			request: &v1alpha3.NodeUnprepareResourcesRequest{
				Claims: []*v1alpha3.Claim{
					{Name: "claim2", Namespace: "namespace2", Uid: "cuid2"},
				},
			},
			expectedResponse: &v1alpha3.NodeUnprepareResourcesResponse{
				Claims: map[string]*v1alpha3.NodeUnprepareResourceResponse{"cuid2": {}},
			},
			preparedClaims: ClaimPreparations{
				"cuid1": {{UID: "0000-af-00-0-0x1020"}},
				"cuid2": {{UID: "0000-b3-00-0-0x1020"}},
			},
			expectedPreparedClaims: ClaimPreparations{
				"cuid1": {{UID: "0000-af-00-0-0x1020", PCIAddress: "0000:af:00.0", DeviceIdx: 1, Model: "0x1020"}},
			},
		},
		{
			name: "non-existent claim success",
			request: &v1alpha3.NodeUnprepareResourcesRequest{
				Claims: []*v1alpha3.Claim{
					{Name: "claim1", Namespace: "namespace1", Uid: "cuid1"},
				},
			},
			expectedResponse: &v1alpha3.NodeUnprepareResourcesResponse{
				Claims: map[string]*v1alpha3.NodeUnprepareResourceResponse{"cuid1": {}},
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
