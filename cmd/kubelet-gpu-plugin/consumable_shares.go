//
// Copyright (C) 2026 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0
//

package main

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	resourcev1 "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/dynamic-resource-allocation/resourceslice"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
)

// Values of the --consumable-shares flag, other than a positive number of shares per GPU.
const (
	// ConsumableSharesDisabled keeps each GPU allocatable to one claim only.
	ConsumableSharesDisabled = "disabled"
	// ConsumableSharesMemory lets claims share a GPU until its memory is used up. A claim that does
	// not request memory consumes all of it.
	ConsumableSharesMemory = "memory"
	// ConsumableSharesUnlimited lets any number of claims share a GPU.
	ConsumableSharesUnlimited = "unlimited"
)

// validateConsumableShares returns an error if the --consumable-shares flag value is not valid.
func validateConsumableShares(mode string) error {
	switch mode {
	case "", ConsumableSharesDisabled, ConsumableSharesMemory, ConsumableSharesUnlimited:
		return nil
	}
	if shares, err := strconv.Atoi(mode); err != nil || shares < 1 {
		return fmt.Errorf("invalid --consumable-shares value %q: must be '%s', '%s', '%s', or a positive integer",
			mode, ConsumableSharesDisabled, ConsumableSharesMemory, ConsumableSharesUnlimited)
	}
	return nil
}

// consumableSharesEnabled returns true if the --consumable-shares flag value enables sharing.
func consumableSharesEnabled(mode string) bool {
	return mode != "" && mode != ConsumableSharesDisabled
}

// isConsumableCapacityDisabledError returns true if the apiserver dropped the consumable capacity
// fields from the published ResourceSlice because the DRAConsumableCapacity feature gate is disabled.
func isConsumableCapacityDisabledError(err error) bool {
	var droppedFields *resourceslice.DroppedFieldsError
	return errors.As(err, &droppedFields) && slices.Contains(droppedFields.DisabledFeatures(), "DRAConsumableCapacity")
}

// claimHasShares returns true if any device of the prepared claim was allocated as a share.
func claimHasShares(claim ClaimPreparation) bool {
	for _, dev := range claim.PreparedDevices {
		if dev.KubeletpluginDevice.ShareID != nil {
			return true
		}
	}
	return false
}

// deviceHasSharedClaims returns true if any prepared claim holds a share of the device.
func deviceHasSharedClaims(prepared ClaimPreparations, deviceName string) bool {
	for _, claim := range prepared {
		for _, dev := range claim.PreparedDevices {
			if dev.KubeletpluginDevice.DeviceName == deviceName && dev.KubeletpluginDevice.ShareID != nil {
				return true
			}
		}
	}
	return false
}

// describePreparedDevices returns a one-line summary of the prepared devices for logging.
func describePreparedDevices(devices []PreparedDevice) string {
	parts := make([]string, 0, len(devices))
	for _, dev := range devices {
		switch {
		case dev.AdminAccess:
			parts = append(parts, dev.KubeletpluginDevice.DeviceName+" (adminAccess)")
		case dev.KubeletpluginDevice.ShareID != nil:
			parts = append(parts, fmt.Sprintf("%s (share %s)", dev.KubeletpluginDevice.DeviceName, *dev.KubeletpluginDevice.ShareID))
		default:
			parts = append(parts, dev.KubeletpluginDevice.DeviceName+" (exclusive)")
		}
	}
	return strings.Join(parts, ", ")
}

// exclusiveForUnknownMemory records the devices already reported as exclusive because their memory
// is unknown, so that applyConsumableShares logs that once. It is only accessed with the nodeState lock held.
var exclusiveForUnknownMemory = map[string]bool{}

// applyConsumableShares marks the device as allocatable to multiple claims, and sets how much of
// each capacity a claim consumes when it does not request it explicitly. The mode is expected to
// be validated with validateConsumableShares.
func applyConsumableShares(dev *resourcev1.Device, mode string) {
	if !consumableSharesEnabled(mode) {
		return
	}

	// In memory mode the memory capacity is the only limit on sharing. With unknown memory every
	// claim would consume 0 of 0 and any number of claims could share the GPU, so keep it exclusive.
	// GetResources runs on every publish, so log the reason once per device.
	if memory, found := dev.Capacity["memory"]; mode == ConsumableSharesMemory && (!found || memory.Value.IsZero()) {
		if !exclusiveForUnknownMemory[dev.Name] {
			exclusiveForUnknownMemory[dev.Name] = true
			klog.Errorf("Device %v: memory is unknown, so it cannot be shared in --consumable-shares=%s mode and is published as exclusive. "+
				"Enable privileged mode or health monitoring to discover memory, or use a different sharing mode.", dev.Name, mode)
		}
		return
	}
	delete(exclusiveForUnknownMemory, dev.Name)

	dev.AllowMultipleAllocations = ptr.To(true)

	// Every capacity of a shareable device is consumable, and a claim that does not request a
	// capacity consumes all of it by default. Default to 0 so that one claim does not block others.
	if millicores, found := dev.Capacity["millicores"]; found {
		millicores.RequestPolicy = &resourcev1.CapacityRequestPolicy{
			Default: ptr.To(resource.MustParse("0")),
			ValidRange: &resourcev1.CapacityRequestPolicyRange{
				Min: ptr.To(resource.MustParse("0")),
				Max: ptr.To(millicores.Value.DeepCopy()),
			},
		}
		dev.Capacity["millicores"] = millicores
	}

	// Memory is 0 when it is unknown (non-privileged mode without xpumd, VFIO-bound GPUs,
	// survivability mode). The apiserver rejects a request policy whose Min or Step exceeds the
	// capacity value, so such a device gets no memory policy: a claim that does not request memory
	// consumes all 0 of it, which does not block other claims.
	if memory, found := dev.Capacity["memory"]; found && !memory.Value.IsZero() {
		memDefault, memMin := resource.MustParse("0"), resource.MustParse("0")
		if mode == ConsumableSharesMemory {
			memDefault, memMin = memory.Value.DeepCopy(), resource.MustParse("1Mi")
		}
		memory.RequestPolicy = &resourcev1.CapacityRequestPolicy{
			Default: &memDefault,
			ValidRange: &resourcev1.CapacityRequestPolicyRange{
				Min:  &memMin,
				Max:  ptr.To(memory.Value.DeepCopy()),
				Step: ptr.To(resource.MustParse("1Mi")),
			},
		}
		dev.Capacity["memory"] = memory
	}

	// Only the numeric mode publishes a shares capacity.
	if shares, err := strconv.Atoi(mode); err == nil {
		one := resource.MustParse("1")
		sharesValue := *resource.NewQuantity(int64(shares), resource.DecimalSI)
		dev.Capacity["shares"] = resourcev1.DeviceCapacity{
			Value: sharesValue,
			RequestPolicy: &resourcev1.CapacityRequestPolicy{
				Default: ptr.To(one.DeepCopy()),
				ValidRange: &resourcev1.CapacityRequestPolicyRange{
					Min:  ptr.To(one.DeepCopy()),
					Max:  ptr.To(sharesValue.DeepCopy()),
					Step: ptr.To(one.DeepCopy()),
				},
			},
		}
	}
}
