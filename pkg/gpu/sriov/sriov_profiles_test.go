/*
 * Copyright (c) 2023, Intel Corporation.  All Rights Reserved.
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

package sriov

import (
	"strings"
	"testing"
)

func TestAllProfilesArePresent(t *testing.T) {
	t.Log("All profiles are present with no typos")
	for deviceId, profileIds := range PerDeviceIdProfiles {
		for _, profileId := range profileIds {
			if _, exists := Profiles[profileId]; !exists {
				t.Errorf("profile %s for device %s does not exist", profileId, deviceId)
			}
		}
	}
}

func TestNoUnusedProfilesArePresent(t *testing.T) {
	t.Log("All present profiles except fairShare are used by some device")
	for profileId := range Profiles {
		if profileId == "fairShare" {
			continue
		}
		unused := true
		for _, deviceProfiles := range PerDeviceIdProfiles {
			for _, deviceProfile := range deviceProfiles {
				if profileId == deviceProfile {
					unused = false
				}
			}
		}
		if unused {
			t.Errorf("profile %s is unused", profileId)
		}
	}
}

func TestAllDefaultProfilesArePresent(t *testing.T) {
	t.Log("All default profiles are present with no typos")
	for deviceId, profileName := range PerDeviceIdDefaultProfiles {
		if _, exists := Profiles[profileName]; !exists {
			t.Errorf("default profile %s for device %s does not exist", profileName, deviceId)
		}
	}
}

func TestProfileSelection(t *testing.T) {
	t.Log("Profile selection works correctly")
	memory, profileMillicores, profileName, err := PickVFProfile("0x56c0", 13500, 0, true)
	if err != nil || profileName != "flex170_m1" || memory != 13542 || profileMillicores != 1000 {
		t.Errorf("unexpected response from PickVFProfile: %v / %v / %v ; expected %v / %v / %v",
			memory, profileName, err,
			13542, "flex170_m1", nil)
	}
}

func TestMaxFairVFs(t *testing.T) {
	type testcase struct {
		model              string
		vfsMemory          []int
		expectedVfsNumber  int
		expectedErrPattern string
	}
	testcases := []testcase{
		{model: "0x56c0", vfsMemory: []int{2048, 1024, 512, 3417}, expectedVfsNumber: 4, expectedErrPattern: ""},
		{model: "0x56c0", vfsMemory: []int{2048, 2048, 2048, 2048, 2048}, expectedVfsNumber: 5, expectedErrPattern: ""},
		{model: "0x56c0", vfsMemory: []int{0, 0, 0}, expectedVfsNumber: 3, expectedErrPattern: ""},
		{model: "0x56cC", vfsMemory: []int{256, 0, 0}, expectedVfsNumber: 0, expectedErrPattern: "no VF profiles for device 0x56cC"},
	}
	t.Log("Maximum fair-share VFs calculation works correctly")

	for _, testcase := range testcases {
		vfsNumber, err := MaxFairVFs(testcase.model, testcase.vfsMemory)

		if (err != nil && testcase.expectedErrPattern == "") ||
			(err != nil && !strings.Contains(err.Error(), testcase.expectedErrPattern)) ||
			(err == nil && testcase.expectedErrPattern != "") {
			t.Errorf("bad result, expected err pattern: %s, got: %s", testcase.expectedErrPattern, err)
		}

		if vfsNumber != testcase.expectedVfsNumber {
			t.Errorf("bad result, unexpected number of VFs: %v; expected %v", vfsNumber, testcase.expectedVfsNumber)
		}
	}
}

func TestSanitizeLmemQuotaMiB(t *testing.T) {
	t.Log("Maximum fair-share VFs calculation works correctly")

	ok := SanitizeLmemQuotaMiB("0x56c0", true, 1024)
	if !ok {
		t.Errorf("unexpected response from SanitizeLmemQuotaMiB: %v; expected true", ok)
	}

	ok = SanitizeLmemQuotaMiB("0x56c0", true, 48024)
	if ok {
		t.Errorf("unexpected response from SanitizeLmemQuotaMiB: %v; expected false", ok)
	}
}

func TestDeviceProfileExists(t *testing.T) {
	type testcase struct {
		model          string
		profile        string
		expectedResult bool
	}
	testcases := []testcase{
		{model: "0x56c0", profile: "flex170_m2", expectedResult: true},
		{model: "0x56c0", profile: "max1100_c2", expectedResult: false},
		{model: "0x56c0", profile: "fairShare", expectedResult: true},
		{model: "0x56cC", profile: "flex170_m2", expectedResult: false},
	}

	t.Log("Maximum fair-share VFs calculation works correctly")
	for _, testcase := range testcases {

		ok := DeviceProfileExists(testcase.model, testcase.profile)
		if ok != testcase.expectedResult {
			t.Errorf("unexpected response for %v / %v : %v. Expected %v",
				testcase.model, testcase.profile, ok, testcase.expectedResult)
		}
	}
}

func TestProfileMillicores(t *testing.T) {
	actualMillicores := profileMillicores("flex170_m2")
	if actualMillicores != 500 {
		t.Errorf("bad result: expected 500 millicores for flex140_m2 profile, got %v", actualMillicores)
	}
}
