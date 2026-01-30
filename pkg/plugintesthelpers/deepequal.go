/*
 * Copyright (c) 2026, Intel Corporation.  All Rights Reserved.
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

package plugintesthelpers

import (
	"reflect"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"

	helpers "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/helpers"
)

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
