//
// Copyright (C) 2026 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0
//

package main

import (
	"errors"
	"fmt"
	"testing"

	resourcev1 "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
	"k8s.io/dynamic-resource-allocation/resourceslice"
	"k8s.io/utils/ptr"
)

func TestValidateConsumableShares(t *testing.T) {
	for _, mode := range []string{"", "disabled", "memory", "unlimited", "1", "4"} {
		if err := validateConsumableShares(mode); err != nil {
			t.Errorf("mode %q: unexpected error: %v", mode, err)
		}
	}
	for _, mode := range []string{"0", "-1", "two", "Disabled"} {
		if err := validateConsumableShares(mode); err == nil {
			t.Errorf("mode %q: expected error", mode)
		}
	}
}

func TestApplyConsumableShares(t *testing.T) {
	testCases := []struct {
		name          string
		mode          string
		memoryValue   string
		shared        bool
		memoryDefault string // empty if no memory request policy is expected
		memoryMin     string
		shares        string // empty if no shares capacity is expected
	}{
		{name: "disabled", mode: "disabled", memoryValue: "16Gi"},
		{name: "unlimited", mode: "unlimited", memoryValue: "16Gi", shared: true, memoryDefault: "0", memoryMin: "0"},
		{name: "memory", mode: "memory", memoryValue: "16Gi", shared: true, memoryDefault: "16Gi", memoryMin: "1Mi"},
		{name: "4 shares", mode: "4", memoryValue: "16Gi", shared: true, memoryDefault: "0", memoryMin: "0", shares: "4"},
		// Memory mode has no other limit on sharing, so the device stays exclusive; the apiserver
		// rejects a memory policy whose step exceeds a 0 capacity, so the other modes set none.
		{name: "memory mode, undiscovered memory", mode: "memory", memoryValue: "0"},
		{name: "2 shares, undiscovered memory", mode: "2", memoryValue: "0", shared: true, shares: "2"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dev := resourcev1.Device{
				Name: "card0",
				Capacity: map[resourcev1.QualifiedName]resourcev1.DeviceCapacity{
					"memory":     {Value: resource.MustParse(tc.memoryValue)},
					"millicores": {Value: resource.MustParse("1000")},
				},
			}
			applyConsumableShares(&dev, tc.mode)

			if !tc.shared {
				if dev.AllowMultipleAllocations != nil || dev.Capacity["memory"].RequestPolicy != nil ||
					dev.Capacity["millicores"].RequestPolicy != nil || len(dev.Capacity) != 2 {
					t.Fatalf("expected device to be unchanged, got %+v", dev)
				}
				return
			}

			if dev.AllowMultipleAllocations == nil || !*dev.AllowMultipleAllocations {
				t.Fatal("expected AllowMultipleAllocations to be true")
			}

			millicores := dev.Capacity["millicores"].RequestPolicy
			if millicores == nil || !millicores.Default.IsZero() || millicores.ValidRange.Max.Cmp(resource.MustParse("1000")) != 0 {
				t.Errorf("unexpected millicores request policy: %+v", millicores)
			}

			memory := dev.Capacity["memory"].RequestPolicy
			if tc.memoryDefault == "" {
				if memory != nil {
					t.Errorf("expected no memory request policy for 0 memory, got %+v", memory)
				}
			} else {
				if memory == nil {
					t.Fatal("expected memory request policy")
				}
				expectQuantity(t, "memory default", *memory.Default, tc.memoryDefault)
				expectQuantity(t, "memory min", *memory.ValidRange.Min, tc.memoryMin)
				expectQuantity(t, "memory max", *memory.ValidRange.Max, tc.memoryValue)
				expectQuantity(t, "memory step", *memory.ValidRange.Step, "1Mi")
			}

			expectSharesCapacity(t, dev, tc.shares)
		})
	}
}

// expectSharesCapacity checks the device's "shares" capacity against want, the expected shares
// value, or "" if no shares capacity is expected.
func expectSharesCapacity(t *testing.T, dev resourcev1.Device, want string) {
	t.Helper()
	shares, found := dev.Capacity["shares"]
	if want == "" {
		if found {
			t.Errorf("expected no shares capacity, got %+v", shares)
		}
		return
	}
	if !found || shares.RequestPolicy == nil {
		t.Fatalf("expected shares capacity with request policy, got %+v", shares)
	}
	expectQuantity(t, "shares", shares.Value, want)
	expectQuantity(t, "shares default", *shares.RequestPolicy.Default, "1")
	expectQuantity(t, "shares min", *shares.RequestPolicy.ValidRange.Min, "1")
	expectQuantity(t, "shares max", *shares.RequestPolicy.ValidRange.Max, want)
	expectQuantity(t, "shares step", *shares.RequestPolicy.ValidRange.Step, "1")
}

func expectQuantity(t *testing.T, what string, got resource.Quantity, want string) {
	t.Helper()
	if got.Cmp(resource.MustParse(want)) != 0 {
		t.Errorf("%s: expected %v, got %v", what, want, got.String())
	}
}

func TestIsConsumableCapacityDisabledError(t *testing.T) {
	sliceWith := func(allowMultiple *bool) *resourcev1.ResourceSlice {
		return &resourcev1.ResourceSlice{Spec: resourcev1.ResourceSliceSpec{
			Devices: []resourcev1.Device{{Name: "gpu", AllowMultipleAllocations: allowMultiple}},
		}}
	}
	dropped := &resourceslice.DroppedFieldsError{DesiredSlice: sliceWith(ptr.To(true)), ActualSlice: sliceWith(nil)}
	if !isConsumableCapacityDisabledError(fmt.Errorf("wrapped: %w", dropped)) {
		t.Error("expected dropped AllowMultipleAllocations to be detected")
	}

	notDropped := &resourceslice.DroppedFieldsError{DesiredSlice: sliceWith(ptr.To(true)), ActualSlice: sliceWith(ptr.To(true))}
	for _, err := range []error{nil, errors.New("other error"), notDropped} {
		if isConsumableCapacityDisabledError(err) {
			t.Errorf("unexpected detection for error %v", err)
		}
	}
}

func TestDescribePreparedDevices(t *testing.T) {
	shareID := types.UID("share-1")
	devices := []PreparedDevice{
		{KubeletpluginDevice: kubeletplugin.Device{DeviceName: "gpu-a", ShareID: &shareID}},
		{KubeletpluginDevice: kubeletplugin.Device{DeviceName: "gpu-b"}},
		{KubeletpluginDevice: kubeletplugin.Device{DeviceName: "gpu-c"}, AdminAccess: true},
	}
	want := "gpu-a (share share-1), gpu-b (exclusive), gpu-c (adminAccess)"
	if got := describePreparedDevices(devices); got != want {
		t.Errorf("describePreparedDevices: got %q, want %q", got, want)
	}

	claim := ClaimPreparation{PreparedDevices: devices}
	if !claimHasShares(claim) {
		t.Error("expected claimHasShares to be true")
	}
	if claimHasShares(ClaimPreparation{PreparedDevices: devices[1:]}) {
		t.Error("expected claimHasShares to be false without a ShareID")
	}
	prepared := ClaimPreparations{"claim": claim}
	if !deviceHasSharedClaims(prepared, "gpu-a") || deviceHasSharedClaims(prepared, "gpu-b") {
		t.Error("deviceHasSharedClaims: expected true for gpu-a and false for gpu-b")
	}
}
