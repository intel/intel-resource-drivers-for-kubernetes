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
	"context"
	"encoding/json"
	"fmt"
	"path"
	"reflect"
	"testing"
	"time"

	"google.golang.org/grpc/metadata"
	resourcev1 "k8s.io/api/resource/v1alpha2"
	drav1 "k8s.io/kubelet/pkg/apis/dra/v1alpha3"

	"github.com/fsnotify/fsnotify"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/fakesysfs"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
	helpers "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/plugintesthelpers"
)

func newStructuredHandle(deviceUIDs []string) []*resourcev1.StructuredResourceHandle {

	results := make([]resourcev1.DriverAllocationResult, len(deviceUIDs))
	for deviceIdx, deviceUID := range deviceUIDs {
		newResult := resourcev1.DriverAllocationResult{
			AllocationResultModel: resourcev1.AllocationResultModel{
				NamedResources: &resourcev1.NamedResourcesAllocationResult{
					Name: deviceUID,
				},
			},
		}
		results[deviceIdx] = newResult
	}

	handles := []*resourcev1.StructuredResourceHandle{
		{
			Results: results,
		},
	}

	return handles
}

func TestNodePrepareStructuredResources(t *testing.T) {
	type testCase struct {
		name                   string
		request                *drav1.NodePrepareResourcesRequest
		expectedResponse       *drav1.NodePrepareResourcesResponse
		preparedClaims         ClaimPreparations
		expectedPreparedClaims ClaimPreparations
		updateFakeSysfs        bool
	}

	testcases := []testCase{
		{
			name: "one gpu success",
			request: &drav1.NodePrepareResourcesRequest{
				Claims: []*drav1.Claim{
					{
						Uid:                      "cuid1",
						StructuredResourceHandle: newStructuredHandle([]string{"0000-00-02-0-0x56c0"}),
					},
				},
			},
			expectedResponse: &drav1.NodePrepareResourcesResponse{
				Claims: map[string]*drav1.NodePrepareResourceResponse{
					"cuid1": {CDIDevices: []string{"intel.com/gpu=0000-00-02-0-0x56c0"}},
				},
			},
			preparedClaims: nil,
			expectedPreparedClaims: ClaimPreparations{
				"cuid1": {
					{UID: "0000-00-02-0-0x56c0", PCIAddress: "0000:00:02.0", Model: "0x56c0", CardIdx: 0, RenderdIdx: 128, MemoryMiB: 16256, Millicores: 1000, DeviceType: "gpu", MaxVFs: 16},
				},
			},
			updateFakeSysfs: false,
		},
		{
			name: "one gpu not found error",
			request: &drav1.NodePrepareResourcesRequest{
				Claims: []*drav1.Claim{
					{
						Uid:                      "cuid1",
						StructuredResourceHandle: newStructuredHandle([]string{"0000-00-06-0-0x56c0"}),
					},
				},
			},
			expectedResponse: &drav1.NodePrepareResourcesResponse{
				Claims: map[string]*drav1.NodePrepareResourceResponse{
					"cuid1": {Error: "error preparing resource: allocated device 0000-00-06-0-0x56c0 not found"},
				},
			},
			preparedClaims:         nil,
			expectedPreparedClaims: ClaimPreparations{},
			updateFakeSysfs:        false,
		},
		{
			name: "one gpu already prepared claim success",
			request: &drav1.NodePrepareResourcesRequest{
				Claims: []*drav1.Claim{
					{
						Uid:                      "cuid1",
						StructuredResourceHandle: newStructuredHandle([]string{"0000-00-02-0-0x56c0"}),
					},
				},
			},
			expectedResponse: &drav1.NodePrepareResourcesResponse{
				Claims: map[string]*drav1.NodePrepareResourceResponse{
					"cuid1": {CDIDevices: []string{"intel.com/gpu=0000-00-02-0-0x56c0"}},
				},
			},
			preparedClaims: ClaimPreparations{
				"cuid1": {
					{UID: "0000-00-02-0-0x56c0", PCIAddress: "0000:00:02.0", Model: "0x56c0", CardIdx: 0, RenderdIdx: 128, MemoryMiB: 16256, Millicores: 1000, DeviceType: "gpu", MaxVFs: 16},
				},
			},
			expectedPreparedClaims: ClaimPreparations{
				"cuid1": {
					{UID: "0000-00-02-0-0x56c0", PCIAddress: "0000:00:02.0", Model: "0x56c0", CardIdx: 0, RenderdIdx: 128, MemoryMiB: 16256, Millicores: 1000, DeviceType: "gpu", MaxVFs: 16},
				},
			},
			updateFakeSysfs: false,
		},
	}

	var watcher *fsnotify.Watcher
	for _, testcase := range testcases {
		t.Log(testcase.name)

		testDirs, err := helpers.NewTestDirs()
		defer helpers.CleanupTest(t, testcase.name, testDirs.TestRoot)
		if err != nil {
			t.Errorf("%v: setup error: %v", testcase.name, err)
			return
		}

		if err := fakesysfs.FakeSysFsGpuContents(
			t,
			testDirs.SysfsRoot,
			device.DevicesInfo{
				"0000-00-02-0-0x56c0": {Model: "0x56c0", MemoryMiB: 16256, DeviceType: "gpu", CardIdx: 0, RenderdIdx: 128, UID: "0000-00-02-0-0x56c0", MaxVFs: 16},
				"0000-00-03-0-0x56c0": {Model: "0x56c0", MemoryMiB: 16256, DeviceType: "gpu", CardIdx: 1, RenderdIdx: 129, UID: "0000-00-03-0-0x56c0", MaxVFs: 16},
				"0000-00-03-1-0x56c0": {Model: "0x56c0", MemoryMiB: 8064, DeviceType: "vf", CardIdx: 2, RenderdIdx: 130, UID: "0000-00-03-1-0x56c0", VFIndex: 0, VFProfile: "flex170_m2", ParentUID: "0000-00-03-0-0x56c0"},
				// dummy, no SR-IOV tiles
				"0000-00-04-0-0x0000": {Model: "0x0000", MemoryMiB: 14248, DeviceType: "gpu", CardIdx: 3, RenderdIdx: 131, UID: "0000-00-04-0-0x0000", MaxVFs: 16},
			},
		); err != nil {
			t.Errorf("setup error: could not create fake sysfs: %v", err)
			return
		}

		preparedClaimFilePath := path.Join(testDirs.DriverPluginRoot, "preparedClaims.json")
		if err := writePreparedClaimsToFile(path.Join(testDirs.DriverPluginRoot, "preparedClaims.json"), testcase.preparedClaims); err != nil {
			t.Errorf("%v: error %v, writing prepared claims to file", testcase.name, err)
		}

		driver, driverErr := getFakeDriver(testDirs)
		if driverErr != nil {
			t.Errorf("could not create kubelet-plugin: %v\n", driverErr)
		}

		// dynamically add and remove fake sysfs SR-IOV VFs
		if testcase.updateFakeSysfs {
			watcher = fakesysfs.WatchNumvfs(t, testDirs.SysfsRoot)
			defer watcher.Close()
		}

		response, err := driver.NodePrepareResources(context.TODO(), testcase.request)
		if err != nil {
			t.Errorf("%v: error %v, expected no error", testcase.name, err)
		}

		preparedClaims, err := readPreparedClaimsFromFile(preparedClaimFilePath)
		if err != nil {
			t.Errorf("%v: error %v, expected no error", testcase.name, err)
		}

		if !compareNodePrepareResourcesResponses(testcase.expectedResponse, response) {
			t.Errorf("%v: unexpected response: %+v, expected response: %v", testcase.name, response, testcase.expectedResponse)
		}

		if !reflect.DeepEqual(testcase.expectedPreparedClaims, preparedClaims) {
			preparedClaimsJSON, _ := json.MarshalIndent(preparedClaims, "", "\t")
			expectedPreparedClaimsJSON, _ := json.MarshalIndent(testcase.expectedPreparedClaims, "", "\t")
			t.Errorf(
				"%v: unexpected PreparedClaims:\n%s\nexpected PreparedClaims:\n%s",
				testcase.name, preparedClaimsJSON, expectedPreparedClaimsJSON,
			)
		}
	}
}

