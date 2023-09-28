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
	"context"
	"fmt"
	"os"
	"path"
	"reflect"
	"strings"
	"testing"

	gpudrafake "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gpu/clientset/versioned/fake"
	gpuv1alpha2 "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gpu/v1alpha2"
	intelcrd "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gpu/v1alpha2/api"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/kubelet/pkg/apis/dra/v1alpha3"
)

func writeTestFile(t *testing.T, filePath string, fileContents string) {
	fhandle, err := os.Create(filePath)
	if err != nil {
		t.Errorf("could not create test file %v: %v", filePath, err)
	}

	if _, err = fhandle.WriteString(fileContents); err != nil {
		t.Errorf("could not write to test file %v: %v", filePath, err)
	}

	if err := fhandle.Close(); err != nil {
		t.Errorf("could not close file %v", filePath)
	}
}

func fakeSysfsSRIOVContents(t *testing.T, sysfsRoot string, devices DevicesInfo) {
	for deviceUID, device := range devices {
		i915DevDir := path.Join(sysfsRoot, "bus/pci/drivers/i915/", deviceUID[:12])
		switch device.deviceType {
		case "gpu":
			if device.maxvfs <= 0 {
				continue
			}
			writeTestFile(t, path.Join(i915DevDir, "sriov_totalvfs"), fmt.Sprint(device.maxvfs))
			writeTestFile(t, path.Join(i915DevDir, "sriov_drivers_autoprobe"), "1")
		case "vf":
			if _, found := devices[device.parentuid]; !found {
				t.Errorf("parent device %v of VF %v is not found", device.parentuid, deviceUID)
			}

			if err := os.Symlink(fmt.Sprintf("../%s", device.parentuid[:12]), path.Join(i915DevDir, "physfn")); err != nil {
				t.Errorf("setup error: creating fake sysfs, err: %v", err)
			}

			parentI915DevDir := path.Join(sysfsRoot, "bus/pci/drivers/i915/", device.parentuid[:12])

			parentLinkName := path.Join(parentI915DevDir, fmt.Sprintf("virtfn%d", device.vfindex))
			targetName := fmt.Sprintf("../%s", deviceUID[:12])

			if err := os.Symlink(targetName, parentLinkName); err != nil {
				t.Errorf("setup error: creating fake sysfs, err: %v", err)
			}
		default:
			t.Errorf("setup error: unsupporrted device type: %v (device %v)", device.deviceType, deviceUID)
		}
	}
}

func fakeSysFsContents(t *testing.T, sysfsRootUntrusted string, devices DevicesInfo) {
	// fake sysfsroot should be deletable.
	// To prevent disaster mistakes, it is enforced to be in /tmp.
	sysfsRoot := path.Join(sysfsRootUntrusted)
	if !strings.HasPrefix(sysfsRoot, "/tmp") {
		t.Errorf("fake sysfsroot can only be in /tmp, got: %v", sysfsRoot)
	}

	// Fail immediately, if the directory exists to prevent data loss when
	// fake sysfs would need to be deleted.
	if _, err := os.Stat(sysfsRoot); err == nil {
		t.Errorf("cannot create fake sysfs, path exists: %v\n", sysfsRoot)
	}

	if err := os.Mkdir(sysfsRoot, 0770); err != nil {
		t.Errorf("could not create fake sysfs root %v: %v", sysfsRoot, err)
	}

	for deviceUID, device := range devices {
		// driver setup
		i915DevDir := path.Join(sysfsRoot, "bus/pci/drivers/i915/", deviceUID[:12])
		if err := os.MkdirAll(i915DevDir, 0770); err != nil {
			t.Errorf("setup error: creating fake sysfs, err: %v", err)
		}
		writeTestFile(t, path.Join(i915DevDir, "device"), device.model)

		cardName := fmt.Sprintf("card%v", device.cardidx)
		renderdName := fmt.Sprintf("renderD%v", device.renderdidx)
		if err := os.MkdirAll(path.Join(i915DevDir, "drm", cardName), 0770); err != nil {
			t.Errorf("setup error: creating fake sysfs, err: %v", err)
		}
		if err := os.MkdirAll(path.Join(i915DevDir, "drm", renderdName), 0770); err != nil {
			t.Errorf("setup error: creating fake sysfs, err: %v", err)
		}

		// DRM setup
		drmDevDir := path.Join(sysfsRoot, "class/drm", cardName)
		if err := os.MkdirAll(drmDevDir, 0770); err != nil {
			t.Errorf("setup error: creating fake sysfs, err: %v", err)
		}
		if err := os.MkdirAll(path.Join(drmDevDir, "gt/gt0"), 0770); err != nil {
			t.Errorf("setup error: creating fake sysfs, err: %v", err)
		}

		localMemoryStr := fmt.Sprint(device.memoryMiB * 1024 * 1024)
		writeTestFile(t, path.Join(drmDevDir, "lmem_total_bytes"), localMemoryStr)
	}
	fakeSysfsSRIOVContents(t, sysfsRoot, devices)
}

