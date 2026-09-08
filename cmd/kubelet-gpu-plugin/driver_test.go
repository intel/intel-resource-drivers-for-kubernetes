//
// Copyright (C) 2023-2026 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0
//

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
			"0000-00-02-0-0x56c0": {Model: "0x56c0", MemoryMiB: 8192, DeviceType: "gpu", CardName: "card0", MEIName: "mei0", RenderDName: "renderD128", UID: "0000-00-02-0-0x56c0", MaxVFs: 16, Driver: "i915", CurrentDriver: "i915"},
			"0000-00-03-0-0x56c0": {Model: "0x56c0", MemoryMiB: 8192, DeviceType: "gpu", CardName: "card1", MEIName: "mei1", RenderDName: "renderD129", UID: "0000-00-03-0-0x56c0", MaxVFs: 16, Driver: "xe", CurrentDriver: "xe"},
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
		Coreclient: kubefake.NewClientset(),
		DriverFlags: &GPUFlags{
			ManageBinding: true,
		}, // ensure correct type to avoid nil type assertion failure
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
		// Where driver change is expected, bindUnbindWatcher will be used,
		// it needs explicit stopping to close all fsnotify processes.
		driverChange bool
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
				testhelpers.NewClaim("namespace1", "claim1", "uid1", "request1", "gpu.intel.com", "node1", "gpu.intel.com", []string{"0000-00-02-0-0x56c0"}, false),
			},
			expectedResponse: map[types.UID]kubeletplugin.PrepareResult{
				"uid1": {
					Devices: []kubeletplugin.Device{
						{
							Requests:     []string{"request1"},
							PoolName:     "node1",
							DeviceName:   "0000-00-02-0-0x56c0",
							CDIDeviceIDs: []string{"intel.com/gpu=0000-00-02-0-0x56c0"},
							Metadata:     &kubeletplugin.DeviceMetadata{Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pciBusID": {StringValue: &[]string{"0000:00:02.0"}[0]}}},
						},
					},
				},
			},
			initialPreparedClaims: ClaimPreparations{},
			expectedPreparedClaims: ClaimPreparations{
				"uid1": {
					PreparedDevices: []PreparedDevice{
						{
							KubeletpluginDevice: kubeletplugin.Device{
								Requests:     []string{"request1"},
								PoolName:     "node1",
								DeviceName:   "0000-00-02-0-0x56c0",
								CDIDeviceIDs: []string{"intel.com/gpu=0000-00-02-0-0x56c0"},
								Metadata:     &kubeletplugin.DeviceMetadata{Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pciBusID": {StringValue: &[]string{"0000:00:02.0"}[0]}}},
							},
						},
					},
				},
			},
		},
		{
			name: "single existing VF",
			request: []*resourceapi.ResourceClaim{
				testhelpers.NewClaim("namespace2", "claim2", "uid2", "request2", "gpu.intel.com", "node1", "gpu.intel.com", []string{"0000-00-03-1-0x56c0"}, false),
			},
			expectedResponse: map[types.UID]kubeletplugin.PrepareResult{
				"uid2": {
					Devices: []kubeletplugin.Device{
						{
							Requests:     []string{"request2"},
							PoolName:     "node1",
							DeviceName:   "0000-00-03-1-0x56c0",
							CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-1-0x56c0"},
							Metadata:     &kubeletplugin.DeviceMetadata{Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pciBusID": {StringValue: &[]string{"0000:00:03.1"}[0]}}},
						},
					},
				},
			},
			initialPreparedClaims: ClaimPreparations{},
			expectedPreparedClaims: ClaimPreparations{
				"uid2": {
					PreparedDevices: []PreparedDevice{
						{
							KubeletpluginDevice: kubeletplugin.Device{
								Requests:     []string{"request2"},
								PoolName:     "node1",
								DeviceName:   "0000-00-03-1-0x56c0",
								CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-1-0x56c0"},
								Metadata:     &kubeletplugin.DeviceMetadata{Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pciBusID": {StringValue: &[]string{"0000:00:03.1"}[0]}}},
							},
						},
					},
				},
			},
		},
		{
			name: "single GPU without admin access prepare failure because of double allocation",
			request: []*resourceapi.ResourceClaim{
				testhelpers.NewClaim("namespace1", "claim1", "uid0", "request1", "gpu.intel.com", "node1", "gpu.intel.com", []string{"0000-00-02-0-0x56c0"}, false),
			},
			expectedResponse: map[types.UID]kubeletplugin.PrepareResult{
				"uid0": {
					Err: errors.New("device 0000-00-02-0-0x56c0 (pool node1) is already allocated to another claim and cannot be prepared without adminAccess flag"),
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
								Metadata:     &kubeletplugin.DeviceMetadata{Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pciBusID": {StringValue: &[]string{"0000:00:02.0"}[0]}}},
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
								Metadata:     &kubeletplugin.DeviceMetadata{Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pciBusID": {StringValue: &[]string{"0000:00:02.0"}[0]}}},
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
						{
							Requests:     []string{"monitor"},
							PoolName:     "node1",
							DeviceName:   "0000-00-02-0-0x56c0",
							CDIDeviceIDs: []string{"intel.com/gpu=0000-00-02-0-0x56c0", "intel.com/gpu-mei=mei0"},
							Metadata:     &kubeletplugin.DeviceMetadata{Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pciBusID": {StringValue: &[]string{"0000:00:02.0"}[0]}}},
						},
						{
							Requests:     []string{"monitor"},
							PoolName:     "node1",
							DeviceName:   "0000-00-03-0-0x56c0",
							CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-0-0x56c0", "intel.com/gpu-mei=mei1"},
							Metadata:     &kubeletplugin.DeviceMetadata{Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pciBusID": {StringValue: &[]string{"0000:00:03.0"}[0]}}},
						},
						{
							Requests:     []string{"monitor"},
							PoolName:     "node1",
							DeviceName:   "0000-00-03-1-0x56c0",
							CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-1-0x56c0"},
							Metadata:     &kubeletplugin.DeviceMetadata{Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pciBusID": {StringValue: &[]string{"0000:00:03.1"}[0]}}},
						},
						{
							Requests:     []string{"monitor"},
							PoolName:     "node1",
							DeviceName:   "0000-00-04-0-0x0000",
							CDIDeviceIDs: []string{"intel.com/gpu=0000-00-04-0-0x0000", "intel.com/gpu-mei=mei2"},
							Metadata:     &kubeletplugin.DeviceMetadata{Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pciBusID": {StringValue: &[]string{"0000:00:04.0"}[0]}}},
						},
					},
				},
			},
			initialPreparedClaims: ClaimPreparations{},
			expectedPreparedClaims: ClaimPreparations{
				"uid3": {
					PreparedDevices: []PreparedDevice{
						{
							KubeletpluginDevice: kubeletplugin.Device{
								Requests:     []string{"monitor"},
								PoolName:     "node1",
								DeviceName:   "0000-00-02-0-0x56c0",
								CDIDeviceIDs: []string{"intel.com/gpu=0000-00-02-0-0x56c0", "intel.com/gpu-mei=mei0"},
								Metadata:     &kubeletplugin.DeviceMetadata{Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pciBusID": {StringValue: &[]string{"0000:00:02.0"}[0]}}},
							},
							AdminAccess: true,
						},
						{
							KubeletpluginDevice: kubeletplugin.Device{
								Requests:     []string{"monitor"},
								PoolName:     "node1",
								DeviceName:   "0000-00-03-0-0x56c0",
								CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-0-0x56c0", "intel.com/gpu-mei=mei1"},
								Metadata:     &kubeletplugin.DeviceMetadata{Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pciBusID": {StringValue: &[]string{"0000:00:03.0"}[0]}}},
							},
							AdminAccess: true,
						},
						{
							KubeletpluginDevice: kubeletplugin.Device{
								Requests:     []string{"monitor"},
								PoolName:     "node1",
								DeviceName:   "0000-00-03-1-0x56c0",
								CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-1-0x56c0"},
								Metadata:     &kubeletplugin.DeviceMetadata{Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pciBusID": {StringValue: &[]string{"0000:00:03.1"}[0]}}},
							},
							AdminAccess: true,
						},
						{
							KubeletpluginDevice: kubeletplugin.Device{
								Requests:     []string{"monitor"},
								PoolName:     "node1",
								DeviceName:   "0000-00-04-0-0x0000",
								CDIDeviceIDs: []string{"intel.com/gpu=0000-00-04-0-0x0000", "intel.com/gpu-mei=mei2"},
								Metadata:     &kubeletplugin.DeviceMetadata{Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pciBusID": {StringValue: &[]string{"0000:00:04.0"}[0]}}},
							},
							AdminAccess: true,
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
						{
							Requests:     []string{"monitor"},
							PoolName:     "node1",
							DeviceName:   "0000-00-02-0-0x56c0",
							CDIDeviceIDs: []string{"intel.com/gpu=0000-00-02-0-0x56c0", "intel.com/gpu-mei=mei0"},
							Metadata:     &kubeletplugin.DeviceMetadata{Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pciBusID": {StringValue: &[]string{"0000:00:02.0"}[0]}}},
						},
						{
							Requests:     []string{"monitor"},
							PoolName:     "node1",
							DeviceName:   "0000-00-03-0-0x56c0",
							CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-0-0x56c0", "intel.com/gpu-mei=mei1"},
							Metadata:     &kubeletplugin.DeviceMetadata{Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pciBusID": {StringValue: &[]string{"0000:00:03.0"}[0]}}},
						},
						{
							Requests:     []string{"monitor"},
							PoolName:     "node1",
							DeviceName:   "0000-00-03-1-0x56c0",
							CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-1-0x56c0"},
							Metadata:     &kubeletplugin.DeviceMetadata{Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pciBusID": {StringValue: &[]string{"0000:00:03.1"}[0]}}},
						},
						{
							Requests:     []string{"monitor"},
							PoolName:     "node1",
							DeviceName:   "0000-00-04-0-0x0000",
							CDIDeviceIDs: []string{"intel.com/gpu=0000-00-04-0-0x0000", "intel.com/gpu-mei=mei2"},
							Metadata:     &kubeletplugin.DeviceMetadata{Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pciBusID": {StringValue: &[]string{"0000:00:04.0"}[0]}}},
						},
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
								Metadata:     &kubeletplugin.DeviceMetadata{Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pciBusID": {StringValue: &[]string{"0000:00:03.1"}[0]}}},
							},
						},
					},
				},
			},
			expectedPreparedClaims: ClaimPreparations{
				"uid3": {
					PreparedDevices: []PreparedDevice{
						{
							KubeletpluginDevice: kubeletplugin.Device{
								Requests:     []string{"monitor"},
								PoolName:     "node1",
								DeviceName:   "0000-00-02-0-0x56c0",
								CDIDeviceIDs: []string{"intel.com/gpu=0000-00-02-0-0x56c0", "intel.com/gpu-mei=mei0"},
								Metadata:     &kubeletplugin.DeviceMetadata{Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pciBusID": {StringValue: &[]string{"0000:00:02.0"}[0]}}},
							},
							AdminAccess: true,
						},
						{
							KubeletpluginDevice: kubeletplugin.Device{
								Requests:     []string{"monitor"},
								PoolName:     "node1",
								DeviceName:   "0000-00-03-0-0x56c0",
								CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-0-0x56c0", "intel.com/gpu-mei=mei1"},
								Metadata:     &kubeletplugin.DeviceMetadata{Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pciBusID": {StringValue: &[]string{"0000:00:03.0"}[0]}}},
							},
							AdminAccess: true,
						},
						{
							KubeletpluginDevice: kubeletplugin.Device{
								Requests:     []string{"monitor"},
								PoolName:     "node1",
								DeviceName:   "0000-00-03-1-0x56c0",
								CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-1-0x56c0"},
								Metadata:     &kubeletplugin.DeviceMetadata{Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pciBusID": {StringValue: &[]string{"0000:00:03.1"}[0]}}},
							},
							AdminAccess: true,
						},
						{
							KubeletpluginDevice: kubeletplugin.Device{
								Requests:     []string{"monitor"},
								PoolName:     "node1",
								DeviceName:   "0000-00-04-0-0x0000",
								CDIDeviceIDs: []string{"intel.com/gpu=0000-00-04-0-0x0000", "intel.com/gpu-mei=mei2"},
								Metadata:     &kubeletplugin.DeviceMetadata{Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pciBusID": {StringValue: &[]string{"0000:00:04.0"}[0]}}},
							},
							AdminAccess: true,
						},
					},
				},
				"uid4": {
					PreparedDevices: []PreparedDevice{
						{
							KubeletpluginDevice: kubeletplugin.Device{
								Requests:     []string{"request4"},
								PoolName:     "node1",
								DeviceName:   "0000-00-03-1-0x56c0",
								CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-1-0x56c0"},
								Metadata:     &kubeletplugin.DeviceMetadata{Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pciBusID": {StringValue: &[]string{"0000:00:03.1"}[0]}}},
							},
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
						{
							Requests:     []string{"request4"},
							PoolName:     "node1",
							DeviceName:   "0000-00-03-1-0x56c0",
							CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-1-0x56c0"},
							Metadata:     &kubeletplugin.DeviceMetadata{Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pciBusID": {StringValue: &[]string{"0000:00:03.1"}[0]}}},
						},
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
								Metadata:     &kubeletplugin.DeviceMetadata{Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pciBusID": {StringValue: &[]string{"0000:00:03.1"}[0]}}},
							},
						},
					},
				},
			},
			expectedPreparedClaims: ClaimPreparations{
				"uid4": {
					PreparedDevices: []PreparedDevice{
						{
							KubeletpluginDevice: kubeletplugin.Device{
								Requests:     []string{"request4"},
								PoolName:     "node1",
								DeviceName:   "0000-00-03-1-0x56c0",
								CDIDeviceIDs: []string{"intel.com/gpu=0000-00-03-1-0x56c0"},
								Metadata:     &kubeletplugin.DeviceMetadata{Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pciBusID": {StringValue: &[]string{"0000:00:03.1"}[0]}}},
							},
						},
					},
				},
			},
		},
		{
			name: "single Xe GPU",
			request: []*resourceapi.ResourceClaim{
				testhelpers.NewClaim("namespacexe", "claimxe", "uidxe", "requestxe", "gpu.intel.com", "node1", "gpu.intel.com", []string{"0000-00-05-0-0xe211"}, false),
			},
			expectedResponse: map[types.UID]kubeletplugin.PrepareResult{
				"uidxe": {
					Devices: []kubeletplugin.Device{
						{
							Requests:     []string{"requestxe"},
							PoolName:     "node1",
							DeviceName:   "0000-00-05-0-0xe211",
							CDIDeviceIDs: []string{"intel.com/gpu=0000-00-05-0-0xe211"},
							Metadata:     &kubeletplugin.DeviceMetadata{Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pciBusID": {StringValue: &[]string{"0000:00:05.0"}[0]}}},
						},
					},
				},
			},
			initialPreparedClaims: ClaimPreparations{},
			expectedPreparedClaims: ClaimPreparations{
				"uidxe": {
					PreparedDevices: []PreparedDevice{
						{
							KubeletpluginDevice: kubeletplugin.Device{
								Requests:     []string{"requestxe"},
								PoolName:     "node1",
								DeviceName:   "0000-00-05-0-0xe211",
								CDIDeviceIDs: []string{"intel.com/gpu=0000-00-05-0-0xe211"},
								Metadata:     &kubeletplugin.DeviceMetadata{Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pciBusID": {StringValue: &[]string{"0000:00:05.0"}[0]}}},
							},
						},
					},
				},
			},
		},
		{
			name: "single VFIO GPU, no driver change",
			request: []*resourceapi.ResourceClaim{
				testhelpers.NewClaim("namespacevfio", "claimvfio", "uidvfio", "requestvfio", "gpu.intel.com", "node1", "gpu-vfio.intel.com", []string{"0000-00-06-0-0xe211"}, false),
			},
			expectedResponse: map[types.UID]kubeletplugin.PrepareResult{
				"uidvfio": {
					Devices: []kubeletplugin.Device{
						{
							Requests:     []string{"requestvfio"},
							PoolName:     "node1",
							DeviceName:   "0000-00-06-0-0xe211",
							CDIDeviceIDs: []string{"intel.com/gpu=0000-00-06-0-0xe211"},
							Metadata:     &kubeletplugin.DeviceMetadata{Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pciBusID": {StringValue: &[]string{"0000:00:06.0"}[0]}}},
						},
					},
				},
			},
			initialPreparedClaims: ClaimPreparations{},
			expectedPreparedClaims: ClaimPreparations{
				"uidvfio": {
					PreparedDevices: []PreparedDevice{
						{
							KubeletpluginDevice: kubeletplugin.Device{
								Requests:     []string{"requestvfio"},
								PoolName:     "node1",
								DeviceName:   "0000-00-06-0-0xe211",
								CDIDeviceIDs: []string{"intel.com/gpu=0000-00-06-0-0xe211"},
								Metadata:     &kubeletplugin.DeviceMetadata{Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pciBusID": {StringValue: &[]string{"0000:00:06.0"}[0]}}},
							},
						},
					},
				},
			},
		},
		{
			name: "single VFIO GPU, driver change from xe to xe-vfio-pci",
			request: []*resourceapi.ResourceClaim{
				testhelpers.NewClaim("namespacevfio", "claimvfio", "uidvfio", "requestvfio", "gpu.intel.com", "node1", "gpu-vfio.intel.com", []string{"0000-00-05-0-0xe211"}, false),
			},
			expectedResponse: map[types.UID]kubeletplugin.PrepareResult{
				"uidvfio": {
					Devices: []kubeletplugin.Device{
						{
							Requests:     []string{"requestvfio"},
							PoolName:     "node1",
							DeviceName:   "0000-00-05-0-0xe211",
							CDIDeviceIDs: []string{"intel.com/gpu=0000-00-05-0-0xe211"},
							Metadata:     &kubeletplugin.DeviceMetadata{Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pciBusID": {StringValue: &[]string{"0000:00:05.0"}[0]}}},
						},
					},
				},
			},
			initialPreparedClaims: ClaimPreparations{},
			expectedPreparedClaims: ClaimPreparations{
				"uidvfio": {
					PreparedDevices: []PreparedDevice{
						{
							KubeletpluginDevice: kubeletplugin.Device{
								Requests:     []string{"requestvfio"},
								PoolName:     "node1",
								DeviceName:   "0000-00-05-0-0xe211",
								CDIDeviceIDs: []string{"intel.com/gpu=0000-00-05-0-0xe211"},
								Metadata:     &kubeletplugin.DeviceMetadata{Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pciBusID": {StringValue: &[]string{"0000:00:05.0"}[0]}}},
							},
						},
					},
				},
			},
			driverChange: true,
		},
		{
			name: "single VFIO GPU, driver change from xe-vfio-pci to xe",
			request: []*resourceapi.ResourceClaim{
				testhelpers.NewClaim("namespacexe", "claimxe", "uidxe", "requestxe", "gpu.intel.com", "node1", "gpu.intel.com", []string{"0000-00-06-0-0xe211"}, false),
			},
			expectedResponse: map[types.UID]kubeletplugin.PrepareResult{
				"uidxe": {
					Devices: []kubeletplugin.Device{
						{
							Requests:     []string{"requestxe"},
							PoolName:     "node1",
							DeviceName:   "0000-00-06-0-0xe211",
							CDIDeviceIDs: []string{"intel.com/gpu=0000-00-06-0-0xe211"},
							Metadata:     &kubeletplugin.DeviceMetadata{Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pciBusID": {StringValue: &[]string{"0000:00:06.0"}[0]}}},
						},
					},
				},
			},
			initialPreparedClaims: ClaimPreparations{},
			expectedPreparedClaims: ClaimPreparations{
				"uidxe": {
					PreparedDevices: []PreparedDevice{
						{
							KubeletpluginDevice: kubeletplugin.Device{
								Requests:     []string{"requestxe"},
								PoolName:     "node1",
								DeviceName:   "0000-00-06-0-0xe211",
								CDIDeviceIDs: []string{"intel.com/gpu=0000-00-06-0-0xe211"},
								Metadata:     &kubeletplugin.DeviceMetadata{Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pciBusID": {StringValue: &[]string{"0000:00:06.0"}[0]}}},
							},
						},
					},
				},
			},
			driverChange: true,
		},
		{
			// The first subrequest of the prioritized list is selected by the
			// scheduler: plain GPU, no driver change is needed.
			name: "firstAvailable request, GPU subrequest selected",
			request: []*resourceapi.ResourceClaim{
				testhelpers.NewClaimFirstAvailable(
					"namespacefa", "claimfa", "uidfa", "requestfa", "gpu.intel.com", "node1",
					[]testhelpers.SubRequest{
						{Name: "gpu", DeviceClass: "gpu.intel.com"},
						{Name: "vfio", DeviceClass: "gpu-vfio.intel.com"},
					},
					0,
					[]string{"0000-00-02-0-0x56c0"}),
			},
			expectedResponse: map[types.UID]kubeletplugin.PrepareResult{
				"uidfa": {
					Devices: []kubeletplugin.Device{
						{
							Requests:     []string{"requestfa/gpu"},
							PoolName:     "node1",
							DeviceName:   "0000-00-02-0-0x56c0",
							CDIDeviceIDs: []string{"intel.com/gpu=0000-00-02-0-0x56c0"},
							Metadata:     &kubeletplugin.DeviceMetadata{Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pciBusID": {StringValue: &[]string{"0000:00:02.0"}[0]}}},
						},
					},
				},
			},
			initialPreparedClaims: ClaimPreparations{},
			expectedPreparedClaims: ClaimPreparations{
				"uidfa": {
					PreparedDevices: []PreparedDevice{
						{
							KubeletpluginDevice: kubeletplugin.Device{
								Requests:     []string{"requestfa/gpu"},
								PoolName:     "node1",
								DeviceName:   "0000-00-02-0-0x56c0",
								CDIDeviceIDs: []string{"intel.com/gpu=0000-00-02-0-0x56c0"},
								Metadata:     &kubeletplugin.DeviceMetadata{Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pciBusID": {StringValue: &[]string{"0000:00:02.0"}[0]}}},
							},
						},
					},
				},
			},
		},
		{
			name: "firstAvailable request, VFIO subrequest selected, no driver change",
			request: []*resourceapi.ResourceClaim{
				testhelpers.NewClaimFirstAvailable(
					"namespacefa", "claimfa", "uidfa", "requestfa", "gpu.intel.com", "node1",
					[]testhelpers.SubRequest{
						{Name: "gpu", DeviceClass: "gpu.intel.com"},
						{Name: "vfio", DeviceClass: "gpu-vfio.intel.com"},
					},
					1,
					[]string{"0000-00-06-0-0xe211"}),
			},
			expectedResponse: map[types.UID]kubeletplugin.PrepareResult{
				"uidfa": {
					Devices: []kubeletplugin.Device{
						{
							Requests:     []string{"requestfa/vfio"},
							PoolName:     "node1",
							DeviceName:   "0000-00-06-0-0xe211",
							CDIDeviceIDs: []string{"intel.com/gpu=0000-00-06-0-0xe211"},
							Metadata:     &kubeletplugin.DeviceMetadata{Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pciBusID": {StringValue: &[]string{"0000:00:06.0"}[0]}}},
						},
					},
				},
			},
			initialPreparedClaims: ClaimPreparations{},
			expectedPreparedClaims: ClaimPreparations{
				"uidfa": {
					PreparedDevices: []PreparedDevice{
						{
							KubeletpluginDevice: kubeletplugin.Device{
								Requests:     []string{"requestfa/vfio"},
								PoolName:     "node1",
								DeviceName:   "0000-00-06-0-0xe211",
								CDIDeviceIDs: []string{"intel.com/gpu=0000-00-06-0-0xe211"},
								Metadata:     &kubeletplugin.DeviceMetadata{Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pciBusID": {StringValue: &[]string{"0000:00:06.0"}[0]}}},
							},
						},
					},
				},
			},
		},
		{
			// selected VFIO subrequest requires the kernel driver of the device to be changed from xe to xe-vfio-pci.
			name: "firstAvailable request, VFIO subrequest selected, driver change from xe to xe-vfio-pci",
			request: []*resourceapi.ResourceClaim{
				testhelpers.NewClaimFirstAvailable(
					"namespacefa", "claimfa", "uidfa", "requestfa", "gpu.intel.com", "node1",
					[]testhelpers.SubRequest{
						{Name: "vfio", DeviceClass: "gpu-vfio.intel.com"},
						{Name: "gpu", DeviceClass: "gpu.intel.com"},
					},
					0,
					[]string{"0000-00-05-0-0xe211"}),
			},
			expectedResponse: map[types.UID]kubeletplugin.PrepareResult{
				"uidfa": {
					Devices: []kubeletplugin.Device{
						{
							Requests:     []string{"requestfa/vfio"},
							PoolName:     "node1",
							DeviceName:   "0000-00-05-0-0xe211",
							CDIDeviceIDs: []string{"intel.com/gpu=0000-00-05-0-0xe211"},
							Metadata:     &kubeletplugin.DeviceMetadata{Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pciBusID": {StringValue: &[]string{"0000:00:05.0"}[0]}}},
						},
					},
				},
			},
			initialPreparedClaims: ClaimPreparations{},
			expectedPreparedClaims: ClaimPreparations{
				"uidfa": {
					PreparedDevices: []PreparedDevice{
						{
							KubeletpluginDevice: kubeletplugin.Device{
								Requests:     []string{"requestfa/vfio"},
								PoolName:     "node1",
								DeviceName:   "0000-00-05-0-0xe211",
								CDIDeviceIDs: []string{"intel.com/gpu=0000-00-05-0-0xe211"},
								Metadata:     &kubeletplugin.DeviceMetadata{Attributes: map[string]resourceapi.DeviceAttribute{"resource.kubernetes.io/pciBusID": {StringValue: &[]string{"0000:00:05.0"}[0]}}},
							},
						},
					},
				},
			},
			driverChange: true,
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
				"0000-00-05-0-0xe211": {Model: "0xe211", MemoryMiB: 24576, DeviceType: "gpu", CardName: "card4", MEIName: "mei3", RenderDName: "renderD128", UID: "0000-00-05-0-0xe211", MaxVFs: 16, Driver: "xe", CurrentDriver: "xe"},
				"0000-00-06-0-0xe211": {Model: "0xe211", MemoryMiB: 24576, DeviceType: "gpu", VFIODevice: "vfio0", IOMMUGroup: "15", UID: "0000-00-06-0-0xe211", MaxVFs: 0, Driver: "xe", CurrentDriver: "xe-vfio-pci"},
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
		if testcase.driverChange {
			watcher := fakesysfs.WatchDriverBindUnbind(t, testDirs.SysfsRoot, testDirs.DevfsRoot, false)
			defer watcher.Close()
			time.Sleep(device.DriverChangeDelay)
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

// fakeSurvivabilityGpu re-creates fake sysfs and devfs with a single GPU that is either in
// survivability mode - no DRM devices, only MEI - or fully functional.
func fakeSurvivabilityGpu(t *testing.T, testDirs testhelpers.TestDirsType, deviceUID string, survivability bool) {
	t.Helper()

	gpu := &device.DeviceInfo{
		UID:           deviceUID,
		PCIAddress:    "0000:00:02.0",
		Model:         "0x56c0",
		MEIName:       "mei0",
		DeviceType:    "gpu",
		Driver:        device.SysfsXeDriverName,
		CurrentDriver: device.SysfsXeDriverName,
		Survivability: survivability,
	}
	if !survivability {
		gpu.CardName = "card0"
		gpu.RenderDName = "renderD128"
	}

	recreateFakeGpu(t, testDirs, gpu)
}

// recreateFakeGpu wipes the fake sysfs and devfs contents and recreates them with a single GPU
// in the described state.
func recreateFakeGpu(t *testing.T, testDirs testhelpers.TestDirsType, gpu *device.DeviceInfo) {
	t.Helper()

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

	if err := fakesysfs.FakeSysFsGpuContents(
		testDirs.SysfsRoot, testDirs.DevfsRoot, device.DevicesInfo{gpu.UID: gpu}, false); err != nil {
		t.Fatalf("setup error: could not create fake sysfs: %v", err)
	}
}

// gpuCDIDeviceExists tells whether the GPU CDI device of the device UID is in the CDI registry.
func gpuCDIDeviceExists(t *testing.T, state *nodeState, deviceUID string) bool {
	t.Helper()

	testhelpers.CDICacheDelay()

	return state.CdiCache.GetDevice(device.CDIKind+"="+deviceUID) != nil
}

//nolint:cyclop // test code
func TestRefreshDeviceOnSurvivabilityChange(t *testing.T) {
	testDirs, err := testhelpers.NewTestDirs(device.DriverName)
	defer testhelpers.CleanupTest(t, "TestRefreshDeviceOnSurvivabilityChange", testDirs.TestRoot)
	if err != nil {
		t.Fatalf("setup error: %v", err)
	}

	const deviceUID = "0000-00-02-0-0x56c0"
	const pciAddress = "0000:00:02.0"

	os.Setenv(helpers.DevfsEnvVarName, testDirs.DevfsRoot)
	defer os.Unsetenv(helpers.DevfsEnvVarName)

	// The GPU has broken firmware when the driver starts.
	fakeSurvivabilityGpu(t, testDirs, deviceUID, true)

	drv, err := getFakeDriver(testDirs)
	if err != nil {
		t.Fatalf("could not create fake driver: %v", err)
	}
	defer func() { _ = drv.Shutdown(context.TODO()) }()
	drv.state.SysfsRoot = testDirs.SysfsRoot

	//nolint:forcetypeassert
	allocatable := drv.state.Allocatable.(map[string]*device.DeviceInfo)
	discovered := allocatable[deviceUID]
	if discovered == nil {
		t.Fatalf("expected device %v in allocatable devices: %+v", deviceUID, allocatable)
	}
	if !discovered.Survivability || discovered.Health() != device.HealthUnhealthy {
		t.Errorf("expected discovered device to be in survivability mode and unhealthy, got: %+v", discovered)
	}
	if discovered.MEIName != "mei0" {
		t.Errorf("expected MEI device to be discovered for device in survivability mode, got: %+v", discovered)
	}
	if gpuCDIDeviceExists(t, drv.state, deviceUID) {
		t.Errorf("expected no GPU CDI device for device %v in survivability mode", deviceUID)
	}

	// Firmware was reflashed, the device is functional again.
	fakeSurvivabilityGpu(t, testDirs, deviceUID, false)

	needToPublish, err := drv.state.RefreshDeviceOnDriverEvent(pciAddress, device.SysfsXeDriverName)
	if err != nil {
		t.Fatalf("unexpected error refreshing device: %v", err)
	}
	if !needToPublish {
		t.Error("expected ResourceSlice publishing to be needed after leaving survivability mode")
	}
	if discovered.Survivability {
		t.Errorf("expected device to be healthy after leaving survivability mode, got: %+v", discovered)
	}
	if _, found := discovered.HealthStatus[device.HealthStatusSurvivability]; found {
		t.Errorf("expected device to not have survivability health status, got: %+v", discovered)
	}
	if discovered.CardName != "card0" || discovered.RenderDName != "renderD128" {
		t.Errorf("expected DRM devices to be discovered after leaving survivability mode, got: %+v", discovered)
	}
	if !gpuCDIDeviceExists(t, drv.state, deviceUID) {
		t.Errorf("expected GPU CDI device for device %v after leaving survivability mode", deviceUID)
	}

	// Firmware broke again.
	fakeSurvivabilityGpu(t, testDirs, deviceUID, true)

	needToPublish, err = drv.state.RefreshDeviceOnDriverEvent(pciAddress, device.SysfsXeDriverName)
	if err != nil {
		t.Fatalf("unexpected error refreshing device: %v", err)
	}
	if !needToPublish {
		t.Error("expected ResourceSlice publishing to be needed after entering survivability mode")
	}
	if !discovered.Survivability || discovered.Health() != device.HealthUnhealthy {
		t.Errorf("expected device to be in survivability mode and unhealthy, got: %+v", discovered)
	}
	if discovered.CardName != "" || discovered.RenderDName != "" {
		t.Errorf("expected no DRM devices for device in survivability mode, got: %+v", discovered)
	}
	if gpuCDIDeviceExists(t, drv.state, deviceUID) {
		t.Errorf("expected no GPU CDI device for device %v in survivability mode", deviceUID)
	}
}

// TestRefreshDeviceOnRebindAfterSurvivability covers the recovery of a device with broken firmware
// through kernel driver unbinding: the survivability_mode sysfs file is gone already when the
// kernel driver is unbound, so the CDI spec of the device has to be updated when the device is
// bound back to the kernel driver, based on the changed kernel driver alone.
func TestRefreshDeviceOnRebindAfterSurvivability(t *testing.T) {
	testDirs, err := testhelpers.NewTestDirs(device.DriverName)
	defer testhelpers.CleanupTest(t, "TestRefreshDeviceOnRebindAfterSurvivability", testDirs.TestRoot)
	if err != nil {
		t.Fatalf("setup error: %v", err)
	}

	const deviceUID = "0000-00-02-0-0x56c0"
	const pciAddress = "0000:00:02.0"

	os.Setenv(helpers.DevfsEnvVarName, testDirs.DevfsRoot)
	defer os.Unsetenv(helpers.DevfsEnvVarName)

	// The GPU has broken firmware when the driver starts.
	fakeSurvivabilityGpu(t, testDirs, deviceUID, true)

	drv, err := getFakeDriver(testDirs)
	if err != nil {
		t.Fatalf("could not create fake driver: %v", err)
	}
	defer func() { _ = drv.Shutdown(context.TODO()) }()
	drv.state.SysfsRoot = testDirs.SysfsRoot

	//nolint:forcetypeassert
	discovered := drv.state.Allocatable.(map[string]*device.DeviceInfo)[deviceUID]
	if discovered == nil || !discovered.Survivability {
		t.Fatalf("expected device %v to be discovered in survivability mode, got: %+v", deviceUID, discovered)
	}

	// Kernel driver is unbound from the device for the firmware reflashing.
	recreateFakeGpu(t, testDirs, &device.DeviceInfo{
		UID:        deviceUID,
		PCIAddress: pciAddress,
		Model:      "0x56c0",
		DeviceType: "gpu",
		Driver:     device.SysfsXeDriverName,
	})

	if _, err = drv.state.RefreshDeviceOnDriverEvent(pciAddress, ""); err != nil {
		t.Fatalf("unexpected error refreshing unbound device: %v", err)
	}
	if discovered.CurrentDriver != "" || discovered.Survivability {
		t.Errorf("expected unbound device without survivability mode, got: %+v", discovered)
	}
	if gpuCDIDeviceExists(t, drv.state, deviceUID) {
		t.Errorf("expected no GPU CDI device for unbound device %v", deviceUID)
	}

	// Firmware was reflashed and the kernel driver is bound back to the device.
	fakeSurvivabilityGpu(t, testDirs, deviceUID, false)

	needToPublish, err := drv.state.RefreshDeviceOnDriverEvent(pciAddress, device.SysfsXeDriverName)
	if err != nil {
		t.Fatalf("unexpected error refreshing rebound device: %v", err)
	}
	if !needToPublish {
		t.Error("expected ResourceSlice publishing to be needed after the device was bound back")
	}
	if discovered.CardName != "card0" || discovered.RenderDName != "renderD128" {
		t.Errorf("expected DRM devices to be discovered for rebound device, got: %+v", discovered)
	}
	if !gpuCDIDeviceExists(t, drv.state, deviceUID) {
		t.Errorf("expected GPU CDI device for rebound device %v", deviceUID)
	}
}

// TestPrepareSurvivabilityDevice covers preparing a claim for a device in survivability mode:
// the device is unusable as a GPU until its firmware has been reflashed, so only claims with the
// adminAccess flag - e.g. the firmware reflashing or monitoring deployment - can be prepared for it.
func TestPrepareSurvivabilityDevice(t *testing.T) {
	const deviceUID = "0000-00-02-0-0x56c0"
	const pciAddress = "0000:00:02.0"

	pciAddressAttributes := &kubeletplugin.DeviceMetadata{
		Attributes: map[string]resourceapi.DeviceAttribute{
			"resource.kubernetes.io/pciBusID": {StringValue: &[]string{pciAddress}[0]},
		},
	}

	type testCase struct {
		name                   string
		survivability          bool
		request                *resourceapi.ResourceClaim
		expectedResponse       map[types.UID]kubeletplugin.PrepareResult
		expectedPreparedClaims ClaimPreparations
	}

	testcases := []testCase{
		{
			name:          "claim without admin access is rejected for device in survivability mode",
			survivability: true,
			request: testhelpers.NewClaim(
				"namespace1", "claim1", "uid1", "request1", "gpu.intel.com", "node1", "gpu.intel.com", []string{deviceUID}, false),
			expectedResponse: map[types.UID]kubeletplugin.PrepareResult{
				"uid1": {
					Err: errors.New("device 0000-00-02-0-0x56c0 (pool node1) is in survivability mode and cannot be prepared without adminAccess flag"),
				},
			},
			expectedPreparedClaims: ClaimPreparations{},
		},
		{
			name:          "claim with admin access gets the MEI device of the device in survivability mode",
			survivability: true,
			request: testhelpers.NewMonitoringClaim(
				"namespace2", "monitor", "uid2", "monitor", "gpu.intel.com", "node1", []string{deviceUID}),
			expectedResponse: map[types.UID]kubeletplugin.PrepareResult{
				"uid2": {
					Devices: []kubeletplugin.Device{
						{
							Requests:     []string{"monitor"},
							PoolName:     "node1",
							DeviceName:   deviceUID,
							CDIDeviceIDs: []string{"intel.com/gpu-mei=mei0"},
							Metadata:     pciAddressAttributes,
						},
					},
				},
			},
			expectedPreparedClaims: ClaimPreparations{
				"uid2": {
					PreparedDevices: []PreparedDevice{
						{
							KubeletpluginDevice: kubeletplugin.Device{
								Requests:     []string{"monitor"},
								PoolName:     "node1",
								DeviceName:   deviceUID,
								CDIDeviceIDs: []string{"intel.com/gpu-mei=mei0"},
								Metadata:     pciAddressAttributes,
							},
							AdminAccess: true,
						},
					},
				},
			},
		},
		{
			// Control case: the same claim is prepared when the firmware of the device is intact.
			name:          "claim without admin access is prepared for functional device",
			survivability: false,
			request: testhelpers.NewClaim(
				"namespace1", "claim1", "uid1", "request1", "gpu.intel.com", "node1", "gpu.intel.com", []string{deviceUID}, false),
			expectedResponse: map[types.UID]kubeletplugin.PrepareResult{
				"uid1": {
					Devices: []kubeletplugin.Device{
						{
							Requests:     []string{"request1"},
							PoolName:     "node1",
							DeviceName:   deviceUID,
							CDIDeviceIDs: []string{"intel.com/gpu=" + deviceUID},
							Metadata:     pciAddressAttributes,
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
								DeviceName:   deviceUID,
								CDIDeviceIDs: []string{"intel.com/gpu=" + deviceUID},
								Metadata:     pciAddressAttributes,
							},
						},
					},
				},
			},
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			testDirs, err := testhelpers.NewTestDirs(device.DriverName)
			defer testhelpers.CleanupTest(t, testcase.name, testDirs.TestRoot)
			if err != nil {
				t.Fatalf("setup error: %v", err)
			}

			os.Setenv(helpers.DevfsEnvVarName, testDirs.DevfsRoot)
			defer os.Unsetenv(helpers.DevfsEnvVarName)

			fakeSurvivabilityGpu(t, testDirs, deviceUID, testcase.survivability)

			preparedClaimFilePath := path.Join(testDirs.KubeletPluginDir, device.PreparedClaimsFileName)
			if err := WritePreparedClaimsToFile(preparedClaimFilePath, ClaimPreparations{}); err != nil {
				t.Fatalf("setup error: could not write prepared claims to file: %v", err)
			}

			drv, err := getFakeDriver(testDirs)
			if err != nil {
				t.Fatalf("could not create fake driver: %v", err)
			}
			defer func() { _ = drv.Shutdown(context.TODO()) }()

			response, err := drv.PrepareResourceClaims(context.TODO(), []*resourceapi.ResourceClaim{testcase.request})
			if err != nil {
				t.Fatalf("unexpected error preparing claim: %v", err)
			}

			if !testhelpers.DeepEqualPrepareResults(testcase.expectedResponse, response) {
				t.Errorf("unexpected response: %v, expected response: %v", response, testcase.expectedResponse)
			}

			preparedClaims, err := readPreparedClaimsFromFile(preparedClaimFilePath)
			if err != nil {
				t.Fatalf("unexpected error reading prepared claims: %v", err)
			}

			if !reflect.DeepEqual(testcase.expectedPreparedClaims, preparedClaims) {
				t.Errorf("unexpected PreparedClaims: %v, expected PreparedClaims: %v", preparedClaims, testcase.expectedPreparedClaims)
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
