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

package main

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/helpers"
)

// TestPreparedClaimsFiles checks prepared claims JSON read & write helpers.
// nolint:cyclop
func TestPreparedClaimsFiles(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "dra-test-*.json")
	if err != nil {
		t.Errorf("tmp file creation failed: %v", err)
	}
	preparedClaimsFile := tmpFile.Name()
	defer os.RemoveAll(preparedClaimsFile)

	missingPath := "non/existing/file"

	multiClaimV1 := helpers.ClaimPreparations{
		"uid1": {Devices: []kubeletplugin.Device{{Requests: []string{"request1"}, DeviceName: "0000-af-00-1-0xabcd", PoolName: "node1", CDIDeviceIDs: []string{"0000-af-00-1-0xabcd"}}}},
		"uid2": {Devices: []kubeletplugin.Device{{Requests: []string{"request1"}, DeviceName: "0000-af-00-2-0xabcd", PoolName: "node1", CDIDeviceIDs: []string{"0000-af-00-2-0xabcd"}}}},
		"uid3": {Devices: []kubeletplugin.Device{{Requests: []string{"request1"}, DeviceName: "0000-af-00-3-0xabcd", PoolName: "node1", CDIDeviceIDs: []string{"0000-af-00-3-0xabcd"}}}},
	}
	multiClaimV1bytes, err := json.Marshal(multiClaimV1)
	if err != nil {
		t.Errorf("prepared claims JSON encoding failed. Err: %v", err)
	}
	multiClaimV2 := ClaimPreparations{
		"uid1": ClaimPreparation{
			PreparedDevices: []PreparedDevice{
				{AdminAccess: false, KubeletpluginDevice: kubeletplugin.Device{Requests: []string{"request1"}, DeviceName: "0000-af-00-1-0xabcd", PoolName: "node1", CDIDeviceIDs: []string{"0000-af-00-1-0xabcd"}}},
			},
		},
		"uid2": ClaimPreparation{
			PreparedDevices: []PreparedDevice{
				{AdminAccess: false, KubeletpluginDevice: kubeletplugin.Device{Requests: []string{"request1"}, DeviceName: "0000-af-00-2-0xabcd", PoolName: "node1", CDIDeviceIDs: []string{"0000-af-00-2-0xabcd"}}},
			},
		},
		"uid3": ClaimPreparation{
			PreparedDevices: []PreparedDevice{
				{AdminAccess: false, KubeletpluginDevice: kubeletplugin.Device{Requests: []string{"request1"}, DeviceName: "0000-af-00-3-0xabcd", PoolName: "node1", CDIDeviceIDs: []string{"0000-af-00-3-0xabcd"}}},
			},
		},
	}
	multiClaimCheckpoint := PreparedClaimsCheckpoint{
		TypeMeta: metav1.TypeMeta{
			Kind:       CheckpointKind,
			APIVersion: CheckpointAPIVersion,
		},
		PreparedClaims: multiClaimV2,
	}
	multiClaimCheckpointbytes, err := json.Marshal(multiClaimCheckpoint)
	if err != nil {
		t.Errorf("prepared claims JSON encoding failed. Err: %v", err)
	}

	type testCase struct {
		name            string // testcase name
		claimsJSONbytes []byte // if not nil, test setup will write claimsJSONbytes into tmpClaim file
		expectedClaims  ClaimPreparations
		file            string // file to read/write claims from: tmpClaim or missingPath
		expectedError   string // part of expected error message
		readOrGet       string // "read" for "readPreparedClaimsFromFile" call, "get" for "GetOrCreatePreparedClaims" call
	}

	testcases := []testCase{
		{
			"read fail returns error", nil, nil, missingPath, "no such file or directory", "read",
		},
		{
			"getOrCreate write error", nil, nil, missingPath, "open non/existing/file", "get",
		},
		{
			"getOrCreate create success", []byte("{}"), ClaimPreparations{}, preparedClaimsFile, "", "read",
		},
		{
			"read invalid JSON returns error", []byte("'"), nil, preparedClaimsFile, "failed parsing ClaimPreparations data", "get",
		},
		{
			"empty write & read OK", []byte("{}"), ClaimPreparations{}, preparedClaimsFile, "", "get",
		},
		{
			"multi-claim v1 OK", multiClaimV1bytes, multiClaimV2, preparedClaimsFile, "", "get",
		},
		{
			"multi-claim v2 write & read OK", multiClaimCheckpointbytes, multiClaimV2, preparedClaimsFile, "", "read",
		},
	}

	for _, test := range testcases {
		t.Log(test.name)

		var claims ClaimPreparations
		var err error

		// when nil - do not prepare file, let it be missing for read tests
		if test.claimsJSONbytes != nil {
			if err := os.WriteFile(test.file, test.claimsJSONbytes, 0600); err != nil {
				t.Errorf("%v: unexpected error preparing test case: writing test claims to file. Err: %v", test.name, err)
				continue
			}
		}

		switch test.readOrGet {
		case "read":
			claims, err = readPreparedClaimsFromFile(test.file)
		case "get":
			claims, err = GetOrCreatePreparedClaims(test.file)
		default:
			t.Errorf("%v: invalid test setup: unknown readOrGet value: %v", test.name, test.readOrGet)
			continue
		}
		errorCheck(t, test.name+"- reading claims", test.expectedError, err)

		if test.expectedClaims != nil && !reflect.DeepEqual(claims, test.expectedClaims) {
			t.Errorf("%v: unexpected claims: %+v, expected: %+v", test.name, claims, test.expectedClaims)
			continue
		}

		if test.expectedClaims != nil {
			// read pre-existing JSON
			if err := WritePreparedClaimsToFile(test.file, claims); err != nil {
				t.Errorf("%v: unexpected final error writing claims to file. Err: %v", test.name, err)
			}
		}

	}
}