func TestFakeSysfs(t *testing.T) {
	fakeSysfsRoot := "/tmp/fakesysfs2"

	fakeSysFsContents(
		t,
		fakeSysfsRoot,
		DevicesInfo{
			"0000:00:02.0-0x56c0": {model: "0x56c0", memoryMiB: 8192, deviceType: "gpu", cardidx: 0, renderdidx: 128, uid: "0000:00:02.0-0x56c0", maxvfs: 16},
		},
	)

	if err := os.RemoveAll(fakeSysfsRoot); err != nil {
		t.Errorf("could not cleanup fake sysfs %v", fakeSysfsRoot)
	}
}

func getFakeDriver(sysfsRoot string) (*driver, error) {

	fakeGas := &gpuv1alpha2.GpuAllocationState{
		TypeMeta:   metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{Namespace: "namespace1", Name: "node1"},
		Status:     "Ready",
	}
	fakeDRAClient := gpudrafake.NewSimpleClientset(fakeGas)

	config := &configType{
		crdconfig: &intelcrd.GpuAllocationStateConfig{
			Name:      "node1",
			Namespace: "namespace1",
		},
		clientset: &clientsetType{
			kubefake.NewSimpleClientset(),
			fakeDRAClient,
		},
		cdiRoot: "/tmp/fakecdiroot",
	}

	os.Setenv("SYSFS_ROOT", sysfsRoot)

	return newDriver(context.TODO(), config)
}

func TestNodePrepareResources(t *testing.T) {
	type testCase struct {
		name             string
		request          *v1alpha3.NodePrepareResourcesRequest
		expectedResponse *v1alpha3.NodePrepareResourcesResponse
	}

	testcases := []testCase{
		{
			name: "blank request",
			request: &v1alpha3.NodePrepareResourcesRequest{
				Claims: []*v1alpha3.Claim{},
			},
			expectedResponse: &v1alpha3.NodePrepareResourcesResponse{
				Claims: map[string]*v1alpha3.NodePrepareResourceResponse{},
			},
		},
		{
			name: "single GPU",
			request: &v1alpha3.NodePrepareResourcesRequest{
				Claims: []*v1alpha3.Claim{
					{Name: "claim1", Namespace: "namespace2", Uid: "uid1"},
				},
			},
			expectedResponse: &v1alpha3.NodePrepareResourcesResponse{
				Claims: map[string]*v1alpha3.NodePrepareResourceResponse{
					"uid1": {CDIDevices: []string{"intel.com/gpu=0000:00:02.0-0x56c0"}},
				},
			},
		},
	}

	fakeSysfsRoot := "/tmp/fakesysfs"
	fakeSysFsContents(
		t,
		fakeSysfsRoot,
		DevicesInfo{
			"0000:00:02.0-0x56c0":     {model: "0x56c0", memoryMiB: 14248, deviceType: "gpu", cardidx: 0, renderdidx: 128, uid: "0000:00:02.0-0x56c0", maxvfs: 16},
			"0000:00:03.0-0x56c0":     {model: "0x56c0", memoryMiB: 14248, deviceType: "gpu", cardidx: 1, renderdidx: 129, uid: "0000:00:03.0-0x56c0", maxvfs: 16},
			"0000:00:03.0-0x56c0-vf0": {model: "0x56c0", memoryMiB: 8064, deviceType: "vf", cardidx: 2, renderdidx: 130, uid: "0000:00:03.0-0x56c0-vf0", vfindex: 0, vfprofile: "flex170_m2", parentuid: "0000:00:03.0-0x56c0"},
		},
	)

	driver, driverErr := getFakeDriver(fakeSysfsRoot)
	if driverErr != nil {
		t.Errorf("could not create kubelet-plugin: %v\n", driverErr)
	}

	gasspecWithAllocations := driver.gas.Spec.DeepCopy()
	if gasspecWithAllocations.AllocatedClaims == nil {
		gasspecWithAllocations.AllocatedClaims = map[string]gpuv1alpha2.AllocatedClaim{}
	}

	gasspecWithAllocations.AllocatedClaims["uid1"] = gpuv1alpha2.AllocatedClaim{
		Gpus: []gpuv1alpha2.AllocatedGpu{
			{
				UID: "0000:00:02.0-0x56c0", Type: "gpu", Memory: 4096,
			},
		},
	}
	if err := driver.gas.Update(context.TODO(), gasspecWithAllocations); err != nil {
		t.Errorf("setup error: could not update GAS")
	}

	for _, testcase := range testcases {
		response, err := driver.NodePrepareResources(context.TODO(), testcase.request)

		if err != nil {
			t.Errorf("%v: error %v, expected no error", testcase.name, err)
		}

		if !reflect.DeepEqual(response, testcase.expectedResponse) {
			t.Errorf("%v: unexpected response: %+v, expected response: %v", testcase.name, response, testcase.expectedResponse)
		}
	}
	if err := os.RemoveAll(fakeSysfsRoot); err != nil {
		t.Errorf("could not cleanup fake sysfs %v", fakeSysfsRoot)
	}
}

