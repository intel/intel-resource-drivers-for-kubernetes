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
			"0000-00-02-0-0x56c0": {Model: "0x56c0", MemoryMiB: 8192, DeviceType: "gpu", CardName: "card0", MEIName: "mei0", RenderDName: "renderD128", UID: "0000-00-02-0-0x56c0", MaxVFs: 16, Driver: "i915"},
			"0000-00-03-0-0x56c0": {Model: "0x56c0", MemoryMiB: 8192, DeviceType: "gpu", CardName: "card1", MEIName: "mei1", RenderDName: "renderD128", UID: "0000-00-03-0-0x56c0", MaxVFs: 16, Driver: "xe"},
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
						{Requests: []string{"monitor"}, PoolName: "node1", DeviceName: "0000-00-02-0-0x56c0", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-02-0-0x56c0", "intel.com/gpu-mei=mei0"}},
						{Requests: []string{"monitor"}, PoolName: "node1", DeviceName: "0000-00-03-0-0x56c0", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-0-0x56c0", "intel.com/gpu-mei=mei1"}},
						{Requests: []string{"monitor"}, PoolName: "node1", DeviceName: "0000-00-03-1-0x56c0", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-1-0x56c0"}},
						{Requests: []string{"monitor"}, PoolName: "node1", DeviceName: "0000-00-04-0-0x0000", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-04-0-0x0000", "intel.com/gpu-mei=mei2"}},
					},
				},
			},
			initialPreparedClaims: ClaimPreparations{},
			expectedPreparedClaims: ClaimPreparations{
				"uid3": {
					PreparedDevices: []PreparedDevice{
						{
							KubeletpluginDevice: kubeletplugin.Device{Requests: []string{"monitor"}, PoolName: "node1", DeviceName: "0000-00-02-0-0x56c0", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-02-0-0x56c0", "intel.com/gpu-mei=mei0"}},
							AdminAccess:         true,
						},
						{
							KubeletpluginDevice: kubeletplugin.Device{Requests: []string{"monitor"}, PoolName: "node1", DeviceName: "0000-00-03-0-0x56c0", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-0-0x56c0", "intel.com/gpu-mei=mei1"}},
							AdminAccess:         true,
						},
						{
							KubeletpluginDevice: kubeletplugin.Device{Requests: []string{"monitor"}, PoolName: "node1", DeviceName: "0000-00-03-1-0x56c0", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-1-0x56c0"}},
							AdminAccess:         true,
						},
						{
							KubeletpluginDevice: kubeletplugin.Device{Requests: []string{"monitor"}, PoolName: "node1", DeviceName: "0000-00-04-0-0x0000", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-04-0-0x0000", "intel.com/gpu-mei=mei2"}},
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
						{Requests: []string{"monitor"}, PoolName: "node1", DeviceName: "0000-00-02-0-0x56c0", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-02-0-0x56c0", "intel.com/gpu-mei=mei0"}},
						{Requests: []string{"monitor"}, PoolName: "node1", DeviceName: "0000-00-03-0-0x56c0", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-0-0x56c0", "intel.com/gpu-mei=mei1"}},
						{Requests: []string{"monitor"}, PoolName: "node1", DeviceName: "0000-00-03-1-0x56c0", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-1-0x56c0"}},
						{Requests: []string{"monitor"}, PoolName: "node1", DeviceName: "0000-00-04-0-0x0000", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-04-0-0x0000", "intel.com/gpu-mei=mei2"}},
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
							KubeletpluginDevice: kubeletplugin.Device{Requests: []string{"monitor"}, PoolName: "node1", DeviceName: "0000-00-02-0-0x56c0", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-02-0-0x56c0", "intel.com/gpu-mei=mei0"}},
							AdminAccess:         true,
						},
						{
							KubeletpluginDevice: kubeletplugin.Device{Requests: []string{"monitor"}, PoolName: "node1", DeviceName: "0000-00-03-0-0x56c0", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-0-0x56c0", "intel.com/gpu-mei=mei1"}},
							AdminAccess:         true,
						},
						{
							KubeletpluginDevice: kubeletplugin.Device{Requests: []string{"monitor"}, PoolName: "node1", DeviceName: "0000-00-03-1-0x56c0", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-1-0x56c0"}},
							AdminAccess:         true,
						},
						{
							KubeletpluginDevice: kubeletplugin.Device{Requests: []string{"monitor"}, PoolName: "node1", DeviceName: "0000-00-04-0-0x0000", CDIDeviceIDs: []string{"intel.com/gpu=0000-00-04-0-0x0000", "intel.com/gpu-mei=mei2"}},
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
				"0000-00-02-0-0x56c0": {Model: "0x56c0", MemoryMiB: 16256, DeviceType: "gpu", CardName: "card0", MEIName: "mei0", RenderDName: "renderD128", UID: "0000-00-02-0-0x56c0", MaxVFs: 16, Driver: "i915", CurrentDriver: "i915"},
				"0000-00-03-0-0x56c0": {Model: "0x56c0", MemoryMiB: 16256, DeviceType: "gpu", CardName: "card1", MEIName: "mei1", RenderDName: "renderD129", UID: "0000-00-03-0-0x56c0", MaxVFs: 16, Driver: "i915", CurrentDriver: "i915"},
				"0000-00-03-1-0x56c0": {Model: "0x56c0", MemoryMiB: 8064, DeviceType: "vf", CardName: "card2", RenderDName: "renderD130", UID: "0000-00-03-1-0x56c0", VFIndex: 0, VFProfile: "flex170_m2", ParentUID: "0000-00-03-0-0x56c0", Driver: "i915", CurrentDriver: "i915"},
				// dummy, no SR-IOV tiles
				"0000-00-04-0-0x0000": {Model: "0x0000", MemoryMiB: 14248, DeviceType: "gpu", CardName: "card3", MEIName: "mei2", RenderDName: "renderD131", UID: "0000-00-04-0-0x0000", MaxVFs: 16, Driver: "i915", CurrentDriver: "i915"},
				"0000-00-05-0-0x56c0": {Model: "0x56c0", MemoryMiB: 16256, DeviceType: "gpu", CardName: "card4", MEIName: "mei3", RenderDName: "renderD128", UID: "0000-00-05-0-0x56c0", MaxVFs: 16, Driver: "xe", CurrentDriver: "xe"},
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
				"0000-b3-00-0-0x0bda": {Model: "0x0bda", MemoryMiB: 49136, DeviceType: "gpu", CardName: "card0", MEIName: "mei0", UID: "0000-b3-00-0-0x0bda", MaxVFs: 63, Driver: "i915", CurrentDriver: "i915"},
				"0000-af-00-0-0x0bda": {Model: "0x0bda", MemoryMiB: 49136, DeviceType: "gpu", CardName: "card1", MEIName: "mei1", UID: "0000-af-00-0-0x0bda", MaxVFs: 63, Driver: "i915", CurrentDriver: "i915"},
				"0000-af-00-1-0x0bda": {Model: "0x0bda", MemoryMiB: 22528, Millicores: 500, DeviceType: "vf", CardName: "card2", UID: "0000-af-00-1-0x0bda", VFIndex: 0, VFProfile: "max_47g_c2", ParentUID: "0000-af-00-0-0x0bda", Driver: "i915", CurrentDriver: "i915"},
				"0000-af-00-2-0x0bda": {Model: "0x0bda", MemoryMiB: 22528, Millicores: 500, DeviceType: "vf", CardName: "card3", UID: "0000-af-00-2-0x0bda", VFIndex: 1, VFProfile: "max_47g_c2", ParentUID: "0000-af-00-0-0x0bda", Driver: "i915", CurrentDriver: "i915"},
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

	if err := fakesysfs.FakeSysFsGpuContents(
		testDirs.SysfsRoot,
		testDirs.DevfsRoot,
		device.DevicesInfo{
			deviceUID: {
				UID:           deviceUID,
				PCIAddress:    "0000:00:02.0",
				Model:         "0x56c0",
				ModelName:     "Flex 170",
				FamilyName:    "Data Center Flex",
				CardName:      "card0",
				MEIName:       "mei0",
				RenderDName:   "renderD128",
				DeviceType:    "gpu",
				Driver:        device.SysfsI915DriverName,
				CurrentDriver: device.SysfsI915DriverName,
			},
		},
		false,
	); err != nil {
		t.Fatalf("setup error: could not create fake sysfs: %v", err)
	}

	drv, err := getFakeDriver(testDirs)
	if err != nil {
		t.Fatalf("could not create fake driver: %v", err)
	}
	defer func() { _ = drv.Shutdown(context.TODO()) }()

	preparedClaimsFilePath := path.Join(testDirs.KubeletPluginDir, device.PreparedClaimsFileName)

	//nolint:forcetypeassert
	allocatable := drv.state.Allocatable.(map[string]*device.DeviceInfo)
	if len(allocatable) != 1 {
		t.Fatalf("expected 1 allocatable device, got %d", len(allocatable))
	}

	type testCase struct {
		name                  string
		udevEvent             *udev.Event
		expectedDeviceUID     string
		innitialCurrentDriver string
		initialCardName       string
		initialRenderDName    string
		expectedCurrentDriver string
		expectedCardName      string
		expectedRenderDName   string
	}

	testcases := []testCase{
		{
			name:                  "unbind event changes current driver unbound",
			udevEvent:             &udev.Event{Action: "unbind", Devpath: "/devices/pci0000:00/0000:00:02.0/drm/card0", Subsystem: "pci", Properties: map[string]string{"PCI_SLOT_NAME": "0000:00:02.0"}},
			expectedDeviceUID:     deviceUID,
			innitialCurrentDriver: "i915",
			initialCardName:       "card0",
			initialRenderDName:    "renderD128",
			expectedCurrentDriver: "",
			expectedCardName:      "",
			expectedRenderDName:   "",
		},
		{
			name:                  "bind event changes current driver to i915 and keeps drm indexes when unchanged",
			udevEvent:             &udev.Event{Action: "bind", Devpath: "/devices/pci0000:00/0000:00:02.0/drm/card0", Subsystem: "pci", Properties: map[string]string{"PCI_SLOT_NAME": "0000:00:02.0", "DRIVER": "i915"}},
			expectedDeviceUID:     deviceUID,
			innitialCurrentDriver: "vfio-pci",
			initialCardName:       "card0",
			initialRenderDName:    "renderD128",
			expectedCurrentDriver: "i915",
			expectedCardName:      "card0",
			expectedRenderDName:   "renderD128",
		},
		{
			name:                  "bind event changes current driver to i915 and refreshes drm indexes when changed",
			udevEvent:             &udev.Event{Action: "bind", Devpath: "/devices/pci0000:00/0000:00:02.0/drm/card0", Subsystem: "pci", Properties: map[string]string{"PCI_SLOT_NAME": "0000:00:02.0", "DRIVER": "i915"}},
			expectedDeviceUID:     deviceUID,
			innitialCurrentDriver: "vfio-pci",
			initialCardName:       "card1",
			initialRenderDName:    "renderD129",
			expectedCurrentDriver: "i915",
			expectedCardName:      "card0",
			expectedRenderDName:   "renderD128",
		},
		{
			name:                  "bind event changes current driver to vfio-pci",
			udevEvent:             &udev.Event{Action: "bind", Devpath: "/devices/pci0000:00/0000:00:02.0/vfio-dev/vfio0", Subsystem: "pci", Properties: map[string]string{"PCI_SLOT_NAME": "0000:00:02.0", "DRIVER": "vfio-pci"}},
			expectedDeviceUID:     deviceUID,
			innitialCurrentDriver: "i915",
			initialCardName:       "card0",
			initialRenderDName:    "renderD128",
			expectedCurrentDriver: "vfio-pci",
			expectedCardName:      "",
			expectedRenderDName:   "",
		},
	}

	for _, testcase := range testcases {
		t.Log(testcase.name)

		switch testcase.udevEvent.Action {
		case "bind":
			// Wipe sysfs, devfs and re-create fake device - easier than fiddling with fake sysfs manually.
			for _, toDelete := range []string{"bus", "devices", "class"} {
				if err := os.RemoveAll(path.Join(testDirs.SysfsRoot, toDelete)); err != nil && !os.IsNotExist(err) {
					t.Fatalf("setup error: failed removing fake sysfs dir: %v", err)
				}
			}
			for _, toDelete := range []string{"dri", "vfio"} {
				if err := os.RemoveAll(path.Join(testDirs.DevfsRoot, toDelete)); err != nil && !os.IsNotExist(err) {
					t.Fatalf("setup error: failed removing fake devfs dir: %v", err)
				}
			}
			switch testcase.udevEvent.Properties["DRIVER"] {
			case "i915":
				if err := fakesysfs.FakeSysFsGpuContents(testDirs.SysfsRoot, testDirs.DevfsRoot, device.DevicesInfo{
					deviceUID: {
						UID:           deviceUID,
						PCIAddress:    "0000:00:02.0",
						Model:         "0x56c0",
						ModelName:     "Flex 170",
						FamilyName:    "Data Center Flex",
						CardName:      "card0",
						MEIName:       "mei0",
						RenderDName:   "renderD128",
						DeviceType:    "gpu",
						Driver:        device.SysfsI915DriverName,
						CurrentDriver: device.SysfsI915DriverName,
					},
				},
					false); err != nil {
					t.Fatalf("setup error: could not create fake sysfs: %v", err)
				}
			case "vfio-pci":
				if err := fakesysfs.FakeSysFsGpuContents(testDirs.SysfsRoot, testDirs.DevfsRoot, device.DevicesInfo{
					deviceUID: {
						UID:           deviceUID,
						PCIAddress:    "0000:00:02.0",
						Model:         "0x56c0",
						ModelName:     "Flex 170",
						FamilyName:    "Data Center Flex",
						IOMMUGroup:    "15",
						VFIODevice:    "vfio0",
						DeviceType:    "gpu",
						Driver:        device.SysfsVFIODriverName,
						CurrentDriver: device.SysfsVFIODriverName,
					},
				},
					false); err != nil {
					t.Fatalf("setup error: could not create fake sysfs: %v", err)
				}
			}
		case "unbind":
			driverLink := path.Join(testDirs.SysfsRoot, "devices/pci0000:00/0000:00:02.0/driver")
			if err := os.Remove(driverLink); err != nil && !os.IsNotExist(err) {
				t.Fatalf("setup error: failed removing driver symlink: %v", err)
			}
		}

		allocatable[deviceUID].CurrentDriver = testcase.innitialCurrentDriver
		allocatable[deviceUID].CardName = testcase.initialCardName
		allocatable[deviceUID].RenderDName = testcase.initialRenderDName
		drv.state.PreparedClaimsFilePath = preparedClaimsFilePath
		drv.state.SysfsRoot = testDirs.SysfsRoot

		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("refreshDevicesAndPublish panicked unexpectedly: %v", r)
				}
			}()

			drv.refreshDeviceOnDriverEvent(context.Background(), testcase.udevEvent)
		}()

		//nolint:forcetypeassert
		updatedAllocatable := drv.state.Allocatable.(map[string]*device.DeviceInfo)
		updated := updatedAllocatable[testcase.expectedDeviceUID]

		if updated == nil {
			t.Fatalf("expected allocatable to include %q", testcase.expectedDeviceUID)
		}

		if updated.CurrentDriver != testcase.expectedCurrentDriver {
			t.Errorf("expected CurrentDriver to be %q, got %q", testcase.expectedCurrentDriver, updated.CurrentDriver)
		}

		if updated.CardName != testcase.expectedCardName {
			t.Errorf("expected CardName to be %q, got %q", testcase.expectedCardName, updated.CardName)
		}

		if updated.RenderDName != testcase.expectedRenderDName {
			t.Errorf("expected RenderDName to be %q, got %q", testcase.expectedRenderDName, updated.RenderDName)
		}

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