// Fake transport implementation, based on channel instead of GRPC.
func newFakeTransport(transport chan *drav1.NodeListAndWatchResourcesResponse) drav1.Node_NodeListAndWatchResourcesServer {
	server := fakeNodeListAndWatchResourcesServer{}
	server.transport = transport
	return drav1.Node_NodeListAndWatchResourcesServer(&server)
}

type fakeNodeListAndWatchResourcesServer struct {
	transport chan *drav1.NodeListAndWatchResourcesResponse
}

func (x *fakeNodeListAndWatchResourcesServer) Send(m *drav1.NodeListAndWatchResourcesResponse) error {
	select {
	case x.transport <- m:
	case <-time.After(5 * time.Second):
	}
	return nil
}
func (x *fakeNodeListAndWatchResourcesServer) SetHeader(metadata.MD) error  { return nil }
func (x *fakeNodeListAndWatchResourcesServer) SendHeader(metadata.MD) error { return nil }
func (x *fakeNodeListAndWatchResourcesServer) SetTrailer(metadata.MD)       {}
func (x *fakeNodeListAndWatchResourcesServer) Context() context.Context     { return context.TODO() }
func (x *fakeNodeListAndWatchResourcesServer) SendMsg(m any) error          { return nil }
func (x *fakeNodeListAndWatchResourcesServer) RecvMsg(m any) error          { return nil }

