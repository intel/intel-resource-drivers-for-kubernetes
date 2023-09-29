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

package main

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestGetDefaultVFMemoryFromConfigMap(t *testing.T) {
	type funcArgs struct {
		filepath string
		deviceId string
		eccOn    bool
	}

	type testCase struct {
		name             string
		filecontents     []string
		arguments        funcArgs
		expectedResult   uint64
		expectedErrorStr string
	}

	testfileName := "/tmp/getDefaultVFMemoryFromConfigMap"

	testCases := []testCase{
		{
			name:             "unsupported deviceId, no model",
			filecontents:     []string{"blank"},
			arguments:        funcArgs{filepath: testfileName, deviceId: "0x1234", eccOn: true},
			expectedResult:   0,
			expectedErrorStr: "unsupported device 0x1234",
		},
		{
			name: "wrong format of file contents",
			filecontents: []string{
				"string1",
				"[]",
				"0",
				"",
				"{\"one\": \"two\"}",
				"{\"flex170\x00\": 8192}",
			},
			arguments:        funcArgs{filepath: testfileName, deviceId: "0x56c0", eccOn: true},
			expectedResult:   0,
			expectedErrorStr: fmt.Sprintf("failed parsing file %s. Err: ", testfileName),
		},
		{
			name: "misconfigured memory",
			filecontents: []string{
				"{\"flex170\":4}",
				"{\"flex170\":524288}",
			},
			arguments:        funcArgs{filepath: testfileName, deviceId: "0x56c0", eccOn: true},
			expectedResult:   0,
			expectedErrorStr: "misconfigured vf-memory config",
		},
		{
			name: "misconfigured memory",
			filecontents: []string{
				"{\"flex170\":4}",
				"{\"flex170\":524288}",
			},
			arguments:        funcArgs{filepath: testfileName, deviceId: "0x56c1", eccOn: true},
			expectedResult:   0,
			expectedErrorStr: "no data for model flex140 (device ID 0x56c1)",
		},
		{
			name: "ok",
			filecontents: []string{
				"{\"flex170\":4096}",
			},
			arguments:        funcArgs{filepath: testfileName, deviceId: "0x56c0", eccOn: true},
			expectedResult:   4096,
			expectedErrorStr: "",
		},
	}

	// loop through testcases
	for _, testcase := range testCases {

		// loop through file contents that should trigger same outcome
		for _, filecontents := range testcase.filecontents {

			writeTestFile(t, testcase.arguments.filepath, filecontents)

			resVal, resErr := getDefaultVFMemoryFromConfigMap(testcase.arguments.filepath, testcase.arguments.deviceId, testcase.arguments.eccOn)
			if resVal != testcase.expectedResult {
				t.Errorf("unexpected result: expected value: %v, got: %v", testcase.expectedResult, resVal)
			}
			if testcase.expectedErrorStr != "" {
				if resErr == nil {
					t.Errorf("unexpected result: expected error: %v, got no error", testcase.expectedErrorStr)
				} else if !strings.HasPrefix(resErr.Error(), testcase.expectedErrorStr) {
					t.Errorf("unexpected result: expected error: %v, got: %v", testcase.expectedErrorStr, resErr)
				}
			} else {
				if resErr != nil {
					t.Errorf("unexpected result: expected no error, got: %v", resErr)
				}
			}

		}
	}

	// final test setup: EPERM
	if err := os.Chmod(testfileName, 0000); err != nil {
		t.Errorf("could not set permissions for file %v: %v", testfileName, err)
	}

	// final test
	resVal, resErr := getDefaultVFMemoryFromConfigMap(testfileName, "0x56c0", true)
	expectedErrorStr := fmt.Sprintf("failed reading file %v. Err: ", testfileName)
	if resVal != 0 || resErr == nil || !strings.HasPrefix(resErr.Error(), expectedErrorStr) {
		t.Errorf("unexpected result: expected error: %v, got: %v", expectedErrorStr, resErr)
	}

	// cleanup after all tests
	if err := os.Remove(testfileName); err != nil {
		t.Errorf("failed cleaning up temp file %v after test: %v", testfileName, err)
	}
}

func TestGetGpuVFDefaults(t *testing.T) {
	type response struct {
		memory      uint64
		profileName string
		errorStr    string
	}

	type testCase struct {
		name             string
		deviceId         string
		eccOn            bool
		expectedResponse response
	}

	testcases := []testCase{
		{
			name:             "OK",
			deviceId:         "0x56c0",
			eccOn:            false,
			expectedResponse: response{memory: 2016, profileName: "flex170_m8", errorStr: ""},
		},
		{
			name:             "Unsupported device ID",
			deviceId:         "0x1234",
			eccOn:            false,
			expectedResponse: response{memory: 0, profileName: "", errorStr: "could not get default profile VF memory: unsupported device 0x1234"},
		},
	}

	for _, testcase := range testcases {
		vfMem, vfProfileName, err := getGpuVFDefaults(testcase.deviceId, testcase.eccOn)
		if vfMem != testcase.expectedResponse.memory {
			t.Errorf("unexpected response: wrong memory amount: %v, expected %v", vfMem, testcase.expectedResponse.memory)
		}

		if vfProfileName != testcase.expectedResponse.profileName {
			t.Errorf("unexpected response: wrong profile name: %v, expected %v", vfProfileName, testcase.expectedResponse.profileName)
		}

		if err != nil {
			if testcase.expectedResponse.errorStr == "" {
				t.Errorf("unexpected response: expected no error, got: %v", err)
			}
			if !strings.HasPrefix(err.Error(), testcase.expectedResponse.errorStr) {
				t.Errorf("unexpected response: wrong error: %v, expected %v", err, testcase.expectedResponse.errorStr)
			}
		} else if testcase.expectedResponse.errorStr != "" {
			t.Errorf("unexpected response: expected error: %v, got no error", testcase.expectedResponse.errorStr)
		}

	}
}
