//
// Copyright (C) 2022-2026 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0
//

package main

import (
	"reflect"
	"strings"
	"testing"

	resourcev1 "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
)

func TestDeviceInfoDeepCopy(t *testing.T) {
	di := device.DeviceInfo{
		UID:         "f",
		Model:       "ff",
		CardName:    "card2",
		RenderDName: "renderD3",
		MemoryMiB:   4,
		Millicores:  5,
		DeviceType:  "fff",
		MaxVFs:      6,
		ParentUID:   "ffff",
		VFProfile:   "fffff",
		VFIndex:     7,
		MEIName:     "mei0",
	}

	dc := di.DeepCopy()

	if !reflect.DeepEqual(&di, dc) {
		t.Fatalf("device infos %v and %v do not match", di, dc)
	}
}

func errorCheck(t *testing.T, name, substr string, err error) {
	switch {
	case err == nil:
		if substr != "" {
			t.Errorf("%v: unexpected success, expected: %s", name, substr)
		}
	case substr == "":
		t.Errorf("%v: unexpected failure: %v", name, err)
	case !strings.Contains(err.Error(), substr):
		t.Errorf("%v: unexpected error: %v, expected error: %v", name, err, substr)
	}
}

func TestGetResourcesTaintsUnboundUnmanagedDevice(t *testing.T) {
	state := &nodeState{
		Allocatable: map[string]*device.DeviceInfo{
			"gpu-unprepared": {
				UID:           "gpu-unprepared",
				PCIAddress:    "0000:00:01.0",
				Model:         "0x56c0",
				ModelName:     "Flex 170",
				FamilyName:    "Data Center Flex",
				MemoryMiB:     16384,
				Driver:        "xe",
				CurrentDriver: "",
			},
			"gpu-prepared": {
				UID:           "gpu-prepared",
				PCIAddress:    "0000:00:02.0",
				Model:         "0x56c1",
				ModelName:     "Flex 140",
				FamilyName:    "Data Center Flex",
				MemoryMiB:     16384,
				Driver:        "xe",
				CurrentDriver: "xe-vfio-pci",
				IOMMUGroup:    "15",
				VFIODevice:    "vfio0",
			},
		},
		Prepared: ClaimPreparations{
			"claim-1": {
				PreparedDevices: []PreparedDevice{
					{
						KubeletpluginDevice: kubeletplugin.Device{
							DeviceName: "gpu-prepared",
						},
						AdminAccess: false,
					},
				},
			},
		},
		NodeName:      "test-node",
		ManageBinding: false,
	}

	resources := state.GetResources()
	devices := resources.Pools["test-node"].Slices[0].Devices
	if len(devices) != 2 {
		t.Fatalf("expected 2 devices, got %d", len(devices))
	}

	deviceByName := map[string]resourcev1.Device{}
	for _, dev := range devices {
		deviceByName[dev.Name] = dev
	}

	if len(deviceByName["gpu-unprepared"].Taints) != 1 || deviceByName["gpu-unprepared"].Taints[0].Key != device.UnboundUnmanagedTaintKey {
		t.Fatalf("unexpected taints: expected 1 x %v taint, got: %v", device.UnboundUnmanagedTaintKey, deviceByName["gpu-unprepared"].Taints)
	}

	if len(deviceByName["gpu-prepared"].Taints) != 0 {
		t.Fatalf("unexpected taints: expected gpu-prepared to have no taints, got: %v", deviceByName["gpu-prepared"].Taints)
	}
}

func TestGetResourcesTaintsPerUnhealthyType(t *testing.T) {
	state := &nodeState{
		Allocatable: map[string]*device.DeviceInfo{
			"gpu-unhealthy": {
				UID:           "gpu-unhealthy",
				PCIAddress:    "0000:00:01.0",
				Driver:        "xe",
				CurrentDriver: "xe",
				HealthStatus: map[string]string{
					"temperature.core.gpu":          device.HealthUnhealthy,
					"frequency":                     device.HealthHealthy,
					device.HealthStatusDeviceAbsent: device.HealthUnhealthy,
				},
			},
		},
		NodeName:      "test-node",
		ManageBinding: true,
	}

	devices := state.GetResources().Pools["test-node"].Slices[0].Devices
	if len(devices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(devices))
	}

	taints := devices[0].Taints
	if len(taints) != 2 {
		t.Fatalf("expected 2 taints, got %d: %v", len(taints), taints)
	}

	if taints[0].Key != "health-DeviceAbsent" || taints[0].Effect != resourcev1.DeviceTaintEffectNoExecute {
		t.Errorf("unexpected taint[0]:  got key=%v, effect=%v, expected key=health-DeviceAbsent, effect=NoExecute", taints[0].Key, taints[0].Effect)
	}

	if taints[1].Key != "health-xpumd-temperature.core.gpu" || taints[1].Effect != resourcev1.DeviceTaintEffectNoExecute {
		t.Errorf("unexpected taint[1]:  got key=%v, effect=%v, expected key=health-xpumd-temperature.core.gpu, effect=NoExecute", taints[1].Key, taints[1].Effect)
	}
}

