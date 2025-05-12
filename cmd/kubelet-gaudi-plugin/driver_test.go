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

	core "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"

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
	nodeName := "node1"
	gaudiFlags := GaudiFlags{
		Healthcare:         healthcare,
		HealthcareInterval: 1,
	}

	config := &helpers.Config{
		CommonFlags: &helpers.Flags{
			NodeName:                  nodeName,
			CdiRoot:                   testDirs.CdiRoot,
			KubeletPluginDir:          testDirs.KubeletPluginDir,
			KubeletPluginsRegistryDir: testDirs.KubeletPluginRegistryDir,
		},
		Coreclient:  kubefake.NewSimpleClientset(),
		DriverFlags: &gaudiFlags,
	}

	os.Setenv("SYSFS_ROOT", testDirs.SysfsRoot)

	// kubelet-plugin will access node object, it needs to exist.
	newNode := &core.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}
	if _, err := config.Coreclient.CoreV1().Nodes().Create(context.TODO(), newNode, metav1.CreateOptions{}); err != nil {
		return nil, fmt.Errorf("failed creating fake node object: %v", err)
	}

	helperDriver, err := newDriver(context.Background(), config)
	if err != nil {
		return nil, err
	}

	driver, ok := helperDriver.(*driver)
	if !ok {
		return nil, fmt.Errorf("type assertion failed: expected driver, got %T", helperDriver)
	}
	return driver, err
}

