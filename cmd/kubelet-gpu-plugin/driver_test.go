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
	"errors"
	"fmt"
	"os"
	"path"
	"reflect"
	"testing"
	"time"

	core "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"

	"github.com/containers/nri-plugins/pkg/udev"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/fakesysfs"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
	helpers "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/helpers"
	testhelpers "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/plugintesthelpers"
)

func TestGPUFakeSysfs(t *testing.T) {
	testDirs, err := testhelpers.NewTestDirs(device.DriverName)
	defer testhelpers.CleanupTest(t, "TestGPUFakeSysfs", testDirs.TestRoot)
	if err != nil {
		t.Errorf("could not create fake system dirs: %v", err)
		return
	}

	if err := fakesysfs.FakeSysFsGpuContents(
		testDirs.SysfsRoot,
		testDirs.DevfsRoot,
		device.DevicesInfo{
			"0000-00-02-0-0x56c0": {Model: "0x56c0", MemoryMiB: 8192, DeviceType: "gpu", CardIdx: 0, MEIName: "mei0", RenderdIdx: 128, UID: "0000-00-02-0-0x56c0", MaxVFs: 16, Driver: "i915"},
			"0000-00-03-0-0x56c0": {Model: "0x56c0", MemoryMiB: 8192, DeviceType: "gpu", CardIdx: 1, MEIName: "mei1", RenderdIdx: 128, UID: "0000-00-03-0-0x56c0", MaxVFs: 16, Driver: "xe"},
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
		Coreclient:  kubefake.NewClientset(),
		DriverFlags: &GPUFlags{}, // ensure correct type to avoid nil type assertion failure
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
		initialPreparedClaims  ClaimPreparations
		expectedPreparedClaims ClaimPreparations
	}

	testcases := []testCase{
		{
			name:                  "blank request",
			request:               []*resourceapi.ResourceClaim{},
			expectedResponse:      map[types.UID]kubeletplugin.PrepareResult{},
			initialPreparedClaims: ClaimPreparations{},
		},
		{
			name: "single GPU",
			request: []*resourceapi.ResourceClaim{
				testhelpers.NewClaim("namespace1", "claim1", "uid1", "request1", "gpu.intel.com", "node1", []string{"0000-00-02-0-0x56c0"}, false),
			},
			expectedResponse: map[types.UID]kubeletplugin.PrepareResult{
				"uid1": {
					Devices: []kubeletplugin.Device{
						{Requests: []string{"request1"}, PoolName: "node1", DeviceName: "0000-00-02-0-0x56c0", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-02-0-0x56c0"}},
					},
				},
			},
			initialPreparedClaims: ClaimPreparations{},
			expectedPreparedClaims: ClaimPreparations{
				"uid1": {
					PreparedDevices: []PreparedDevice{
						{
							KubeletpluginDevice: kubeletplugin.Device{Requests: []string{"request1"}, PoolName: "node1", DeviceName: "0000-00-02-0-0x56c0", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-02-0-0x56c0"}},
						},
					},
				},
			},
		},
		{
			name: "single existing VF",
			request: []*resourceapi.ResourceClaim{
				testhelpers.NewClaim("namespace2", "claim2", "uid2", "request2", "gpu.intel.com", "node1", []string{"0000-00-03-1-0x56c0"}, false),
			},
			expectedResponse: map[types.UID]kubeletplugin.PrepareResult{
				"uid2": {
					Devices: []kubeletplugin.Device{
						{Requests: []string{"request2"}, PoolName: "node1", DeviceName: "0000-00-03-1-0x56c0", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-1-0x56c0"}},
					},
				},
			},
			initialPreparedClaims: ClaimPreparations{},
			expectedPreparedClaims: ClaimPreparations{
				"uid2": {
					PreparedDevices: []PreparedDevice{
						{
							KubeletpluginDevice: kubeletplugin.Device{Requests: []string{"request2"}, PoolName: "node1", DeviceName: "0000-00-03-1-0x56c0", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-1-0x56c0"}},
						},
					},
				},
			},
		},
		{
			name: "single GPU without admin access prepare failure because of double allocation",
			request: []*resourceapi.ResourceClaim{
				testhelpers.NewClaim("namespace1", "claim1", "uid0", "request1", "gpu.intel.com", "node1", []string{"0000-00-02-0-0x56c0"}, false),
			},
			expectedResponse: map[types.UID]kubeletplugin.PrepareResult{
				"uid0": {
					Err: errors.New("error preparing devices for claim uid0: device 0000-00-02-0-0x56c0 (pool node1) is already allocated to another claim and cannot be prepared without adminAccess flag"),
				},
			},
			initialPreparedClaims: ClaimPreparations{
				"uid1": {
					PreparedDevices: []PreparedDevice{
						{
							KubeletpluginDevice: kubeletplugin.Device{
								Requests:     []string{"request1"},
								PoolName:     "node1",
								DeviceName:   "0000-00-02-0-0x56c0",
								CDIDeviceIDs: []string{"intel.com/gpu=0000-00-02-0-0x56c0"},
							},
						},
					},
				},
			},
			expectedPreparedClaims: ClaimPreparations{
				"uid1": {
					PreparedDevices: []PreparedDevice{
						{
							KubeletpluginDevice: kubeletplugin.Device{
								Requests:     []string{"request1"},
								PoolName:     "node1",
								DeviceName:   "0000-00-02-0-0x56c0",
								CDIDeviceIDs: []string{"intel.com/gpu=0000-00-02-0-0x56c0"},
							},
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
			initialPreparedClaims: ClaimPreparations{},
			expectedPreparedClaims: ClaimPreparations{
				"uid3": {
					PreparedDevices: []PreparedDevice{
						{
							KubeletpluginDevice: kubeletplugin.Device{Requests: []string{"monitor"}, PoolName: "node1", DeviceName: "0000-00-02-0-0x56c0", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-02-0-0x56c0"}},
							AdminAccess:         true,
						},
						{
							KubeletpluginDevice: kubeletplugin.Device{Requests: []string{"monitor"}, PoolName: "node1", DeviceName: "0000-00-03-0-0x56c0", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-0-0x56c0"}},
							AdminAccess:         true,
						},
						{
							KubeletpluginDevice: kubeletplugin.Device{Requests: []string{"monitor"}, PoolName: "node1", DeviceName: "0000-00-03-1-0x56c0", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-1-0x56c0"}},
							AdminAccess:         true,
						},
						{
							KubeletpluginDevice: kubeletplugin.Device{Requests: []string{"monitor"}, PoolName: "node1", DeviceName: "0000-00-04-0-0x0000", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-04-0-0x0000"}},
							AdminAccess:         true,
						},
					},
				},
			},
		},
		{
			name: "monitoring claim, one device prepared already",
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
			initialPreparedClaims: ClaimPreparations{
				"uid4": {
					PreparedDevices: []PreparedDevice{
						{
							KubeletpluginDevice: kubeletplugin.Device{
								Requests:     []string{"request4"},
								PoolName:     "node1",
								DeviceName:   "0000-00-03-1-0x56c0",
								CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-1-0x56c0"},
							},
						},
					},
				},
			},
			expectedPreparedClaims: ClaimPreparations{
				"uid3": {
					PreparedDevices: []PreparedDevice{
						{
							KubeletpluginDevice: kubeletplugin.Device{Requests: []string{"monitor"}, PoolName: "node1", DeviceName: "0000-00-02-0-0x56c0", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-02-0-0x56c0"}},
							AdminAccess:         true,
						},
						{
							KubeletpluginDevice: kubeletplugin.Device{Requests: []string{"monitor"}, PoolName: "node1", DeviceName: "0000-00-03-0-0x56c0", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-0-0x56c0"}},
							AdminAccess:         true,
						},
						{
							KubeletpluginDevice: kubeletplugin.Device{Requests: []string{"monitor"}, PoolName: "node1", DeviceName: "0000-00-03-1-0x56c0", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-1-0x56c0"}},
							AdminAccess:         true,
						},
						{
							KubeletpluginDevice: kubeletplugin.Device{Requests: []string{"monitor"}, PoolName: "node1", DeviceName: "0000-00-04-0-0x0000", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-04-0-0x0000"}},
							AdminAccess:         true,
						},
					},
				},
				"uid4": {
					PreparedDevices: []PreparedDevice{
						{
							KubeletpluginDevice: kubeletplugin.Device{Requests: []string{"request4"}, PoolName: "node1", DeviceName: "0000-00-03-1-0x56c0", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-1-0x56c0"}},
						},
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
						{Requests: []string{"request4"}, PoolName: "node1", DeviceName: "0000-00-03-1-0x56c0", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-1-0x56c0"}},
					},
				},
			},
			initialPreparedClaims: ClaimPreparations{
				"uid4": {
					PreparedDevices: []PreparedDevice{
						{
							KubeletpluginDevice: kubeletplugin.Device{Requests: []string{"request4"}, PoolName: "node1", DeviceName: "0000-00-03-1-0x56c0", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-1-0x56c0"}},
						},
					},
				},
			},
			expectedPreparedClaims: ClaimPreparations{
				"uid4": {
					PreparedDevices: []PreparedDevice{
						{
							KubeletpluginDevice: kubeletplugin.Device{Requests: []string{"request4"}, PoolName: "node1", DeviceName: "0000-00-03-1-0x56c0", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-1-0x56c0"}},
						},
					},
				},
			},
		},
		{
			name: "single Xe GPU",
			request: []*resourceapi.ResourceClaim{
				testhelpers.NewClaim("namespacexe", "claimxe", "uidxe", "requestxe", "gpu.intel.com", "node1", []string{"0000-00-05-0-0x56c0"}, false),
			},
			expectedResponse: map[types.UID]kubeletplugin.PrepareResult{
				"uidxe": {
					Devices: []kubeletplugin.Device{
						{Requests: []string{"requestxe"}, PoolName: "node1", DeviceName: "0000-00-05-0-0x56c0", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-05-0-0x56c0"}},
					},
				},
			},
			initialPreparedClaims: ClaimPreparations{},
			expectedPreparedClaims: ClaimPreparations{
				"uidxe": {
					PreparedDevices: []PreparedDevice{
						{
							KubeletpluginDevice: kubeletplugin.Device{Requests: []string{"requestxe"}, PoolName: "node1", DeviceName: "0000-00-05-0-0x56c0", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-05-0-0x56c0"}},
						},
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
				"0000-00-02-0-0x56c0": {Model: "0x56c0", MemoryMiB: 16256, DeviceType: "gpu", CardIdx: 0, MEIName: "mei0", RenderdIdx: 128, UID: "0000-00-02-0-0x56c0", MaxVFs: 16, Driver: "i915"},
				"0000-00-03-0-0x56c0": {Model: "0x56c0", MemoryMiB: 16256, DeviceType: "gpu", CardIdx: 1, MEIName: "mei1", RenderdIdx: 129, UID: "0000-00-03-0-0x56c0", MaxVFs: 16, Driver: "i915"},
				"0000-00-03-1-0x56c0": {Model: "0x56c0", MemoryMiB: 8064, DeviceType: "vf", CardIdx: 2, RenderdIdx: 130, UID: "0000-00-03-1-0x56c0", VFIndex: 0, VFProfile: "flex170_m2", ParentUID: "0000-00-03-0-0x56c0", Driver: "i915"},
				// dummy, no SR-IOV tiles
				"0000-00-04-0-0x0000": {Model: "0x0000", MemoryMiB: 14248, DeviceType: "gpu", CardIdx: 3, MEIName: "mei2", RenderdIdx: 131, UID: "0000-00-04-0-0x0000", MaxVFs: 16, Driver: "i915"},
				"0000-00-05-0-0x56c0": {Model: "0x56c0", MemoryMiB: 16256, DeviceType: "gpu", CardIdx: 4, MEIName: "mei3", RenderdIdx: 128, UID: "0000-00-05-0-0x56c0", MaxVFs: 16, Driver: "xe"},
			},
			false,
		); err != nil {
			t.Errorf("setup error: could not create fake sysfs: %v", err)
			return
		}

		preparedClaimFilePath := path.Join(testDirs.KubeletPluginDir, device.PreparedClaimsFileName)
		if err := WritePreparedClaimsToFile(preparedClaimFilePath, testcase.initialPreparedClaims); err != nil {
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

		if !testhelpers.DeepEqualPrepareResults(testcase.expectedResponse, response) {
			t.Errorf(
				"%v: unexpected response: %v, expected response: %v",
				testcase.name, response, testcase.expectedResponse)
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
			t.Errorf(
				"%v: unexpected PreparedClaims:%v, expected PreparedClaims: %v",
				testcase.name, preparedClaims, expectedPreparedClaims,
			)
		}

		if err := driver.Shutdown(context.TODO()); err != nil {
			t.Errorf("Shutdown() error = %v, wantErr %v", err, nil)
		}
	}
}

func TestNodeUnprepareResources(t *testing.T) {
	type testCase struct {
		name                   string
		request                []kubeletplugin.NamespacedObject
		expectedResponse       map[types.UID]error
		preparedClaims         ClaimPreparations
		expectedPreparedClaims ClaimPreparations
	}

	testcases := []testCase{
		{
			name:                   "blank request",
			request:                []kubeletplugin.NamespacedObject{},
			expectedResponse:       map[types.UID]error{},
			preparedClaims:         ClaimPreparations{},
			expectedPreparedClaims: ClaimPreparations{},
		},
		{
			name:             "single GPU",
			request:          []kubeletplugin.NamespacedObject{{UID: "uid1"}},
			expectedResponse: map[types.UID]error{"uid1": nil},
			preparedClaims: ClaimPreparations{
				"uid1": {
					PreparedDevices: []PreparedDevice{
						{
							KubeletpluginDevice: kubeletplugin.Device{
								Requests: []string{"request1"}, PoolName: "node1", DeviceName: "0000-b3-00-0-0x0bda", CDIDeviceIDs: []string{"intel.com/gpu=0000-b3-00-0-0x0bda"},
							},
						},
					},
				},
			},
			expectedPreparedClaims: ClaimPreparations{},
		},
		{
			name:             "single VF without cleanup",
			request:          []kubeletplugin.NamespacedObject{{UID: "uid2"}},
			expectedResponse: map[types.UID]error{"uid2": nil},
			preparedClaims: ClaimPreparations{
				"uid2": {
					PreparedDevices: []PreparedDevice{
						{
							KubeletpluginDevice: kubeletplugin.Device{
								Requests: []string{"request2"}, PoolName: "node1", DeviceName: "0000-af-00-1-0x0bda", CDIDeviceIDs: []string{"intel.com/gpu=0000-af-00-1-0x0bda"},
							},
						},
					},
				},
				"uid3": {
					PreparedDevices: []PreparedDevice{
						{
							KubeletpluginDevice: kubeletplugin.Device{Requests: []string{"request3"}, PoolName: "node1", DeviceName: "0000-af-00-2-0x0bda", CDIDeviceIDs: []string{"intel.com/gpu=0000-af-00-2-0x0bda"}},
						},
					},
				},
			},
			expectedPreparedClaims: ClaimPreparations{
				"uid3": {
					PreparedDevices: []PreparedDevice{
						{
							KubeletpluginDevice: kubeletplugin.Device{
								Requests: []string{"request3"}, PoolName: "node1", DeviceName: "0000-af-00-2-0x0bda", CDIDeviceIDs: []string{"intel.com/gpu=0000-af-00-2-0x0bda"},
							},
						},
					},
				},
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
				"0000-b3-00-0-0x0bda": {Model: "0x0bda", MemoryMiB: 49136, DeviceType: "gpu", CardIdx: 0, MEIName: "mei0", UID: "0000-b3-00-0-0x0bda", MaxVFs: 63, Driver: "i915"},
				"0000-af-00-0-0x0bda": {Model: "0x0bda", MemoryMiB: 49136, DeviceType: "gpu", CardIdx: 1, MEIName: "mei1", UID: "0000-af-00-0-0x0bda", MaxVFs: 63, Driver: "i915"},
				"0000-af-00-1-0x0bda": {Model: "0x0bda", MemoryMiB: 22528, Millicores: 500, DeviceType: "vf", CardIdx: 2, UID: "0000-af-00-1-0x0bda", VFIndex: 0, VFProfile: "max_47g_c2", ParentUID: "0000-af-00-0-0x0bda", Driver: "i915"},
				"0000-af-00-2-0x0bda": {Model: "0x0bda", MemoryMiB: 22528, Millicores: 500, DeviceType: "vf", CardIdx: 3, UID: "0000-af-00-2-0x0bda", VFIndex: 1, VFProfile: "max_47g_c2", ParentUID: "0000-af-00-0-0x0bda", Driver: "i915"},
			},
			false,
		); err != nil {
			t.Errorf("%v: setup error: could not create fake sysfs: %v", testcase.name, err)
			return
		}

		preparedClaimsFilePath := path.Join(testDirs.KubeletPluginDir, device.PreparedClaimsFileName)
		if err := WritePreparedClaimsToFile(preparedClaimsFilePath, testcase.preparedClaims); err != nil {
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

		preparedClaims, err := readPreparedClaimsFromFile(preparedClaimsFilePath)
		if err != nil {
			t.Errorf("%v: error %v, expected no error", testcase.name, err)
			continue
		}

		if !testhelpers.DeepEqualErrorMap(response, testcase.expectedResponse) {
			t.Errorf("%v: unexpected response: %+v, expected response: %v", testcase.name, response, testcase.expectedResponse)
		}

		if !reflect.DeepEqual(testcase.expectedPreparedClaims, preparedClaims) {
			t.Errorf(
				"%v: unexpected PreparedClaims: %+v, expected PreparedClaims: %+v",
				testcase.name, preparedClaims, testcase.expectedPreparedClaims,
			)
		}

		if err := driver.Shutdown(context.TODO()); err != nil {
			t.Errorf("Shutdown() error = %v, wantErr %v", err, nil)
		}
	}
}

//nolint:cyclop // test code
func TestRefreshDeviceOnDriverEvent(t *testing.T) {
	testDirs, err := testhelpers.NewTestDirs(device.DriverName)
	defer testhelpers.CleanupTest(t, "TestRefreshDeviceOnDriverEvent", testDirs.TestRoot)
	if err != nil {
		t.Fatalf("setup error: %v", err)
	}

	const deviceUID = "0000-00-02-0-0x56c0"

	drv, err := getFakeDriver(testDirs)
	if err != nil {
		t.Fatalf("could not create fake driver: %v", err)
	}
	defer func() { _ = drv.Shutdown(context.TODO()) }()

	drv.state.Lock()
	drv.state.Allocatable = map[string]*device.DeviceInfo{
		deviceUID: {
			UID:        deviceUID,
			PCIAddress: "0000:00:02.0",
			Model:      "0x56c0",
			ModelName:  "Flex 170",
			FamilyName: "Data Center Flex",
			CardIdx:    0,
			MEIName:    "mei0",
			RenderdIdx: 128,
			MemoryMiB:  16256,
			DeviceType: "gpu",
			Driver:     "i915",
			Health:     device.HealthUnknown,
		},
	}
	//nolint:forcetypeassert
	allocatable := drv.state.Allocatable.(map[string]*device.DeviceInfo)
	drv.state.Unlock()

	type testCase struct {
		name                  string
		eventAction           string
		devpath               string
		discoveredDevices     map[string]*device.DeviceInfo
		expectedDeviceUID     string
		currentDriver         string
		expectedCurrentDriver string
	}

	testcases := []testCase{
		{
			name:                  "unbind event changes current driver unbound",
			eventAction:           "unbind",
			devpath:               "/devices/pci0000:00/0000:00:02.0/drm/card0",
			discoveredDevices:     map[string]*device.DeviceInfo{},
			expectedDeviceUID:     deviceUID,
			currentDriver:         "i915",
			expectedCurrentDriver: "",
		},
		{
			name:        "bind event changes current driver to i915",
			eventAction: "bind",
			devpath:     "/devices/pci0000:00/0000:00:02.0/drm/card0",
			discoveredDevices: map[string]*device.DeviceInfo{
				deviceUID: {
					UID:        deviceUID,
					PCIAddress: "0000:00:02.0",
					Model:      "0x56c0",
					ModelName:  "Flex 170",
					FamilyName: "Data Center Flex",
					CardIdx:    0,
					MEIName:    "mei1",
					RenderdIdx: 128,
					MemoryMiB:  16256,
					DeviceType: "gpu",
					Driver:     "i915",
					Health:     device.HealthUnknown,
				},
			},
			expectedDeviceUID:     deviceUID,
			currentDriver:         "vfio-pci",
			expectedCurrentDriver: "i915",
		},
		{
			name:                  "bind event changes current driver to vfio-pci",
			eventAction:           "bind",
			devpath:               "/devices/pci0000:00/0000:00:02.0/vfio-dev/vfio0",
			discoveredDevices:     map[string]*device.DeviceInfo{},
			expectedDeviceUID:     deviceUID,
			currentDriver:         "i915",
			expectedCurrentDriver: "vfio-pci",
		},
	}

	for _, testcase := range testcases {
		t.Log(testcase.name)

		if testcase.eventAction == "bind" {
			driverLink := path.Join(testDirs.SysfsRoot, "devices/pci0000:00/0000:00:02.0/driver")
			driverTarget := path.Join(testDirs.SysfsRoot, "bus/pci/drivers", testcase.expectedCurrentDriver)
			if err := os.MkdirAll(path.Dir(driverLink), 0755); err != nil {
				t.Fatalf("setup error: failed creating fake pci path: %v", err)
			}
			if err := os.MkdirAll(driverTarget, 0755); err != nil {
				t.Fatalf("setup error: failed creating fake driver path: %v", err)
			}
			if err := os.Remove(driverLink); err != nil && !os.IsNotExist(err) {
				t.Fatalf("setup error: failed removing existing driver symlink: %v", err)
			}
			if err := os.Symlink(driverTarget, driverLink); err != nil {
				t.Fatalf("setup error: failed creating driver symlink: %v", err)
			}
			drv.state.SysfsRoot = testDirs.SysfsRoot
		}

		allocatable[deviceUID].CurrentDriver = testcase.currentDriver

		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("refreshDevicesAndPublish panicked unexpectedly: %v", r)
				}
			}()

			drv.refreshDeviceOnDriverEvent(context.Background(), &udev.Event{Action: testcase.eventAction, Devpath: testcase.devpath})
		}()

		drv.state.Lock()
		//nolint:forcetypeassert
		updatedAllocatable := drv.state.Allocatable.(map[string]*device.DeviceInfo)
		updated := updatedAllocatable[testcase.expectedDeviceUID]
		drv.state.Unlock()

		if updated == nil {
			t.Fatalf("expected allocatable to include %q", testcase.expectedDeviceUID)
		}

		if updated.CurrentDriver != testcase.expectedCurrentDriver {
			t.Errorf("expected CurrentDriver to be %q, got %q", testcase.expectedCurrentDriver, updated.CurrentDriver)
		}

	}
}

func TestShouldProcessUdevEvent(t *testing.T) {
	deviceUID := "0000-00-02-0-0x56c0"

	drv := &driver{
		state: &nodeState{
			Allocatable: map[string]*device.DeviceInfo{
				deviceUID: {
					UID:        deviceUID,
					PCIAddress: "0000:00:02.0",
				},
			},
		},
	}

	type testCase struct {
		name     string
		event    *udev.Event
		expected bool
	}

	testcases := []testCase{
		{
			name: "unsupported action is ignored",
			event: &udev.Event{
				Action:    "change",
				Subsystem: "drm",
				Devpath:   "/devices/pci0000:00/0000:00:02.0/drm/card0",
			},
			expected: false,
		},
		{
			name: "drm card device bind event is processed",
			event: &udev.Event{
				Action:     "bind",
				Properties: map[string]string{"Driver": "i915"},
				Devpath:    "/devices/pci0000:00/0000:00:02.0/drm/card0",
			},
			expected: true,
		},
		{
			name: "vfio-pci bind event is processed",
			event: &udev.Event{
				Action:     "bind",
				Properties: map[string]string{"Driver": "vfio-pci"},
				Devpath:    "/devices/pci0000:00/0000:00:02.0/vfio-dev/vfio0",
			},
			expected: true,
		},
		{
			name: "unbind event is processed",
			event: &udev.Event{
				Action:  "unbind",
				Devpath: "/devices/pci0000:00/0000:00:02.0/vfio-dev/vfio0",
			},
			expected: true,
		},
		{
			name: "bind event for non-GPU device is ignored",
			event: &udev.Event{
				Action:     "bind",
				Properties: map[string]string{"Driver": "vfio-pci"},
				Devpath:    "/devices/pci0000:00/0000:00:03.0/vfio-dev/vfio0",
			},
			expected: false,
		},
		{
			name: "unbind event for non-GPU device is ignored",
			event: &udev.Event{
				Action:  "unbind",
				Devpath: "/devices/pci0000:00/0000:00:03.0/vfio-dev/vfio0",
			},
			expected: false,
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			result := drv.shouldProcessUdevEvent(testcase.event)
			if result != testcase.expected {
				t.Fatalf("expected shouldProcessUdevEvent()=%v, got %v", testcase.expected, result)
			}
		})
	}
}

func TestShouldPublishResourceSlice(t *testing.T) {
	const (
		preparedDeviceUID  = "0000-00-02-0-0x56c0"
		preparedPCIAddress = "0000:00:02.0"
		freeDeviceUID      = "0000-00-03-0-0x56c0"
		freeDevicePCI      = "0000:00:03.0"
	)

	drv := &driver{
		state: &nodeState{
			Allocatable: map[string]*device.DeviceInfo{
				preparedDeviceUID: {
					UID:        preparedDeviceUID,
					PCIAddress: preparedPCIAddress,
				},
				freeDeviceUID: {
					UID:        freeDeviceUID,
					PCIAddress: freeDevicePCI,
				},
			},
			Prepared: ClaimPreparations{
				"claim-1": {
					PreparedDevices: []PreparedDevice{
						{
							KubeletpluginDevice: kubeletplugin.Device{
								DeviceName: preparedDeviceUID,
							},
						},
					},
				},
			},
		},
	}

	testcases := []struct {
		name                    string
		action                  string
		uid                     string
		shouldUntaintNoDRMBound bool
		expected                bool
	}{
		{
			name:                    "bind event with shouldUntaintNoDRMBound=true publishes",
			action:                  "bind",
			uid:                     preparedDeviceUID,
			shouldUntaintNoDRMBound: true,
			expected:                true,
		},
		{
			name:                    "bind event with shouldUntaintNoDRMBound=false does not publish",
			action:                  "bind",
			uid:                     preparedDeviceUID,
			shouldUntaintNoDRMBound: false,
			expected:                false,
		},
		{
			name:     "unbind for prepared device does not publish",
			action:   "unbind",
			uid:      preparedDeviceUID,
			expected: false,
		},
		{
			name:     "unbind for unprepared device publishes",
			action:   "unbind",
			uid:      freeDeviceUID,
			expected: true,
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			if got := drv.shouldPublishResourceSlice(testcase.action, testcase.uid, testcase.shouldUntaintNoDRMBound); got != testcase.expected {
				t.Fatalf("expected shouldPublishResourceSlice()=%v, got %v", testcase.expected, got)
			}
		})
	}
}

func TestHandleError(t *testing.T) {
	type testCase struct {
		name    string
		err     error
		message string
	}

	testcases := []testCase{
		{
			name:    "recoverable error",
			err:     kubeletplugin.ErrRecoverable,
			message: "recoverable error occurred",
		},
		{
			name:    "non-recoverable error",
			err:     errors.New("some other error"),
			message: "non-recoverable error occurred",
		},
		{
			name:    "nil error",
			err:     nil,
			message: "nil error message",
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			drv := &driver{
				state: &nodeState{},
			}

			// Should not panic
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("HandleError panicked unexpectedly: %v", r)
				}
			}()

			drv.HandleError(context.TODO(), testcase.err, testcase.message)
		})
	}
}

func waitForWatchDevicesExit(t *testing.T, done <-chan struct{}, timeout time.Duration) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(timeout):
		t.Fatal("watchDevices did not exit before timeout")
	}
}

func TestWatchDevices_ContextCancelledBeforeStart(t *testing.T) {
	drv := &driver{}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		drv.watchDevices(ctx)
	}()

	waitForWatchDevicesExit(t, done, 3*time.Second)
}

func TestWatchDevices_ContextCancelledAfterStart(t *testing.T) {
	drv := &driver{}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		defer close(done)
		drv.watchDevices(ctx)
	}()

	// Give goroutine a brief chance to start monitor setup, then cancel.
	time.Sleep(50 * time.Millisecond)
	cancel()

	waitForWatchDevicesExit(t, done, 3*time.Second)
}