var _ drav1.Node_NodeListAndWatchResourcesServer = (*fakeNodeListAndWatchResourcesServer)(nil)

func TestNodeListAndWatchResources(t *testing.T) {
	testcaseName := "TestNodeListAndWatchResources"
	testDirs, err := helpers.NewTestDirs()
	defer helpers.CleanupTest(t, testcaseName, testDirs.TestRoot)
	if err != nil {
		t.Errorf("%v: setup error: %v", testcaseName, err)
		return
	}

	if err := fakesysfs.FakeSysFsGpuContents(
		t,
		testDirs.SysfsRoot,
		device.DevicesInfo{
			"0000-00-02-0-0x56c0": {Model: "0x56c0", MemoryMiB: 16256, DeviceType: "gpu", CardIdx: 0, RenderdIdx: 128, UID: "0000-00-02-0-0x56c0", MaxVFs: 16},
			"0000-00-03-0-0x56c0": {Model: "0x56c0", MemoryMiB: 16256, DeviceType: "gpu", CardIdx: 1, RenderdIdx: 129, UID: "0000-00-03-0-0x56c0", MaxVFs: 16},
			"0000-00-03-1-0x56c0": {Model: "0x56c0", MemoryMiB: 8064, DeviceType: "vf", CardIdx: 2, RenderdIdx: 130, UID: "0000-00-03-1-0x56c0", VFIndex: 0, VFProfile: "flex170_m2", ParentUID: "0000-00-03-0-0x56c0"},
			// dummy, no SR-IOV tiles
			"0000-00-04-0-0x0000": {Model: "0x0000", MemoryMiB: 14248, DeviceType: "gpu", CardIdx: 3, RenderdIdx: 131, UID: "0000-00-04-0-0x0000", MaxVFs: 16},
		},
	); err != nil {
		t.Errorf("setup error: could not create fake sysfs: %v", err)
		return
	}

	transportChannel := make(chan *drav1.NodeListAndWatchResourcesResponse)
	streamer := newFakeTransport(transportChannel)

	driver, driverErr := getFakeDriver(testDirs)
	if driverErr != nil {
		t.Errorf("could not create kubelet-plugin: %v", driverErr)
		return
	}

	var response *drav1.NodeListAndWatchResourcesResponse
	go func() {
		if err := driver.NodeListAndWatchResources(&drav1.NodeListAndWatchResourcesRequest{}, streamer); err != nil {
			fmt.Printf("Error sending resource list")
		}
	}()
	select {
	case response = <-transportChannel:
		t.Logf("received response, number of NamedResource objects: %v", len(response.Resources[0].NamedResources.Instances))
	case <-time.After(5 * time.Second):
		t.Error("unepected timeout waiting for NodeListAndWatchResourcesRequest")
		return
	}

	if len(response.Resources) != 1 || len(response.Resources[0].NamedResources.Instances) != 4 {
		t.Errorf("unexpected amount of resources: %d, expected 4", len(response.Resources[0].NamedResources.Instances))
	}

	t.Logf("Response from driver: %+v", response.Resources)
}
