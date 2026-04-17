/* Copyright (C) 2024 Intel Corporation
 * SPDX-License-Identifier: Apache-2.0
 */

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"reflect"
	"testing"

	core "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/fakesysfs"
	helpers "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/helpers"
	testhelpers "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/plugintesthelpers"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/qat/device"
)

const (
	testNodeName  = "test-node-01"
	testNameSpace = "test-namespace-01"
)

func getFakeDriver(testDirs testhelpers.TestDirsType) (*driver, error) {
	config := &helpers.Config{
		CommonFlags: &helpers.Flags{
			NodeName:                  testNodeName,
			CdiRoot:                   testDirs.CdiRoot,
			KubeletPluginDir:          testDirs.KubeletPluginDir,
			KubeletPluginsRegistryDir: testDirs.KubeletPluginRegistryDir,
		},
		Coreclient:  kubefake.NewClientset(),
		DriverFlags: nil,
	}

	if err := os.MkdirAll(config.CommonFlags.KubeletPluginDir, 0755); err != nil {
		return nil, fmt.Errorf("failed creating fake driver plugin dir: %v", err)
	}
	if err := os.MkdirAll(config.CommonFlags.KubeletPluginsRegistryDir, 0755); err != nil {
		return nil, fmt.Errorf("failed creating fake driver plugin dir: %v", err)
	}

	os.Setenv("SYSFS_ROOT", testDirs.SysfsRoot)

	// kubelet-plugin will access node object, it needs to exist.
	newNode := &core.Node{ObjectMeta: metav1.ObjectMeta{Name: testNodeName}}
	if _, err := config.Coreclient.CoreV1().Nodes().Create(context.TODO(), newNode, metav1.CreateOptions{}); err != nil {
		return nil, fmt.Errorf("failed creating fake node object: %v", err)
	}

	helperdriver, err := newDriver(context.TODO(), config)
	if err != nil {
		return nil, fmt.Errorf("failed creating driver object: %v", err)
	}

	driver, ok := helperdriver.(*driver)
	if !ok {
		return nil, fmt.Errorf("type assertion failed: expected driver, got %T", driver)
	}
	return driver, err
}

