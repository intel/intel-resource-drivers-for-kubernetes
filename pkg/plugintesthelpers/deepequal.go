//
// Copyright (C) 2026 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0
//

package plugintesthelpers

import (
	"reflect"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"

	helpers "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/helpers"
)

// DeepEqualPreparedClaims compares two ClaimPreparations to match.
// It is built on top of reflect.DeepEqual, but compares error fields
// by their error message only.
func DeepEqualPreparedClaims(a, b helpers.ClaimPreparations) bool {

	if len(a) != len(b) {
		return false
	}

	for uid, claimA := range a {
		claimB, exists := b[uid]
		if !exists {
			return false
		}

		if !reflect.DeepEqual(claimA.Devices, claimB.Devices) {
			return false
		}

		if claimA.Err == nil && claimB.Err != nil || claimA.Err != nil && claimB.Err == nil {
			return false
		}

		if claimA.Err != nil && claimB.Err != nil && claimA.Err.Error() != claimB.Err.Error() {
			return false
		}
	}

	return true

}

// DeepEqualErrorMap compares two error maps to match.
// It exists because reflect.DeepEqual cannot be used easily in testing
// where an error only needs the message string to be compared.
func DeepEqualErrorMap(a, b map[types.UID]error) bool {
	if len(a) != len(b) {
		return false
	}

	for uid, errA := range a {
		errB, exists := b[uid]
		if !exists {
			return false
		}

		if errA == nil && errB != nil || errA != nil && errB == nil {
			return false
		}

		if errA != nil && errB != nil && errA.Error() != errB.Error() {
			return false
		}
	}

	return true
}

// DeepEqualPrepareResults compares two DeepEqualPrepareResults to match.
// It is built on top of reflect.DeepEqual, but compares error fields
// by their error message only.
func DeepEqualPrepareResults(a, b map[types.UID]kubeletplugin.PrepareResult) bool {
	if len(a) != len(b) {
		return false
	}

	for uid, resultA := range a {
		resultB, exists := b[uid]
		if !exists {
			return false
		}

		if !reflect.DeepEqual(resultA.Devices, resultB.Devices) {
			return false
		}

		if resultA.Err == nil && resultB.Err != nil || resultA.Err != nil && resultB.Err == nil {
			return false
		}

		if resultA.Err != nil && resultB.Err != nil && resultA.Err.Error() != resultB.Err.Error() {
			return false
		}
	}

	return true
}
