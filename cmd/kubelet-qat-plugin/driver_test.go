/* Copyright (C) 2024 Intel Corporation
 * SPDX-License-Identifier: Apache-2.0
 */

package main

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	resourcev1 "k8s.io/api/resource/v1alpha3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
	drav1 "k8s.io/kubelet/pkg/apis/dra/v1alpha4"

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
		claims           []*resourcev1.ResourceClaim
		request          *drav1.NodePrepareResourcesRequest
		expectedResponse *drav1.NodePrepareResourcesResponse
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

	defer fakesysfs.FakeSysFsRemove()
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
			claims: []*resourcev1.ResourceClaim{
				helpers.NewClaim(testNameSpace, "claim1", "uid1", "request1", "qat.intel.com", testNodeName, []string{"qatvf-0000-aa-00-1"}),
			},
			request: &drav1.NodePrepareResourcesRequest{
				Claims: []*drav1.Claim{{UID: "uid1", Name: "claim1", Namespace: testNameSpace}},
			},
			expectedResponse: &drav1.NodePrepareResourcesResponse{
				Claims: map[string]*drav1.NodePrepareResourceResponse{
					"uid1": {Devices: []*drav1.Device{
						{RequestNames: []string{"request1"}, PoolName: testNodeName, DeviceName: "qatvf-0000-aa-00-1", CDIDeviceIDs: []string{"intel.com/qat=qatvf-0000-aa-00-1", "intel.com/qat=qatvf-vfio"}}}},
				},
			},
		},
		{
			name: "QAT device already allocated",
			claims: []*resourcev1.ResourceClaim{
				helpers.NewClaim(testNameSpace, "claim2", "uid2", "request1", "qat.intel.com", testNodeName, []string{"qatvf-0000-aa-00-1"}),
			},
			request: &drav1.NodePrepareResourcesRequest{
				Claims: []*drav1.Claim{{UID: "uid2", Name: "claim2", Namespace: testNameSpace}},
			},
			expectedResponse: &drav1.NodePrepareResourcesResponse{
				Claims: map[string]*drav1.NodePrepareResourceResponse{
					"uid2": {Error: "could not allocate device 'qatvf-0000-aa-00-1', service '' from any device"},
				},
			},
		},
		{
			name: "QAT two devices",
			claims: []*resourcev1.ResourceClaim{
				helpers.NewClaim(testNameSpace, "claim3", "uid1", "request3", "qat.intel.com", testNodeName, []string{"qatvf-0000-aa-00-3", "qatvf-0000-bb-00-1"}),
			},
			request: &drav1.NodePrepareResourcesRequest{
				Claims: []*drav1.Claim{{UID: "uid1", Name: "claim3", Namespace: testNameSpace}},
			},
			expectedResponse: &drav1.NodePrepareResourcesResponse{
				Claims: map[string]*drav1.NodePrepareResourceResponse{
					"uid1": {Devices: []*drav1.Device{
						{RequestNames: []string{"request3"}, PoolName: testNodeName, DeviceName: "qatvf-0000-aa-00-3", CDIDeviceIDs: []string{"intel.com/qat=qatvf-0000-aa-00-3", "intel.com/qat=qatvf-vfio"}},
						{RequestNames: []string{"request3"}, PoolName: testNodeName, DeviceName: "qatvf-0000-bb-00-1", CDIDeviceIDs: []string{"intel.com/qat=qatvf-0000-bb-00-1", "intel.com/qat=qatvf-vfio"}}}},
				},
			},
		},
	}

	for _, testcase := range testcases {
		t.Log(testcase.name)

		for _, testClaim := range testcase.claims {
			createdClaim, err := driver.kubeclient.ResourceV1alpha3().ResourceClaims(testClaim.Namespace).Create(context.TODO(), testClaim, metav1.CreateOptions{})
			if err != nil {
				t.Errorf("could not create test claim: %v", err)
				continue
			}
			t.Logf("created test claim: %+v", createdClaim)
		}

		response, err := driver.NodePrepareResources(context.TODO(), testcase.request)
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