//nolint:cyclop // test code
func TestPrepareUnprepareResourceClaims(t *testing.T) {
	type testCase struct {
		name                           string
		request                        []*resourcev1.ResourceClaim
		expectedResponse               map[types.UID]kubeletplugin.PrepareResult
		preparedClaims                 helpers.ClaimPreparations
		expectedPreparedClaims         helpers.ClaimPreparations
		unprepare                      []kubeletplugin.NamespacedObject
		expectedUnprepareErrors        map[types.UID]bool
		expectedPreparedAfterUnprepare helpers.ClaimPreparations
	}

	testcases := []testCase{
		{
			name: "one QAT success then unprepare",
			request: []*resourcev1.ResourceClaim{
				testhelpers.NewClaim(testNameSpace, "claim1", "uid1", "request1", "qat.intel.com", testNodeName, []string{"qatvf-0000-aa-00-1"}, false),
			},
			expectedResponse: map[types.UID]kubeletplugin.PrepareResult{
				"uid1": {
					Devices: []kubeletplugin.Device{
						{Requests: []string{"request1"}, PoolName: testNodeName, DeviceName: "qatvf-0000-aa-00-1", CDIDeviceIDs: []string{"intel.com/qat=qatvf-0000-aa-00-1", "intel.com/qat=qatvf-vfio"}},
					},
				},
			},
			preparedClaims: nil,
			expectedPreparedClaims: helpers.ClaimPreparations{
				"uid1": {
					Devices: []kubeletplugin.Device{
						{Requests: []string{"request1"}, PoolName: testNodeName, DeviceName: "qatvf-0000-aa-00-1", CDIDeviceIDs: []string{"intel.com/qat=qatvf-0000-aa-00-1", "intel.com/qat=qatvf-vfio"}},
					},
				},
			},
			unprepare:                      []kubeletplugin.NamespacedObject{{UID: "uid1"}},
			expectedUnprepareErrors:        map[types.UID]bool{},
			expectedPreparedAfterUnprepare: helpers.ClaimPreparations{},
		},
		{
			name: "single QAT already prepared (file) then unprepare unknown",
			request: []*resourcev1.ResourceClaim{
				testhelpers.NewClaim("namespace2", "claim2", "uid2", "request2", "qat.intel.com", "node1", []string{"qatvf-0000-aa-00-1"}, false),
			},
			expectedResponse: map[types.UID]kubeletplugin.PrepareResult{
				"uid2": {
					Devices: []kubeletplugin.Device{
						{Requests: []string{"request2"}, PoolName: "node1", DeviceName: "qatvf-0000-aa-00-1", CDIDeviceIDs: []string{"intel.com/qat=qatvf-0000-aa-00-1", "intel.com/qat=qatvf-vfio"}},
					},
				},
			},
			preparedClaims: helpers.ClaimPreparations{
				"uid2": {
					Devices: []kubeletplugin.Device{
						{Requests: []string{"request2"}, PoolName: "node1", DeviceName: "qatvf-0000-aa-00-1", CDIDeviceIDs: []string{"intel.com/qat=qatvf-0000-aa-00-1", "intel.com/qat=qatvf-vfio"}},
					},
				},
			},
			expectedPreparedClaims: helpers.ClaimPreparations{
				"uid2": {
					Devices: []kubeletplugin.Device{
						{Requests: []string{"request2"}, PoolName: "node1", DeviceName: "qatvf-0000-aa-00-1", CDIDeviceIDs: []string{"intel.com/qat=qatvf-0000-aa-00-1", "intel.com/qat=qatvf-vfio"}},
					},
				},
			},
			unprepare: []kubeletplugin.NamespacedObject{
				{UID: "uidX"},
			},
			expectedUnprepareErrors: map[types.UID]bool{
				"uidX": true,
			},
			expectedPreparedAfterUnprepare: helpers.ClaimPreparations{
				"uid2": {
					Devices: []kubeletplugin.Device{
						{Requests: []string{"request2"}, PoolName: "node1", DeviceName: "qatvf-0000-aa-00-1", CDIDeviceIDs: []string{"intel.com/qat=qatvf-0000-aa-00-1", "intel.com/qat=qatvf-vfio"}},
					},
				},
			},
		},
		{
			name: "single unavailable device (no prepare, unprepare noop)",
			request: []*resourcev1.ResourceClaim{
				testhelpers.NewClaim(testNameSpace, "claim3", "uid3", "request3", "qat.intel.com", testNodeName, []string{"qatvf-xxxx-xx-xx-x"}, false),
			},
			expectedResponse: map[types.UID]kubeletplugin.PrepareResult{
				"uid3": {Err: fmt.Errorf("error preparing devices for claim uid3: could not find allocatable device qatvf-xxxx-xx-xx-x (pool %v)", testNodeName)},
			},
			unprepare:                      []kubeletplugin.NamespacedObject{{UID: "uid3"}},
			expectedUnprepareErrors:        map[types.UID]bool{"uid3": true},
			expectedPreparedAfterUnprepare: helpers.ClaimPreparations{},
		},
	}

	for _, testcase := range testcases {
		t.Log(testcase.name)

		testDirs, err := testhelpers.NewTestDirs(device.DriverName)
		defer testhelpers.CleanupTest(t, testcase.name, testDirs.TestRoot)
		if err != nil {
			t.Errorf("%v: setup error: %v", testcase.name, err)
			return
		}

		fakeQATDevices := fakesysfs.QATDevices{
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

		// create fake sysfs for this test case under its own root before driver init
		if err := fakesysfs.FakeSysFsQATContents(testDirs.SysfsRoot, fakeQATDevices); err != nil {
			t.Errorf("setup error: could not create fake sysfs: %v", err)
			return
		}

		preparedClaimFilePath := path.Join(testDirs.KubeletPluginDir, "preparedClaims.json")
		if err := helpers.WritePreparedClaimsToFile(preparedClaimFilePath, testcase.preparedClaims); err != nil {
			t.Errorf("%v: error %v, writing prepared claims to file", testcase.name, err)
			continue
		}

		driver, driverErr := getFakeDriver(testDirs)
		if driverErr != nil {
			t.Errorf("could not create kubelet-plugin: %v\n", driverErr)
			continue
		}

		response, err := driver.PrepareResourceClaims(context.Background(), testcase.request)
		if err != nil {
			t.Errorf("%v: error %v, expected no error", testcase.name, err)
			continue
		}
		if !reflect.DeepEqual(testcase.expectedResponse, response) {
			responseJSON, _ := json.MarshalIndent(response, "", "\t")
			expectedResponseJSON, _ := json.MarshalIndent(testcase.expectedResponse, "", "\t")
			t.Errorf("%v: unexpected response: %+v, expected response: %v", testcase.name, string(responseJSON), string(expectedResponseJSON))
		}

		preparedClaims, err := helpers.ReadPreparedClaimsFromFile(preparedClaimFilePath)
		if err != nil {
			t.Errorf("%v: error %v, expected no error", testcase.name, err)
			continue
		}

		expectedPreparedClaims := testcase.expectedPreparedClaims
		if expectedPreparedClaims == nil {
			expectedPreparedClaims = helpers.ClaimPreparations{}
		}

		if !reflect.DeepEqual(expectedPreparedClaims, preparedClaims) {
			preparedClaimsJSON, _ := json.MarshalIndent(preparedClaims, "", "\t")
			expectedPreparedClaimsJSON, _ := json.MarshalIndent(testcase.expectedPreparedClaims, "", "\t")
			t.Errorf(
				"%v: unexpected PreparedClaims:\n%s\nexpected PreparedClaims:\n%s",
				testcase.name, string(preparedClaimsJSON), string(expectedPreparedClaimsJSON),
			)
		}

		unprepareResults, unprepareErr := driver.UnprepareResourceClaims(context.Background(), []kubeletplugin.NamespacedObject{{UID: "uid1"}})
		if unprepareErr != nil {
			t.Errorf("%v: UnprepareResourceClaims error %v, expected no error", testcase.name, unprepareErr)
		}

		for uid, uerr := range unprepareResults {
			expectErr := testcase.expectedUnprepareErrors[uid]
			if expectErr && uerr == nil {
				t.Errorf("%v: expected error for uid %s got nil", testcase.name, uid)
			}
			if !expectErr && uerr != nil {
				t.Errorf("%v: unexpected unprepare error for uid %s: %v", testcase.name, uid, uerr)
			}
		}

		afterClaims, err := helpers.ReadPreparedClaimsFromFile(preparedClaimFilePath)
		if err != nil {
			t.Errorf("%v: read prepared claims after unprepare error: %v", testcase.name, err)
		}
		expectedAfterUnprepare := testcase.expectedPreparedAfterUnprepare
		if expectedAfterUnprepare == nil {
			expectedAfterUnprepare = helpers.ClaimPreparations{}
		}
		if !reflect.DeepEqual(expectedAfterUnprepare, afterClaims) {
			gotJSON, _ := json.MarshalIndent(afterClaims, "", "\t")
			expJSON, _ := json.MarshalIndent(expectedAfterUnprepare, "", "\t")
			t.Errorf("%v: prepared claims mismatch after unprepare:\n%s\nexpected:\n%s", testcase.name, string(gotJSON), string(expJSON))
		}

		if err := driver.Shutdown(context.TODO()); err != nil {
			t.Errorf("Shutdown() error = %v, wantErr %v", err, nil)
		}
	}
}
