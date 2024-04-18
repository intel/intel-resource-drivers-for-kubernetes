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

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/fakesysfs"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gaudi/device"
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
	}

	testcases := []testCase{
		{
			name: "one Gaudi success",
			request: &drav1.NodePrepareResourcesRequest{
				Claims: []*drav1.Claim{
					{
						Uid:                      "cuid1",
						StructuredResourceHandle: newStructuredHandle([]string{"0000-00-02-0-0x1020"}),
					},
				},
			},
			expectedResponse: &drav1.NodePrepareResourcesResponse{
				Claims: map[string]*drav1.NodePrepareResourceResponse{
					"cuid1": {CDIDevices: []string{"intel.com/gaudi=0000-00-02-0-0x1020"}},
				},
			},
			preparedClaims: nil,
			expectedPreparedClaims: ClaimPreparations{
				"cuid1": {
					{UID: "0000-00-02-0-0x1020", PCIAddress: "0000:00:02.0", Model: "0x1020", DeviceIdx: 0},
				},
			},
		},
		{
			name: "one Gaudi not found error",
			request: &drav1.NodePrepareResourcesRequest{
				Claims: []*drav1.Claim{
					{
						Uid:                      "cuid1",
						StructuredResourceHandle: newStructuredHandle([]string{"0000-00-06-0-0x1020"}),
					},
				},
			},
			expectedResponse: &drav1.NodePrepareResourcesResponse{
				Claims: map[string]*drav1.NodePrepareResourceResponse{
					"cuid1": {Error: "error preparing resource: allocated device 0000-00-06-0-0x1020 not found"},
				},
			},
			preparedClaims:         nil,
			expectedPreparedClaims: ClaimPreparations{},
		},
		{
			name: "one Gaudi already prepared claim success",
			request: &drav1.NodePrepareResourcesRequest{
				Claims: []*drav1.Claim{
					{
						Uid:                      "cuid1",
						StructuredResourceHandle: newStructuredHandle([]string{"0000-00-02-0-0x1020"}),
					},
				},
			},
			expectedResponse: &drav1.NodePrepareResourcesResponse{
				Claims: map[string]*drav1.NodePrepareResourceResponse{
					"cuid1": {CDIDevices: []string{"intel.com/gaudi=0000-00-02-0-0x1020"}},
				},
			},
			preparedClaims: ClaimPreparations{
				"cuid1": {
					{UID: "0000-00-02-0-0x1020", PCIAddress: "0000:00:02.0", Model: "0x1020", DeviceIdx: 0},
				},
			},
			expectedPreparedClaims: ClaimPreparations{
				"cuid1": {
					{UID: "0000-00-02-0-0x1020", PCIAddress: "0000:00:02.0", Model: "0x1020", DeviceIdx: 0},
				},
			},
		},
	}

	for _, testcase := range testcases {
		t.Log(testcase.name)

		testDirs, err := helpers.NewTestDirs()
		defer helpers.CleanupTest(t, testcase.name, testDirs.TestRoot)
		if err != nil {
			t.Errorf("%v: setup error: %v", testcase.name, err)
			return
		}

		if err := fakesysfs.FakeSysFsGaudiContents(
			testDirs.SysfsRoot,
			device.DevicesInfo{
				"0000-00-02-0-0x1020": {Model: "0x1020", DeviceIdx: 0, PCIAddress: "0000:00:02.0", UID: "0000-00-02-0-0x1020"},
				"0000-00-03-0-0x1020": {Model: "0x1020", DeviceIdx: 1, PCIAddress: "0000:00:03.0", UID: "0000-00-03-0-0x1020"},
				"0000-00-04-0-0x1020": {Model: "0x1020", DeviceIdx: 2, PCIAddress: "0000:00:04.0", UID: "0000-00-04-0-0x1020"},
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

	if err := fakesysfs.FakeSysFsGaudiContents(
		testDirs.SysfsRoot,
		device.DevicesInfo{
			"0000-00-02-0-0x1020": {Model: "0x1020", DeviceIdx: 0, PCIAddress: "0000:00:02.0", UID: "0000-00-02-0-0x1020"},
			"0000-00-03-0-0x1020": {Model: "0x1020", DeviceIdx: 1, PCIAddress: "0000:00:03.0", UID: "0000-00-03-0-0x1020"},
			"0000-00-04-0-0x1020": {Model: "0x1020", DeviceIdx: 2, PCIAddress: "0000:00:04.0", UID: "0000-00-04-0-0x1020"},
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

	if len(response.Resources) != 1 || len(response.Resources[0].NamedResources.Instances) != 3 {
		t.Errorf("unexpected amount of resources: %d, expected 3", len(response.Resources[0].NamedResources.Instances))
	}

	t.Logf("Response from driver: %+v", response.Resources)
}
