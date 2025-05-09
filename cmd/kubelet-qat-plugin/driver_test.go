/* Copyright (C) 2024 Intel Corporation
 * SPDX-License-Identifier: Apache-2.0
 */

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	resourcev1 "k8s.io/api/resource/v1beta1"
	"k8s.io/apimachinery/pkg/types"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/fakesysfs"
	helpers "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/plugintesthelpers"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/qat/device"
)

const (
	testNodeName  = "test-node-01"
	testNameSpace = "test-namespace-01"
)

func newFakeDriver(ctx context.Context) (*driver, error) {
	qatdevices, err := device.New()
	if err != nil {
		return nil, err
	}

	d := &driver{
		kubeclient: kubefake.NewSimpleClientset(),
		nodename:   testNodeName,
		devices:    qatdevices,
		statefile:  "",
	}

	return d, nil
}

func TestDriver(t *testing.T) {
	type testCase struct {
		name             string
		request          []*resourcev1.ResourceClaim
		expectedResponse map[types.UID]kubeletplugin.PrepareResult
	}

	setupdevices := fakesysfs.QATDevices{
		{Device: "0000:aa:00.0",
			State:    "up",
			Services: "sym;asym",
			TotalVFs: 3,
			NumVFs:   0,
		},
		{Device: "0000:bb:00.0",
			State:    "up",
			Services: "dc",
			TotalVFs: 3,
			NumVFs:   0,
		},
	}

	defer fakesysfs.FakeFsRemove()
	if err := fakesysfs.FakeSysFsQATContents(setupdevices); err != nil {
		t.Fatalf("err: %v", err)
	}

	driver, err := newFakeDriver(context.TODO())
	if err != nil {
		t.Fatalf("could not create qatdevices with New(): %v", err)
	}

	testcases := []testCase{
		{
			name: "QAT allocate device",
			request: []*resourcev1.ResourceClaim{
				helpers.NewClaim(testNameSpace, "claim1", "uid1", "request1", "qat.intel.com", testNodeName, []string{"qatvf-0000-aa-00-1"}),
			},
			expectedResponse: map[types.UID]kubeletplugin.PrepareResult{
				"uid1": {
					Devices: []kubeletplugin.Device{
						{Requests: []string{"request1"}, PoolName: testNodeName, DeviceName: "qatvf-0000-aa-00-1", CDIDeviceIDs: []string{"intel.com/qat=qatvf-0000-aa-00-1", "intel.com/qat=qatvf-vfio"}},
					},
				},
			},
		},
		{
			name: "QAT reallocate same device and same claim UID",
			request: []*resourcev1.ResourceClaim{
				helpers.NewClaim(testNameSpace, "claim-a", "uid1", "request1", "qat.intel.com", testNodeName, []string{"qatvf-0000-aa-00-1"}),
			},
			expectedResponse: map[types.UID]kubeletplugin.PrepareResult{
				"uid1": {
					Devices: []kubeletplugin.Device{
						{Requests: []string{"request1"}, PoolName: testNodeName, DeviceName: "qatvf-0000-aa-00-1", CDIDeviceIDs: []string{"intel.com/qat=qatvf-0000-aa-00-1", "intel.com/qat=qatvf-vfio"}},
					},
				},
			},
		},
		{
			name: "QAT device already allocated",
			request: []*resourcev1.ResourceClaim{
				helpers.NewClaim(testNameSpace, "claim2", "uid2", "request1", "qat.intel.com", testNodeName, []string{"qatvf-0000-aa-00-1"}),
			},
			expectedResponse: map[types.UID]kubeletplugin.PrepareResult{
				"uid2": {
					Err: fmt.Errorf("could not allocate device 'qatvf-0000-aa-00-1', service '' from any device"),
				},
			},
		},
		{
			name: "QAT two devices",
			request: []*resourcev1.ResourceClaim{
				helpers.NewClaim(testNameSpace, "claim3", "uid1", "request3", "qat.intel.com", testNodeName, []string{"qatvf-0000-aa-00-3", "qatvf-0000-bb-00-1"}),
			},
			expectedResponse: map[types.UID]kubeletplugin.PrepareResult{
				"uid1": {
					Devices: []kubeletplugin.Device{
						{Requests: []string{"request3"}, PoolName: testNodeName, DeviceName: "qatvf-0000-aa-00-3", CDIDeviceIDs: []string{"intel.com/qat=qatvf-0000-aa-00-3", "intel.com/qat=qatvf-vfio"}},
						{Requests: []string{"request3"}, PoolName: testNodeName, DeviceName: "qatvf-0000-bb-00-1", CDIDeviceIDs: []string{"intel.com/qat=qatvf-0000-bb-00-1", "intel.com/qat=qatvf-vfio"}},
					},
				},
			},
		},
	}

	for _, testcase := range testcases {
		t.Log(testcase.name)

		response, err := driver.PrepareResourceClaims(context.TODO(), testcase.request)
		if err != nil {
			t.Errorf("%v: error %v, expected no error", testcase.name, err)
			continue
		}

		if !reflect.DeepEqual(testcase.expectedResponse, response) {
			responseJSON, _ := json.MarshalIndent(response, "", "\t")
			expectedResponseJSON, _ := json.MarshalIndent(testcase.expectedResponse, "", "\t")
			t.Errorf("%v: unexpected response: %+v, expected response: %v", testcase.name, string(responseJSON), string(expectedResponseJSON))
		}
	}
}
