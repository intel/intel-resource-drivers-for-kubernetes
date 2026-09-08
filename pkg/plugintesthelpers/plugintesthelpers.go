//
// Copyright (C) 2024-2026 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0
//

package plugintesthelpers

import (
	"fmt"
	"os"
	"path"
	"testing"
	"time"

	resourcev1 "k8s.io/api/resource/v1"
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
	claim := NewClaim(claimNs, claimName, claimUID, requestName, driverName, pool, driverName, allocatedDevices, true)
	claim.Spec.Devices.Requests[0].Exactly.AdminAccess = &[]bool{true}[0]
	claim.Spec.Devices.Requests[0].Exactly.AllocationMode = "All"

	return claim
}

// TODO: Test also >1 Count and different AlloctionModes + Selectors.
// See: https://pkg.go.dev/k8s.io/api/resource/v1#ExactDeviceRequest
func NewClaim(claimNs, claimName, claimUID, requestName, driverName, pool, deviceClass string, allocatedDevices []string, adminAccess bool) *resourcev1.ResourceClaim {
	claim := newClaim(claimNs, claimName, claimUID, requestName, driverName, pool, requestName, allocatedDevices, &adminAccess)
	claim.Spec.Devices.Requests[0].Exactly = &resourcev1.ExactDeviceRequest{DeviceClassName: deviceClass, Count: 1}

	return claim
}

// SubRequest describes a single entry of a firstAvailable prioritized list.
type SubRequest struct {
	Name        string
	DeviceClass string
}

// NewClaimFirstAvailable returns a ResourceClaim with one firstAvailable
// (prioritized list) device request.
// See: https://pkg.go.dev/k8s.io/api/resource/v1#DeviceRequest
func NewClaimFirstAvailable(
	claimNs, claimName, claimUID, requestName, driverName, pool string,
	subRequests []SubRequest, selected int, allocatedDevices []string) *resourcev1.ResourceClaim {

	firstAvailable := []resourcev1.DeviceSubRequest{}
	for _, subRequest := range subRequests {
		firstAvailable = append(firstAvailable, resourcev1.DeviceSubRequest{
			Name:            subRequest.Name,
			DeviceClassName: subRequest.DeviceClass,
			Count:           1,
		})
	}

	// Allocation result of a firstAvailable request always refers to the
	// selected subrequest, not to the request itself.
	allocatedRequestName := requestName
	if selected >= 0 && selected < len(subRequests) {
		allocatedRequestName = requestName + "/" + subRequests[selected].Name
	}

	claim := newClaim(claimNs, claimName, claimUID, requestName, driverName, pool, allocatedRequestName, allocatedDevices, nil)
	claim.Spec.Devices.Requests[0].FirstAvailable = firstAvailable

	return claim
}

// newClaim returns a ResourceClaim with one, yet unspecified, device request
// named requestName, and an allocation result for allocatedDevices referring to
// allocatedRequestName. The caller is expected to set either Exactly or
// FirstAvailable on the first request.
func newClaim(
	claimNs, claimName, claimUID, requestName, driverName, pool, allocatedRequestName string,
	allocatedDevices []string, adminAccess *bool) *resourcev1.ResourceClaim {

	allocationResults := []resourcev1.DeviceRequestAllocationResult{}
	for _, deviceUID := range allocatedDevices {
		newDevice := resourcev1.DeviceRequestAllocationResult{
			Device:      deviceUID,
			Request:     allocatedRequestName,
			Driver:      driverName,
			Pool:        pool,
			AdminAccess: adminAccess,
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
		TypeMeta:   metav1.TypeMeta{APIVersion: "resource.k8s.io/v1", Kind: "ResourceClaim"},
		ObjectMeta: metav1.ObjectMeta{Namespace: claimNs, Name: claimName, UID: types.UID(claimUID)},
		Spec: resourcev1.ResourceClaimSpec{
			Devices: resourcev1.DeviceClaim{
				Requests: []resourcev1.DeviceRequest{
					{Name: requestName},
					{Name: "complimentaryRequest", Exactly: &resourcev1.ExactDeviceRequest{DeviceClassName: "NonExistent"}},
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

func CDICacheDelay() {
	time.Sleep(200 * time.Millisecond)
}
