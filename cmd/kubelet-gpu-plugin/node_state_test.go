/*
 * Copyright (c) 2025, Intel Corporation.  All Rights Reserved.
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

	resourcev1 "k8s.io/api/resource/v1"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/helpers"
	testhelpers "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/plugintesthelpers"
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
// nolint:cyclop
func TestPreparedClaimsFiles(t *testing.T) {
	type testOp struct {
		// if non-empty, op1 writes to given file, op2 reads file & compares against
		claims helpers.ClaimPreparations
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

	multiClaim := helpers.ClaimPreparations{
		"uid1": {Devices: []kubeletplugin.Device{{Requests: []string{"request1"}, DeviceName: "0000-af-00-1-0xabcd", PoolName: "node1", CDIDeviceIDs: []string{"0000-af-00-1-0xabcd"}}}},
		"uid2": {Devices: []kubeletplugin.Device{{Requests: []string{"request1"}, DeviceName: "0000-af-00-2-0xabcd", PoolName: "node1", CDIDeviceIDs: []string{"0000-af-00-2-0xabcd"}}}},
		"uid3": {Devices: []kubeletplugin.Device{{Requests: []string{"request1"}, DeviceName: "0000-af-00-3-0xabcd", PoolName: "node1", CDIDeviceIDs: []string{"0000-af-00-3-0xabcd"}}}},
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
				helpers.ClaimPreparations{}, missingPath, "no such file",
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
				helpers.ClaimPreparations{}, "", "",
			},
		},
		{
			"empty write & read OK",
			testOp{
				helpers.ClaimPreparations{}, tmpClaim, "",
			},
			testOp{
				helpers.ClaimPreparations{}, tmpClaim, "",
			},
		},
		{
			"multi-claim JSON read OK",
			testOp{
				nil, claimDir + "multi.json", "",
			},
			testOp{
				multiClaim, "", "",
			},
		},
		{
			"multi-claim write & read OK",
			testOp{
				multiClaim, tmpClaim, "",
			},
			testOp{
				multiClaim, tmpClaim, "",
			},
		},
	}

	var claims helpers.ClaimPreparations
	for _, test := range testcases {
		t.Log(test.name)

		content := true
		switch {
		case test.op1.claims != nil:
			// JSON write & read roundtrip
			if test.op1.file != test.op2.file {
				t.Errorf("=> different files for round-trip check: '%s' vs. '%s'", test.op1.file, test.op2.file)
			}
			err = helpers.WritePreparedClaimsToFile(test.op1.file, test.op1.claims)
			errorCheck(t, "writing claims", test.op1.err, err)
			claims, err = helpers.ReadPreparedClaimsFromFile(test.op2.file)
			errorCheck(t, "reading claims", test.op2.err, err)
		case test.op1.file != "":
			// read pre-existing JSON
			claims, err = helpers.ReadPreparedClaimsFromFile(test.op1.file)
			errorCheck(t, "reading claims", test.op1.err, err)
		default:
			content = false
		}

		if content && test.op2.claims != nil {
			if !testhelpers.DeepEqualPreparedClaims(claims, test.op2.claims) {
				t.Error("unexpected claims")
				for claimUID, claimPreparation := range test.op2.claims {
					t.Logf("expected %v:", claimUID)
					for _, device := range claimPreparation.Devices {
						t.Logf("    %+v", device)
					}
				}
				for claimUID, claimPreparation := range claims {
					t.Logf("found %v:", claimUID)
					for _, device := range claimPreparation.Devices {
						t.Logf("    %+v", device)
					}
				}
			} else {
				t.Log("=> claims match (OK)")
			}
		}

		if test.op2.file == "" || !content {
			continue
		}

		err = helpers.WritePreparedClaimsToFile(test.op2.file, claims)
		errorCheck(t, "writing claims", test.op2.err, err)

		// TODO: validate saved JSON against something?
	}
}

func TestGetResourcesTaintsOnlyUnpreparedNonDRMBoundDevices(t *testing.T) {
	state := &nodeState{
		NodeState: &helpers.NodeState{
			Allocatable: map[string]*device.DeviceInfo{
				"gpu-unprepared": {
					UID:           "gpu-unprepared",
					PCIAddress:    "0000:00:01.0",
					Model:         "0x56c0",
					ModelName:     "Flex 170",
					FamilyName:    "Data Center Flex",
					MemoryMiB:     16384,
					Driver:        "xe",
					CurrentDriver: "vfio-pci",
					Health:        device.HealthHealthy,
				},
				"gpu-prepared": {
					UID:           "gpu-prepared",
					PCIAddress:    "0000:00:02.0",
					Model:         "0x56c1",
					ModelName:     "Flex 140",
					FamilyName:    "Data Center Flex",
					MemoryMiB:     16384,
					Driver:        "xe",
					CurrentDriver: "vfio-pci",
					Health:        device.HealthHealthy,
				},
			},
			Prepared: helpers.ClaimPreparations{
				"claim-1": {
					Devices: []kubeletplugin.Device{{
						DeviceName: "gpu-prepared",
					}},
				},
			},
			NodeName: "test-node",
		},
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

	if len(deviceByName["gpu-unprepared"].Taints) != 1 {
		t.Fatalf("expected gpu-unprepared to have 1 taint, got %d", len(deviceByName["gpu-unprepared"].Taints))
	}

	if got := deviceByName["gpu-unprepared"].Taints[0].Key; got != "NotDRMBound-vfio-pci" {
		t.Fatalf("unexpected taint key for gpu-unprepared: %s", got)
	}

	if len(deviceByName["gpu-prepared"].Taints) != 0 {
		t.Fatalf("expected gpu-prepared to have no taints, got %d", len(deviceByName["gpu-prepared"].Taints))
	}
}

func TestIsDevicePrepared(t *testing.T) {
	state := &nodeState{
		NodeState: &helpers.NodeState{
			Allocatable: map[string]*device.DeviceInfo{
				"gpu-prepared": {
					UID:        "gpu-prepared",
					PCIAddress: "0000:00:02.0",
				},
				"gpu-free": {
					UID:        "gpu-free",
					PCIAddress: "0000:00:03.0",
				},
			},
			Prepared: helpers.ClaimPreparations{
				"claim-1": {
					Devices: []kubeletplugin.Device{{
						DeviceName: "gpu-prepared",
					}},
				},
			},
		},
	}

	testcases := []struct {
		name        string
		uid         string
		expected    bool
		expectError bool
	}{
		{
			name:     "prepared device",
			uid:      "gpu-prepared",
			expected: true,
		},
		{
			name:     "unprepared device",
			uid:      "gpu-free",
			expected: false,
		},
		{
			name:     "unknown device",
			uid:      "gpu-unknown",
			expected: false,
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			got := state.IsDevicePrepared(testcase.uid)

			if got != testcase.expected {
				t.Fatalf("expected IsDevicePrepared()=%v, got %v", testcase.expected, got)
			}
		})
	}
}
