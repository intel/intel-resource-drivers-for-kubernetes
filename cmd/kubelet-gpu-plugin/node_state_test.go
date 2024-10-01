/*
 * Copyright (c) 2024, Intel Corporation.  All Rights Reserved.
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
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
)

func TestDeviceInfoDeepCopy(t *testing.T) {
	di := device.DeviceInfo{
		UID:        "f",
		Model:      "ff",
		CardIdx:    2,
		RenderdIdx: 3,
		MemoryMiB:  4,
		Millicores: 5,
		DeviceType: "fff",
		MaxVFs:     6,
		ParentUID:  "ffff",
		VFProfile:  "fffff",
		VFIndex:    7,
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
			t.Errorf("unexpected success on %s, expected: %s", name, substr)
		}
	case substr == "":
		t.Errorf("unexpected failure on %s: %v", name, err)
	case !strings.Contains(err.Error(), substr):
		t.Errorf("wrong %s error, expected '%s', got: %v", name, substr, err)
	default:
		t.Logf("=> expected error for %s: %v", name, substr)
	}
}

// TestPreparedClaimsFiles checks prepared claims JSON read & write helpers.
func TestPreparedClaimsFiles(t *testing.T) {
	type testOp struct {
		// if non-empty, op1 writes to given file, op2 reads file & compares against
		claims *ClaimPreparations
		file   string // otherwise, if non-empty, op1 reads, op2 writes to given file
		err    string // part of error message, if any
	}
	type testCase struct {
		name string
		op1  testOp
		op2  testOp
	}

	tmpFile, err := os.CreateTemp("", "dra-test-*.json")
	if err != nil {
		t.Errorf("tmp file creation failed: %v", err)
	}
	tmpClaim := tmpFile.Name()
	defer os.RemoveAll(tmpClaim)

	claimDir := "test-claims/"
	missingPath := "non/existing/file"

	multiClaim := ClaimPreparations{
		"uid1": {{RequestNames: []string{"request1"}, DeviceName: "0000-af-00-1-0xabcd", PoolName: "node1", CDIDeviceIDs: []string{"0000-af-00-1-0xabcd"}}},
		"uid2": {{RequestNames: []string{"request1"}, DeviceName: "0000-af-00-2-0xabcd", PoolName: "node1", CDIDeviceIDs: []string{"0000-af-00-2-0xabcd"}}},
		"uid3": {{RequestNames: []string{"request1"}, DeviceName: "0000-af-00-3-0xabcd", PoolName: "node1", CDIDeviceIDs: []string{"0000-af-00-3-0xabcd"}}},
	}

	testcases := []testCase{
		{
			"read fail returns error",
			testOp{
				nil, missingPath, "failed reading",
			},
			testOp{
				nil, "", "",
			},
		},
		{
			"write fail returns error",
			testOp{
				nil, "", "",
			},
			testOp{
				&ClaimPreparations{}, missingPath, "no such file",
			},
		},
		{
			"invalid JSON returns error",
			testOp{
				nil, claimDir + "invalid.json", "invalid character",
			},
			testOp{
				nil, "", "",
			},
		},
		{
			"empty JSON read OK",
			testOp{
				nil, claimDir + "empty.json", "",
			},
			testOp{
				&ClaimPreparations{}, "", "",
			},
		},
		{
			"empty write & read OK",
			testOp{
				&ClaimPreparations{}, tmpClaim, "",
			},
			testOp{
				&ClaimPreparations{}, tmpClaim, "",
			},
		},
		{
			"multi-claim JSON read OK",
			testOp{
				nil, claimDir + "multi.json", "",
			},
			testOp{
				&multiClaim, "", "",
			},
		},
		{
			"multi-claim write & read OK",
			testOp{
				&multiClaim, tmpClaim, "",
			},
			testOp{
				&multiClaim, tmpClaim, "",
			},
		},
	}

	var claims ClaimPreparations
	for _, test := range testcases {
		t.Log(test.name)

		content := true
		switch {
		case test.op1.claims != nil:
			// JSON write & read roundtrip
			if test.op1.file != test.op2.file {
				t.Errorf("=> different files for round-trip check: '%s' vs. '%s'", test.op1.file, test.op2.file)
			}
			err = writePreparedClaimsToFile(test.op1.file, *test.op1.claims)
			errorCheck(t, "writing claims", test.op1.err, err)
			claims, err = readPreparedClaimsFromFile(test.op2.file)
			errorCheck(t, "reading claims", test.op2.err, err)
		case test.op1.file != "":
			// read pre-existing JSON
			claims, err = readPreparedClaimsFromFile(test.op1.file)
			errorCheck(t, "reading claims", test.op1.err, err)
		default:
			content = false
		}

		if content && test.op2.claims != nil {
			if !reflect.DeepEqual(claims, *test.op2.claims) {
				t.Error("unexpected claims")
				for claimUID, claimDevices := range *test.op2.claims {
					t.Logf("expected %v:", claimUID)
					for _, device := range claimDevices {
						t.Logf("    %+v", *device)
					}
				}
				for claimUID, claimDevices := range claims {
					t.Logf("found %v:", claimUID)
					for _, device := range claimDevices {
						t.Logf("    %+v", *device)
					}
				}
			} else {
				t.Log("=> claims match (OK)")
			}
		}

		if test.op2.file == "" || !content {
			continue
		}

		err = writePreparedClaimsToFile(test.op2.file, claims)
		errorCheck(t, "writing claims", test.op2.err, err)

		// TODO: validate saved JSON against something?
	}
}
