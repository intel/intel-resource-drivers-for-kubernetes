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

package plugintesthelpers

import (
	"fmt"
	"os"
	"path"
	"testing"

	resourcev1 "k8s.io/api/resource/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	testRootPrefix = "test-*"
)

type TestDirsType struct {
	TestRoot                 string
	CdiRoot                  string
	KubeletPluginDir         string
	KubeletPluginRegistryDir string
	SysfsRoot                string
	DevfsRoot                string
}

// NewTestDirs creates fake CDI root, sysfs, driverPlugin dirs and returns
// them as a testDirsType or an error.
func NewTestDirs(driverName string) (TestDirsType, error) {
	testRoot, err := os.MkdirTemp("", testRootPrefix)
	if err != nil {
		return TestDirsType{}, fmt.Errorf("failed creating test root dir: %v", err)
	}

	if err := os.Chmod(testRoot, 0755); err != nil {
		return TestDirsType{}, fmt.Errorf("failed changing permissions to test root dir: %v", err)
	}
	return NewTestDirsAt(testRoot, driverName)
}
func NewTestDirsAt(testRoot string, driverName string) (TestDirsType, error) {
	cdiRoot := path.Join(testRoot, "cdi")
	if err := os.MkdirAll(cdiRoot, 0755); err != nil {
		return TestDirsType{}, fmt.Errorf("failed creating fake CDI root dir: %v", err)
	}

	fakeSysfsRoot := path.Join(testRoot, "sysfs")
	if err := os.MkdirAll(fakeSysfsRoot, 0755); err != nil {
		return TestDirsType{}, fmt.Errorf("failed creating fake sysfs root dir: %v", err)
	}

	driverPluginRoot := path.Join(testRoot, "kubelet-plugin/plugins/", driverName)
	if err := os.MkdirAll(driverPluginRoot, 0755); err != nil {
		return TestDirsType{}, fmt.Errorf("failed creating fake driver plugin dir: %v", err)
	}

	driverRegistrarRoot := path.Join(testRoot, "kubelet-plugin/plugins_registry")
	if err := os.MkdirAll(driverRegistrarRoot, 0755); err != nil {
		return TestDirsType{}, fmt.Errorf("failed creating fake driver plugin dir: %v", err)
	}

	devfsRoot := path.Join(testRoot, "dev")
	if err := os.MkdirAll(devfsRoot, 0755); err != nil {
		return TestDirsType{}, fmt.Errorf("failed creating fake devfs dir: %v", err)
	}

	return TestDirsType{
		TestRoot:                 testRoot,
		CdiRoot:                  cdiRoot,
		SysfsRoot:                fakeSysfsRoot,
		KubeletPluginDir:         driverPluginRoot,
		KubeletPluginRegistryDir: driverRegistrarRoot,
		DevfsRoot:                devfsRoot,
	}, nil
}

func CleanupTest(t *testing.T, testname string, testRoot string) {
	if err := os.RemoveAll(testRoot); err != nil {
		t.Logf("%v: could not cleanup temp directory %v: %v", testname, testRoot, err)
	}
}

func NewMonitoringClaim(claimNs, claimName, claimUID, requestName, driverName, pool string, allocatedDevices []string) *resourcev1.ResourceClaim {
	claim := NewClaim(claimNs, claimName, claimUID, requestName, driverName, pool, allocatedDevices)
	claim.Spec.Devices.Requests[0].AdminAccess = &[]bool{true}[0]
	claim.Spec.Devices.Requests[0].AllocationMode = "All"

	return claim
}

func NewClaim(claimNs, claimName, claimUID, requestName, driverName, pool string, allocatedDevices []string) *resourcev1.ResourceClaim {
	allocationResults := []resourcev1.DeviceRequestAllocationResult{}
	for _, deviceUID := range allocatedDevices {
		newDevice := resourcev1.DeviceRequestAllocationResult{
			Device:  deviceUID,
			Request: requestName,
			Driver:  driverName,
			Pool:    pool,
		}
		allocationResults = append(allocationResults, newDevice)
	}

	alienDevice := resourcev1.DeviceRequestAllocationResult{
		Device:  "numberOne",
		Request: "complimentaryRequest",
		Driver:  "NonExistent",
		Pool:    pool,
	}
	allocationResults = append(allocationResults, alienDevice)

	claim := &resourcev1.ResourceClaim{
		TypeMeta:   metav1.TypeMeta{APIVersion: "resource.k8s.io/v1beta1", Kind: "ResourceClaim"},
		ObjectMeta: metav1.ObjectMeta{Namespace: claimNs, Name: claimName, UID: types.UID(claimUID)},
		Spec: resourcev1.ResourceClaimSpec{
			Devices: resourcev1.DeviceClaim{
				Requests: []resourcev1.DeviceRequest{
					{Name: requestName, DeviceClassName: driverName, Count: 1},
					{Name: "complimentaryRequest", DeviceClassName: "NonExistent"},
				},
			},
		},
		Status: resourcev1.ResourceClaimStatus{
			Allocation: &resourcev1.AllocationResult{
				Devices: resourcev1.DeviceAllocationResult{
					Results: allocationResults,
				},
			},
		},
	}

	return claim
}

func NewClaimWithAlienDevice(claimNs, claimName, claimUID, requestName, driverName, pool string, allocatedDevices []string) *resourcev1.ResourceClaim {
	claim := NewClaim(claimNs, claimName, claimUID, requestName, driverName, pool, allocatedDevices)

	alienDevice := resourcev1.DeviceRequestAllocationResult{
		Device:  "numberOne",
		Request: "complimentaryRequest",
		Driver:  "NonExistent",
		Pool:    pool,
	}
	allocationDevices := &claim.Status.Allocation.Devices
	allocationDevices.Results = append(allocationDevices.Results, alienDevice)

	return claim
}