func TestReuseLeftoverSRIOVResources(t *testing.T) {
	fakeSysfsRoot := "/tmp/fakesysfs2"
	fakeSysFsContents(
		t,
		fakeSysfsRoot,
		DevicesInfo{
			"0000:00:02.0-0x56c0": {model: "0x56c0", memoryMiB: 14248, deviceType: "gpu", cardidx: 0, renderdidx: 128, uid: "0000:00:02.0-0x56c0", maxvfs: 16},
			"0000:00:03.0-0x56c0": {model: "0x56c0", memoryMiB: 14248, deviceType: "gpu", cardidx: 1, renderdidx: 129, uid: "0000:00:03.0-0x56c0", maxvfs: 16},
		},
	)

	driver, driverErr := getFakeDriver(fakeSysfsRoot)
	if driverErr != nil {
		t.Errorf("could not create kubelet-plugin: %v\n", driverErr)
	}

	expectedToProvision := map[string]map[string]*DeviceInfo{
		"0000:00:03.0-0x56c0": {
			"0000:00:03.0-0x56c0-vf0": {
				uid:        "0000:00:03.0-0x56c0-vf0",
				memoryMiB:  8064,
				deviceType: "vf",
				vfindex:    0,
				vfprofile:  "flex170_m2",
				parentuid:  "0000:00:03.0-0x56c0",
			},
			"0000:00:03.0-0x56c0-vf1": {
				uid:        "0000:00:03.0-0x56c0-vf1",
				memoryMiB:  0, // memory is not populated until VF is provisioned. Because not needed.
				deviceType: "vf",
				vfindex:    1,
				vfprofile:  "flex170_m2",
				parentuid:  "0000:00:03.0-0x56c0",
			},
		},
	}

	toProvision := map[string]map[string]*DeviceInfo{
		"0000:00:03.0-0x56c0": {
			"0000:00:03.0-0x56c0-vf0": {
				uid:        "0000:00:03.0-0x56c0-vf0",
				memoryMiB:  8064,
				deviceType: "vf",
				vfindex:    0,
				vfprofile:  "flex170_m2",
				parentuid:  "0000:00:03.0-0x56c0",
			},
		},
	}
	driver.reuseLeftoverSRIOVResources(toProvision)

	if !reflect.DeepEqual(toProvision, expectedToProvision) {
		for _, vf := range toProvision["0000:00:03.0-0x56c0"] {
			fmt.Printf("toProvision VF: %+v\n", vf)
		}
		for _, vf := range expectedToProvision["0000:00:03.0-0x56c0"] {
			fmt.Printf("toProvision VF: %+v\n", vf)
		}
		t.Errorf("unexpected result after reusing leftovers: %+v; expected: %+v", toProvision, expectedToProvision)
	}
	if err := os.RemoveAll(fakeSysfsRoot); err != nil {
		t.Errorf("could not cleanup fake sysfs %v", fakeSysfsRoot)
	}
}