func TestGetResourcesTaintsUnsupportedHealth(t *testing.T) {
	state := &nodeState{
		Allocatable: map[string]*device.DeviceInfo{
			"gpu-unhealthy": {
				UID:          "gpu-unhealthy",
				HealthStatus: map[string]string{"invalid category": device.HealthUnhealthy},
			},
		},
		NodeName:      "test-node",
		ManageBinding: true,
	}

	taints := state.GetResources().Pools["test-node"].Slices[0].Devices[0].Taints
	if len(taints) != 1 {
		t.Fatalf("expected 1 taint, got %d: %v", len(taints), taints)
	}

	if taints[0].Key != device.UnsupportedHealthTaintKey || taints[0].Effect != resourcev1.DeviceTaintEffectNoExecute {
		t.Errorf("unexpected taint:  got key=%v, effect=%v, expected key=%v, effect=NoExecute", taints[0].Key, taints[0].Effect, device.UnsupportedHealthTaintKey)
	}
}

func TestIsDeviceUsedExclusivelyAlready(t *testing.T) {
	state := &nodeState{
		Allocatable: map[string]*device.DeviceInfo{
			"gpu-prepared": {
				UID:        "gpu-prepared",
				PCIAddress: "0000:00:02.0",
			},
			"gpu-being-prepared": {
				UID:        "gpu-being-prepared",
				PCIAddress: "0000:00:04.0",
			},
			"gpu-free": {
				UID:        "gpu-free",
				PCIAddress: "0000:00:03.0",
			},
			"gpu-prepared-with-admin-access": {
				UID:        "gpu-prepared-with-admin-access",
				PCIAddress: "0000:00:01.0",
			},
		},
		Prepared: ClaimPreparations{
			"claim-1": {
				PreparedDevices: []PreparedDevice{
					{
						KubeletpluginDevice: kubeletplugin.Device{
							DeviceName: "gpu-prepared",
							PoolName:   "pool0",
						},
					},
				},
			},
			"claim-2": {
				PreparedDevices: []PreparedDevice{
					{
						KubeletpluginDevice: kubeletplugin.Device{
							DeviceName: "gpu-prepared-with-admin-access",
							PoolName:   "pool0",
						},
						AdminAccess: true,
					},
				},
			},
			"claim-new": {
				PreparedDevices: []PreparedDevice{
					{
						KubeletpluginDevice: kubeletplugin.Device{
							DeviceName: "gpu-being-prepared",
							PoolName:   "pool0",
						},
					},
				},
			},
		},
	}

	testcases := []struct {
		name        string
		uid         string
		expected    bool
		expectError bool
		claimUid    types.UID
	}{
		{
			name:     "prepared device",
			uid:      "gpu-prepared",
			expected: true,
			claimUid: "claim-x",
		},
		{
			name:     "unprepared device",
			uid:      "gpu-free",
			claimUid: "claim-x",
			expected: false,
		},
		{
			name:     "unknown device",
			uid:      "gpu-unknown",
			claimUid: "claim-x",
			expected: false,
		},
		{
			name:     "prepared device with admin access",
			uid:      "gpu-prepared-with-admin-access",
			claimUid: "claim-x",
			expected: false, // AdminAccess devices should not be considered prepared for exclusivity checks.
		},
		{
			name:     "already-prepared-claim repeats",
			uid:      "gpu-being-prepared",
			claimUid: "claim-new",
			expected: false, // AdminAccess devices should not be considered prepared for exclusivity checks.
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			got := state.isDeviceUsedExclusivelyAlready(testcase.uid, "pool0", testcase.claimUid)

			if got != testcase.expected {
				t.Fatalf("expected IsDeviceUsedExclusivelyAlready()=%v, got %v", testcase.expected, got)
			}
		})
	}
}

func TestGetRequestDeviceClassNameFromClaim(t *testing.T) {
	claim := &resourcev1.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{Namespace: "namespace1", Name: "claim1"},
		Spec: resourcev1.ResourceClaimSpec{
			Devices: resourcev1.DeviceClaim{
				Requests: []resourcev1.DeviceRequest{
					{
						Name:    "exact-request",
						Exactly: &resourcev1.ExactDeviceRequest{DeviceClassName: device.DriverName, Count: 1},
					},
					{
						Name: "prioritized-request",
						FirstAvailable: []resourcev1.DeviceSubRequest{
							{Name: "vfio", DeviceClassName: device.VFIODeviceClassName, Count: 1},
							{Name: "drm", DeviceClassName: device.DriverName, Count: 1},
						},
					},
					{
						Name: "unknown-request-type",
					},
				},
			},
		},
	}

	testcases := []struct {
		name        string
		requestName string
		expected    string
	}{
		{
			name:        "exactly request",
			requestName: "exact-request",
			expected:    device.DriverName,
		},
		{
			name:        "firstAvailable request, first subrequest selected",
			requestName: "prioritized-request/vfio",
			expected:    device.VFIODeviceClassName,
		},
		{
			name:        "firstAvailable request, second subrequest selected",
			requestName: "prioritized-request/drm",
			expected:    device.DriverName,
		},
		{
			name:        "firstAvailable request without subrequest name",
			requestName: "prioritized-request",
			expected:    "",
		},
		{
			name:        "firstAvailable request with unknown subrequest name",
			requestName: "prioritized-request/nonexistent",
			expected:    "",
		},
		{
			name:        "exactly request with unexpected subrequest name",
			requestName: "exact-request/vfio",
			expected:    device.DriverName,
		},
		{
			name:        "request of unsupported type",
			requestName: "unknown-request-type",
			expected:    "",
		},
		{
			name:        "unknown request",
			requestName: "nonexistent-request",
			expected:    "",
		},
	}

	state := &nodeState{}
	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			got := state.getRequestDeviceClassNameFromClaim(testcase.requestName, claim)

			if got != testcase.expected {
				t.Errorf("expected device class %q, got %q", testcase.expected, got)
			}
		})
	}
}
