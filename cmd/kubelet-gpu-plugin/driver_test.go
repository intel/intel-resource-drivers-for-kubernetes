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
	resourceapi "k8s.io/api/resource/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/fakesysfs"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
	helpers "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/helpers"
	testhelpers "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/plugintesthelpers"
)

func TestFakeSysfs(t *testing.T) {
	testDirs, err := testhelpers.NewTestDirs(device.DriverName)
	if err != nil {
		t.Errorf("could not create fake system dirs: %v", err)
		return
	}

	if err := fakesysfs.FakeSysFsGpuContents(
		testDirs.SysfsRoot,
		testDirs.DevfsRoot,
		device.DevicesInfo{
			"0000-00-02-0-0x56c0": {Model: "0x56c0", MemoryMiB: 8192, DeviceType: "gpu", CardIdx: 0, RenderdIdx: 128, UID: "0000-00-02-0-0x56c0", MaxVFs: 16, Driver: "i915"},
			"0000-00-03-0-0x56c0": {Model: "0x56c0", MemoryMiB: 8192, DeviceType: "gpu", CardIdx: 1, RenderdIdx: 128, UID: "0000-00-03-0-0x56c0", MaxVFs: 16, Driver: "xe"},
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

func getFakeDriver(testDirs testhelpers.TestDirsType) (*driver, error) {
	nodeName := "node1"
	config := &helpers.Config{
		CommonFlags: &helpers.Flags{
			NodeName:                  nodeName,
			CdiRoot:                   testDirs.CdiRoot,
			KubeletPluginDir:          testDirs.KubeletPluginDir,
			KubeletPluginsRegistryDir: testDirs.KubeletPluginRegistryDir,
		},
		Coreclient: kubefake.NewSimpleClientset(),
	}

	if err := os.MkdirAll(config.CommonFlags.KubeletPluginDir, 0755); err != nil {
		return nil, fmt.Errorf("failed creating fake driver plugin dir: %v", err)
	}
	if err := os.MkdirAll(config.CommonFlags.KubeletPluginsRegistryDir, 0755); err != nil {
		return nil, fmt.Errorf("failed creating fake driver plugin dir: %v", err)
	}

	os.Setenv("SYSFS_ROOT", testDirs.SysfsRoot)

	// kubelet-plugin will access node object, it needs to exist.
	newNode := &core.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}
	if _, err := config.Coreclient.CoreV1().Nodes().Create(context.TODO(), newNode, metav1.CreateOptions{}); err != nil {
		return nil, fmt.Errorf("failed creating fake node object: %v", err)
	}

	helperdriver, err := newDriver(context.TODO(), config)
	if err != nil {
		return nil, fmt.Errorf("failed creating driver object: %v", err)
	}

	driver, ok := helperdriver.(*driver)
	if !ok {
		return nil, fmt.Errorf("type assertion failed: expected driver, got %T", driver)
	}
	return driver, err
}

func TestPrepareResourceClaims(t *testing.T) {
	type testCase struct {
		name                   string
		request                []*resourceapi.ResourceClaim
		expectedResponse       map[types.UID]kubeletplugin.PrepareResult
		initialPreparedClaims  helpers.ClaimPreparations
		expectedPreparedClaims helpers.ClaimPreparations
	}

	testcases := []testCase{
		{
			name:                  "blank request",
			request:               []*resourceapi.ResourceClaim{},
			expectedResponse:      map[types.UID]kubeletplugin.PrepareResult{},
			initialPreparedClaims: helpers.ClaimPreparations{},
		},
		{
			name: "single GPU",
			request: []*resourceapi.ResourceClaim{
				testhelpers.NewClaim("namespace1", "claim1", "uid1", "request1", "gpu.intel.com", "node1", []string{"0000-00-02-0-0x56c0"}),
			},
			expectedResponse: map[types.UID]kubeletplugin.PrepareResult{
				"uid1": {
					Devices: []kubeletplugin.Device{
						{Requests: []string{"request1"}, PoolName: "node1", DeviceName: "0000-00-02-0-0x56c0", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-02-0-0x56c0"}},
					},
				},
			},
			initialPreparedClaims: helpers.ClaimPreparations{},
			expectedPreparedClaims: helpers.ClaimPreparations{
				"uid1": {
					Devices: []kubeletplugin.Device{
						{
							Requests:     []string{"request1"},
							PoolName:     "node1",
							DeviceName:   "0000-00-02-0-0x56c0",
							CDIDeviceIDs: []string{"intel.com/gpu=0000-00-02-0-0x56c0"},
						},
					},
				},
			},
		},
		{
			name: "single existing VF",
			request: []*resourceapi.ResourceClaim{
				testhelpers.NewClaim("namespace2", "claim2", "uid2", "request2", "gpu.intel.com", "node1", []string{"0000-00-03-1-0x56c0"}),
			},
			expectedResponse: map[types.UID]kubeletplugin.PrepareResult{
				"uid2": {
					Devices: []kubeletplugin.Device{
						{Requests: []string{"request2"}, PoolName: "node1", DeviceName: "0000-00-03-1-0x56c0", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-1-0x56c0"}},
					},
				},
			},
			initialPreparedClaims: helpers.ClaimPreparations{},
			expectedPreparedClaims: helpers.ClaimPreparations{
				"uid2": {
					Devices: []kubeletplugin.Device{
						{
							Requests:     []string{"request2"},
							PoolName:     "node1",
							DeviceName:   "0000-00-03-1-0x56c0",
							CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-1-0x56c0"},
						},
					},
				},
			},
		},
		{
			name: "monitoring claim",
			request: []*resourceapi.ResourceClaim{
				testhelpers.NewMonitoringClaim(
					"namespace3", "monitor", "uid3", "monitor", "gpu.intel.com", "node1", []string{"0000-00-02-0-0x56c0", "0000-00-03-0-0x56c0", "0000-00-03-1-0x56c0", "0000-00-04-0-0x0000"}),
			},
			expectedResponse: map[types.UID]kubeletplugin.PrepareResult{
				"uid3": {
					Devices: []kubeletplugin.Device{
						{Requests: []string{"monitor"}, PoolName: "node1", DeviceName: "0000-00-02-0-0x56c0", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-02-0-0x56c0"}},
						{Requests: []string{"monitor"}, PoolName: "node1", DeviceName: "0000-00-03-0-0x56c0", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-0-0x56c0"}},
						{Requests: []string{"monitor"}, PoolName: "node1", DeviceName: "0000-00-03-1-0x56c0", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-1-0x56c0"}},
						{Requests: []string{"monitor"}, PoolName: "node1", DeviceName: "0000-00-04-0-0x0000", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-04-0-0x0000"}},
					},
				},
			},
			initialPreparedClaims: helpers.ClaimPreparations{},
			expectedPreparedClaims: helpers.ClaimPreparations{
				"uid3": {
					Devices: []kubeletplugin.Device{
						{Requests: []string{"monitor"}, PoolName: "node1", DeviceName: "0000-00-02-0-0x56c0", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-02-0-0x56c0"}},
						{Requests: []string{"monitor"}, PoolName: "node1", DeviceName: "0000-00-03-0-0x56c0", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-0-0x56c0"}},
						{Requests: []string{"monitor"}, PoolName: "node1", DeviceName: "0000-00-03-1-0x56c0", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-1-0x56c0"}},
						{Requests: []string{"monitor"}, PoolName: "node1", DeviceName: "0000-00-04-0-0x0000", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-04-0-0x0000"}},
					},
				},
			},
		},
		{
			name: "single GPU, already prepared claim",
			request: []*resourceapi.ResourceClaim{
				testhelpers.NewMonitoringClaim("namespace4", "claim4", "uid4", "request4", "gpu.intel.com", "node1", []string{"0000-00-03-1-0x56c0"}),
			},
			expectedResponse: map[types.UID]kubeletplugin.PrepareResult{
				"uid4": {
					Devices: []kubeletplugin.Device{
						{
							Requests: []string{"request4"}, PoolName: "node1", DeviceName: "0000-00-03-1-0x56c0", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-1-0x56c0"},
						},
					},
				},
			},
			initialPreparedClaims: helpers.ClaimPreparations{
				"uid4": {
					Devices: []kubeletplugin.Device{
						{
							Requests:     []string{"request4"},
							PoolName:     "node1",
							DeviceName:   "0000-00-03-1-0x56c0",
							CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-1-0x56c0"},
						},
					},
				},
			},
			expectedPreparedClaims: helpers.ClaimPreparations{
				"uid4": {
					Devices: []kubeletplugin.Device{
						{
							Requests:     []string{"request4"},
							PoolName:     "node1",
							DeviceName:   "0000-00-03-1-0x56c0",
							CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-1-0x56c0"},
						},
					},
				},
			},
		},
		{
			name: "single Xe GPU",
			claims: []*resourcev1.ResourceClaim{
				testhelpers.NewClaim("namespacexe", "claimxe", "uidxe", "requestxe", "gpu.intel.com", "node1", []string{"0000-00-05-0-0x56c0"}),
			},
			request: &drav1.NodePrepareResourcesRequest{
				Claims: []*drav1.Claim{
					{UID: "uidxe", Name: "claimxe", Namespace: "namespacexe"},
				},
			},
			expectedResponse: &drav1.NodePrepareResourcesResponse{
				Claims: map[string]*drav1.NodePrepareResourceResponse{
					"uidxe": {
						Devices: []*drav1.Device{
							{RequestNames: []string{"requestxe"}, PoolName: "node1", DeviceName: "0000-00-05-0-0x56c0", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-05-0-0x56c0"}},
						},
					},
				},
			},
			preparedClaims: helpers.ClaimPreparations{},
			expectedPreparedClaims: helpers.ClaimPreparations{
				"uidxe": {
					{
						RequestNames: []string{"requestxe"},
						PoolName:     "node1",
						DeviceName:   "0000-00-05-0-0x56c0",
						CDIDeviceIDs: []string{"intel.com/gpu=0000-00-05-0-0x56c0"},
					},
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

		if err := fakesysfs.FakeSysFsGpuContents(
			testDirs.SysfsRoot,
			testDirs.DevfsRoot,
			device.DevicesInfo{
				"0000-00-02-0-0x56c0": {Model: "0x56c0", MemoryMiB: 16256, DeviceType: "gpu", CardIdx: 0, RenderdIdx: 128, UID: "0000-00-02-0-0x56c0", MaxVFs: 16, Driver: "i915"},
				"0000-00-03-0-0x56c0": {Model: "0x56c0", MemoryMiB: 16256, DeviceType: "gpu", CardIdx: 1, RenderdIdx: 129, UID: "0000-00-03-0-0x56c0", MaxVFs: 16, Driver: "i915"},
				"0000-00-03-1-0x56c0": {Model: "0x56c0", MemoryMiB: 8064, DeviceType: "vf", CardIdx: 2, RenderdIdx: 130, UID: "0000-00-03-1-0x56c0", VFIndex: 0, VFProfile: "flex170_m2", ParentUID: "0000-00-03-0-0x56c0", Driver: "i915"},
				// dummy, no SR-IOV tiles
				"0000-00-04-0-0x0000": {Model: "0x0000", MemoryMiB: 14248, DeviceType: "gpu", CardIdx: 3, RenderdIdx: 131, UID: "0000-00-04-0-0x0000", MaxVFs: 16, Driver: "i915"},
				"0000-00-05-0-0x56c0": {Model: "0x56c0", MemoryMiB: 16256, DeviceType: "gpu", CardIdx: 4, RenderdIdx: 128, UID: "0000-00-05-0-0x56c0", MaxVFs: 16, Driver: "xe"},
			},
			false,
		); err != nil {
			t.Errorf("setup error: could not create fake sysfs: %v", err)
			return
		}

		preparedClaimFilePath := path.Join(testDirs.KubeletPluginDir, device.PreparedClaimsFileName)
		if err := helpers.WritePreparedClaimsToFile(preparedClaimFilePath, testcase.initialPreparedClaims); err != nil {
			t.Errorf("%v: error %v, writing prepared claims to file", testcase.name, err)
		}

		driver, driverErr := getFakeDriver(testDirs)
		if driverErr != nil {
			t.Errorf("could not create kubelet-plugin: %v\n", driverErr)
			continue
		}

		response, err := driver.PrepareResourceClaims(context.TODO(), testcase.request)
		if err != nil {
			t.Errorf("%v: error %v, expected no error", testcase.name, err)
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

func TestNodeUnprepareResources(t *testing.T) {
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
			name:             "single GPU",
			request:          []kubeletplugin.NamespacedObject{{UID: "uid1"}},
			expectedResponse: map[types.UID]error{"uid1": nil},
			preparedClaims: helpers.ClaimPreparations{
				"uid1": {Devices: []kubeletplugin.Device{{Requests: []string{"request1"}, PoolName: "node1", DeviceName: "0000-b3-00-0-0x0bda", CDIDeviceIDs: []string{"intel.com/gpu=0000-b3-00-0-0x0bda"}}}},
			},
			expectedPreparedClaims: helpers.ClaimPreparations{},
		},
		{
			name:             "single VF without cleanup",
			request:          []kubeletplugin.NamespacedObject{{UID: "uid2"}},
			expectedResponse: map[types.UID]error{"uid2": nil},
			preparedClaims: helpers.ClaimPreparations{
				"uid2": {Devices: []kubeletplugin.Device{{Requests: []string{"request2"}, PoolName: "node1", DeviceName: "0000-af-00-1-0x0bda", CDIDeviceIDs: []string{"intel.com/gpu=0000-af-00-1-0x0bda"}}}},
				"uid3": {Devices: []kubeletplugin.Device{{Requests: []string{"request3"}, PoolName: "node1", DeviceName: "0000-af-00-2-0x0bda", CDIDeviceIDs: []string{"intel.com/gpu=0000-af-00-2-0x0bda"}}}},
			},
			expectedPreparedClaims: helpers.ClaimPreparations{
				"uid3": {Devices: []kubeletplugin.Device{{Requests: []string{"request3"}, PoolName: "node1", DeviceName: "0000-af-00-2-0x0bda", CDIDeviceIDs: []string{"intel.com/gpu=0000-af-00-2-0x0bda"}}}},
			},
		},
	}

	for _, testcase := range testcases {
		t.Log(testcase.name)

		testDirs, err := testhelpers.NewTestDirs(device.DriverName)
		defer testhelpers.CleanupTest(t, "TestNodeUnprepareResources", testDirs.TestRoot)
		if err != nil {
			t.Errorf("%v: setup error: %v", testcase.name, err)
			return
		}

		if err := fakesysfs.FakeSysFsGpuContents(
			testDirs.SysfsRoot,
			testDirs.DevfsRoot,
			device.DevicesInfo{
				"0000-b3-00-0-0x0bda": {Model: "0x0bda", MemoryMiB: 49136, DeviceType: "gpu", CardIdx: 0, UID: "0000-b3-00-0-0x0bda", MaxVFs: 63, Driver: "i915"},
				"0000-af-00-0-0x0bda": {Model: "0x0bda", MemoryMiB: 49136, DeviceType: "gpu", CardIdx: 1, UID: "0000-af-00-0-0x0bda", MaxVFs: 63, Driver: "i915"},
				"0000-af-00-1-0x0bda": {Model: "0x0bda", MemoryMiB: 22528, Millicores: 500, DeviceType: "vf", CardIdx: 2, UID: "0000-af-00-1-0x0bda", VFIndex: 0, VFProfile: "max_47g_c2", ParentUID: "0000-af-00-0-0x0bda", Driver: "i915"},
				"0000-af-00-2-0x0bda": {Model: "0x0bda", MemoryMiB: 22528, Millicores: 500, DeviceType: "vf", CardIdx: 3, UID: "0000-af-00-2-0x0bda", VFIndex: 1, VFProfile: "max_47g_c2", ParentUID: "0000-af-00-0-0x0bda", Driver: "i915"},
			},
			false,
		); err != nil {
			t.Errorf("%v: setup error: could not create fake sysfs: %v", testcase.name, err)
			return
		}

		preparedClaimsFilePath := path.Join(testDirs.KubeletPluginDir, device.PreparedClaimsFileName)
		if err := helpers.WritePreparedClaimsToFile(preparedClaimsFilePath, testcase.preparedClaims); err != nil {
			t.Errorf("%v: error %v, writing prepared claims to file", testcase.name, err)
			continue
		}

		driver, driverErr := getFakeDriver(testDirs)
		if driverErr != nil {
			t.Errorf("could not create kubelet-plugin: %v\n", driverErr)
			continue
		}

		response, err := driver.UnprepareResourceClaims(context.TODO(), testcase.request)
		if err != nil {
			t.Errorf("%v: error %v, expected no error", testcase.name, err)
			continue
		}

		preparedClaims, err := helpers.ReadPreparedClaimsFromFile(preparedClaimsFilePath)
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

func TestShutdown(t *testing.T) {
	testDirs, err := testhelpers.NewTestDirs(device.DriverName)
	if err != nil {
		t.Fatalf("could not create fake system dirs: %v", err)
	}

	driver, err := getFakeDriver(testDirs)
	if err != nil {
		t.Fatalf("could not create driver: %v", err)
	}

	err = driver.Shutdown(context.TODO())
	if err != nil {
		t.Errorf("Shutdown() error = %v, wantErr %v", err, nil)
	}
}