func TestGaudiPrepareResourceClaims(t *testing.T) {
	type testCase struct {
		name                   string
		request                []*resourcev1.ResourceClaim
		expectedResponse       map[types.UID]kubeletplugin.PrepareResult
		preparedClaims         helpers.ClaimPreparations
		expectedPreparedClaims helpers.ClaimPreparations
		noDetectedDevices      bool
	}

	testcases := []testCase{
		{
			name: "one Gaudi success",
			request: []*resourcev1.ResourceClaim{
				testhelpers.NewClaim("default", "claim1", "uid1", "request1", "gaudi.intel.com", "node1", []string{"0000-00-02-0-0x1020"}),
			},
			expectedResponse: map[types.UID]kubeletplugin.PrepareResult{
				"uid1": {
					Devices: []kubeletplugin.Device{
						{Requests: []string{"request1"}, PoolName: "node1", DeviceName: "0000-00-02-0-0x1020", CDIDeviceIDs: []string{"intel.com/gaudi=0000-00-02-0-0x1020", "intel.com/gaudi=uid1"}},
					},
				},
			},
			preparedClaims: nil,
			expectedPreparedClaims: helpers.ClaimPreparations{
				"uid1": {
					Devices: []kubeletplugin.Device{
						{Requests: []string{"request1"}, PoolName: "node1", DeviceName: "0000-00-02-0-0x1020", CDIDeviceIDs: []string{"intel.com/gaudi=0000-00-02-0-0x1020", "intel.com/gaudi=uid1"}},
					},
				},
			},
		},
		{
			name: "single Gaudi, already prepared claim",
			request: []*resourcev1.ResourceClaim{
				testhelpers.NewClaim("namespace2", "claim2", "uid2", "request2", "gaudi.intel.com", "node1", []string{"0000-00-02-0-0x1020"}),
			},
			expectedResponse: map[types.UID]kubeletplugin.PrepareResult{
				"uid2": {
					Devices: []kubeletplugin.Device{
						{Requests: []string{"request2"}, PoolName: "node1", DeviceName: "0000-00-02-0-0x1020", CDIDeviceIDs: []string{"intel.com/gaudi=0000-00-02-0-0x1020", "intel.com/gaudi=uid2"}},
					},
				},
			},
			preparedClaims: helpers.ClaimPreparations{
				"uid2": {
					Devices: []kubeletplugin.Device{
						{Requests: []string{"request2"}, PoolName: "node1", DeviceName: "0000-00-02-0-0x1020", CDIDeviceIDs: []string{"intel.com/gaudi=0000-00-02-0-0x1020", "intel.com/gaudi=uid2"}},
					},
				},
			},
			expectedPreparedClaims: helpers.ClaimPreparations{
				"uid2": {
					Devices: []kubeletplugin.Device{
						{Requests: []string{"request2"}, PoolName: "node1", DeviceName: "0000-00-02-0-0x1020", CDIDeviceIDs: []string{"intel.com/gaudi=0000-00-02-0-0x1020", "intel.com/gaudi=uid2"}},
					},
				},
			},
		},
		{
			name: "single unavailable device",
			request: []*resourcev1.ResourceClaim{
				testhelpers.NewClaim("namespace3", "claim3", "uid3", "request3", "gaudi.intel.com", "node1", []string{"0000-00-05-0-0x1020"}),
			},
			expectedResponse: map[types.UID]kubeletplugin.PrepareResult{
				"uid3": {Err: fmt.Errorf("could not find allocatable device 0000-00-05-0-0x1020 (pool node1)")},
			},
		},
		{
			name:              "no devices detected",
			noDetectedDevices: true,
			request: []*resourcev1.ResourceClaim{
				testhelpers.NewClaim("default", "claim5", "uid5", "request5", "gaudi.intel.com", "node1", []string{"0000-00-02-0-0x1020"}),
			},
			expectedResponse: map[types.UID]kubeletplugin.PrepareResult{
				"uid5": {Err: fmt.Errorf("could not find allocatable device 0000-00-02-0-0x1020 (pool node1)")},
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

		response, err := driver.PrepareResourceClaims(context.Background(), testcase.request)
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

func TestGaudiUnprepareResourceClaims(t *testing.T) {
	type testCase struct {
		name                   string
		request                []kubeletplugin.NamespacedObject
		expectedResponse       map[types.UID]error
		preparedClaims         helpers.ClaimPreparations
		expectedPreparedClaims helpers.ClaimPreparations
	}

	testcases := []testCase{
		{
			name:                   "blank request",
			request:                []kubeletplugin.NamespacedObject{},
			expectedResponse:       map[types.UID]error{},
			preparedClaims:         helpers.ClaimPreparations{},
			expectedPreparedClaims: helpers.ClaimPreparations{},
		},
		{
			name:             "single claim",
			request:          []kubeletplugin.NamespacedObject{{UID: "uid1"}},
			expectedResponse: map[types.UID]error{},
			preparedClaims: helpers.ClaimPreparations{
				"uid1": {Devices: []kubeletplugin.Device{{Requests: []string{"request1"}, PoolName: "node1", DeviceName: "0000-00-02-0-0x1020", CDIDeviceIDs: []string{"intel.com/gaudi=0000-00-02-0-0x1020", "intel.com/gaudi=uid1"}}}},
			},
			expectedPreparedClaims: helpers.ClaimPreparations{},
		},
		{
			name:             "subset of claims",
			request:          []kubeletplugin.NamespacedObject{{UID: "uid2"}},
			expectedResponse: map[types.UID]error{},
			preparedClaims: helpers.ClaimPreparations{
				"uid1": {Devices: []kubeletplugin.Device{{Requests: []string{"request1"}, PoolName: "node1", DeviceName: "0000-af-00-0-0x1020", CDIDeviceIDs: []string{"intel.com/gaudi=0000-af-00-0-0x1020", "intel.com/gaudi=uid1"}}}},
				"uid2": {Devices: []kubeletplugin.Device{{Requests: []string{"request2"}, PoolName: "node1", DeviceName: "0000-b3-00-0-0x1020", CDIDeviceIDs: []string{"intel.com/gaudi=0000-b3-00-0-0x1020", "intel.com/gaudi=uid2"}}}},
			},
			expectedPreparedClaims: helpers.ClaimPreparations{
				"uid1": {Devices: []kubeletplugin.Device{{Requests: []string{"request1"}, PoolName: "node1", DeviceName: "0000-af-00-0-0x1020", CDIDeviceIDs: []string{"intel.com/gaudi=0000-af-00-0-0x1020", "intel.com/gaudi=uid1"}}}},
			},
		},
		{
			name:             "non-existent claim success",
			request:          []kubeletplugin.NamespacedObject{{UID: "uid1"}},
			expectedResponse: map[types.UID]error{},
			preparedClaims: helpers.ClaimPreparations{
				"uid2": {Devices: []kubeletplugin.Device{{Requests: []string{"request2"}, PoolName: "node1", DeviceName: "0000-b3-00-0-0x1020", CDIDeviceIDs: []string{"intel.com/gaudi=0000-b3-00-0-0x1020", "intel.com/gaudi=uid2"}}}},
			},
			expectedPreparedClaims: helpers.ClaimPreparations{
				"uid2": {Devices: []kubeletplugin.Device{{Requests: []string{"request2"}, PoolName: "node1", DeviceName: "0000-b3-00-0-0x1020", CDIDeviceIDs: []string{"intel.com/gaudi=0000-b3-00-0-0x1020", "intel.com/gaudi=uid2"}}}},
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

		response, err := driver.UnprepareResourceClaims(context.Background(), testcase.request)
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

	err = driver.Shutdown(context.Background())
	if err != nil {
		t.Errorf("Shutdown() error = %v, wantErr %v", err, nil)
	}
}
