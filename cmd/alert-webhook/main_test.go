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
	"path/filepath"
	"sync"
	"testing"

	gpucsfake "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gpu/clientset/versioned/fake"
	gpuv1alpha2 "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gpu/v1alpha2"
	intelcrd "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gpu/v1alpha2/api"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
)

const (
	// labels and their values used in JSON test files.
	testNode  = "test-node"             // <node>
	testSpace = "monitoring"            // <namespace>
	uidFormat = "0000:%02d:00.0-0x56a0" // 0000:<pci_bdf>-<pci_dev>
	// path to JSON files used in notification tests.
	jsonPath = "notifications"
)

func createAllocatable(uids []string, devType gpuv1alpha2.GpuType, vfParent string) map[string]intelcrd.AllocatableGpu {
	if uids == nil {
		return nil
	}

	gpus := make(map[string]intelcrd.AllocatableGpu)
	for _, uid := range uids {
		gpus[uid] = intelcrd.AllocatableGpu{
			ParentUID: vfParent,
			Type:      devType,
			UID:       uid,
		}
	}

	if devType == intelcrd.VfDeviceType {
		gpus[vfParent] = intelcrd.AllocatableGpu{
			Type:   intelcrd.GpuDeviceType,
			Maxvfs: uint64(len(uids)),
			UID:    vfParent,
		}
	}

	return gpus
}

func createTainted(uids []string, reason string) map[string]intelcrd.TaintedGpu {
	if uids == nil {
		return nil
	}
	taints := make(map[string]intelcrd.TaintedGpu, len(uids))
	for _, uid := range uids {
		taints[uid] = intelcrd.TaintedGpu{Reason: reason}
	}
	return taints
}

// createFilterFlags returns filterFlags that match test JSON files content.
func createFilterFlags() filterFlags {
	filterAlerts := "GpuNeedsReset"
	filterGroups := "namespace,service"
	filterValues := testSpace + ",xpu-manager"
	return filterFlags{
		alerts: &filterAlerts,
		groups: &filterGroups,
		values: &filterValues,
	}
}

type notification struct {
	name string // file name before prefix
	ok   bool   // whether content parse should succeed
}

func applyNotifications(t *testing.T, alerter *alerter, files []notification) {
	klog.V(5).Infof("processNotifications(alerter: %+v, files: %v)", alerter, files)
	for _, file := range files {
		path := filepath.Join(jsonPath, file.name+".json")

		data, err := os.ReadFile(path)
		if err != nil {
			panic(err)
		}

		err = alerter.parseNotification(data)
		if file.ok && err != nil {
			t.Errorf("ERROR, unexpected error from alerter.parseNotification(): %v", err)
		} else if !file.ok && err == nil {
			t.Error("ERROR, no error from alerter.parseNotification()")
		}
	}
}

func gpuUID(i int) string {
	return fmt.Sprintf(uidFormat, i)
}

// tests all webhook functionality except for:
// - mainloop & option parsing
// - tainter creation (it needs to be faked here)
// - HTTP header parsing.
func TestWholeWebhook(t *testing.T) {

	type testCase struct {
		testName    string
		namespace   string
		cliFlags    cliFlags
		filterFlags filterFlags
		files       []notification      // notifications content files
		devType     gpuv1alpha2.GpuType // type of allocatable devices
		devices     []string            // allocatable GPU devices
		tainted     []string            // already tainted devices
		expected    []string            // GPU devices expected to be tainted
	}

	// parent device added when devType is VF, must have different UID from others
	vfParent := gpuUID(0)
	// devices that are supposed to be present
	allDevices := []string{gpuUID(1), gpuUID(2)}
	// valid alert notification files for 'allDevices'
	allAlerts := []notification{
		{"taint-1", true},
		{"taint-2", true},
	}

	// devices not in 'allDevices' (non-existing)
	unknownDevices := []string{gpuUID(3)}
	// valid alert notification files for 'unknownDevices'
	unknownAlerts := []notification{
		{"taint-3", true},
	}

	// alert notification files for above devices with invalid content
	failingAlerts := []notification{
		{"taint-1-fail", false},
		{"taint-2-fail", false},
		{"taint-3-fail", false},
	}

	// files for alerts notifications that do not pass filters
	// given in defaultFlags (fail without errors)
	filteredAlerts := []notification{
		{"taint-1-filtered", true},
		{"taint-2-filtered", true},
	}
	defaultFlags := createFilterFlags()

	fakeNode := testNode
	fakeReason := "taintReason"

	testCases := []testCase{
		{
			testName:    "clear all device taints with cli flags",
			namespace:   testSpace,
			cliFlags:    cliFlags{node: &fakeNode},
			filterFlags: defaultFlags,
			devType:     intelcrd.GpuDeviceType,
			devices:     allDevices,
			tainted:     allDevices,
			files:       nil,
			expected:    nil,
		},
		{
			testName:    "taint all devices with cli flags",
			namespace:   testSpace,
			cliFlags:    cliFlags{node: &fakeNode, reason: &fakeReason},
			filterFlags: defaultFlags,
			devType:     intelcrd.GpuDeviceType,
			devices:     allDevices,
			tainted:     nil,
			files:       nil,
			expected:    allDevices,
		},
		{
			testName:    "cli flags taint PF, not VFs",
			namespace:   testSpace,
			cliFlags:    cliFlags{node: &fakeNode, reason: &fakeReason},
			filterFlags: defaultFlags,
			devType:     intelcrd.VfDeviceType,
			devices:     allDevices,
			tainted:     nil,
			files:       nil,
			expected:    []string{vfParent},
		},
		{
			testName:    "VF alerts taint only their PF",
			namespace:   testSpace,
			cliFlags:    cliFlags{},
			filterFlags: defaultFlags,
			devType:     intelcrd.VfDeviceType,
			devices:     allDevices,
			tainted:     nil,
			files:       allAlerts,
			expected:    []string{vfParent},
		},
		{
			testName:    "no taints from alerts with no devices",
			namespace:   testSpace,
			cliFlags:    cliFlags{},
			filterFlags: defaultFlags,
			devType:     intelcrd.GpuDeviceType,
			devices:     nil,
			tainted:     nil,
			files:       allAlerts,
			expected:    nil,
		},
		{
			testName:    "taint all devices with alerts",
			namespace:   testSpace,
			cliFlags:    cliFlags{},
			filterFlags: defaultFlags,
			devType:     intelcrd.GpuDeviceType,
			devices:     allDevices,
			tainted:     nil,
			files:       allAlerts,
			expected:    allDevices,
		},
		{
			testName:    "do not taint unknown devices",
			namespace:   testSpace,
			cliFlags:    cliFlags{},
			filterFlags: defaultFlags,
			devType:     intelcrd.GpuDeviceType,
			devices:     allDevices,
			tainted:     nil,
			files:       append(allAlerts, unknownAlerts...),
			expected:    allDevices,
		},
		{
			testName:    "do not remove unknown existing taints",
			namespace:   testSpace,
			cliFlags:    cliFlags{},
			filterFlags: defaultFlags,
			devType:     intelcrd.GpuDeviceType,
			devices:     allDevices,
			tainted:     unknownDevices,
			files:       allAlerts,
			expected:    append(allDevices, unknownDevices...),
		},
		{
			testName:    "invalid alerts do not taint",
			namespace:   testSpace,
			cliFlags:    cliFlags{},
			filterFlags: defaultFlags,
			devType:     intelcrd.GpuDeviceType,
			devices:     allDevices,
			tainted:     nil,
			files:       failingAlerts,
			expected:    nil,
		},
		{
			testName:    "filtered alerts do not taint",
			namespace:   testSpace,
			cliFlags:    cliFlags{},
			filterFlags: defaultFlags,
			devType:     intelcrd.GpuDeviceType,
			devices:     allDevices,
			tainted:     nil,
			files:       filteredAlerts,
			expected:    nil,
		},
		{
			testName:    "invalid alerts do not remove existing taints",
			namespace:   testSpace,
			cliFlags:    cliFlags{},
			filterFlags: defaultFlags,
			devType:     intelcrd.GpuDeviceType,
			devices:     allDevices,
			tainted:     allDevices,
			files:       append(failingAlerts, filteredAlerts...),
			expected:    allDevices,
		},
	}

	for _, testCase := range testCases {
		gas := &gpuv1alpha2.GpuAllocationState{
			TypeMeta:   metav1.TypeMeta{},
			ObjectMeta: metav1.ObjectMeta{Namespace: testCase.namespace, Name: fakeNode},
		}
		gas.Spec.AllocatableDevices = createAllocatable(testCase.devices, testCase.devType, vfParent)
		gas.Spec.TaintedDevices = createTainted(testCase.tainted, fakeReason)

		ctx := context.Background()
		draClientset := gpucsfake.NewSimpleClientset(gas)
		tainter := &tainter{
			ctx:      ctx,
			nsname:   testCase.namespace,
			csconfig: &rest.Config{},
			clientset: &clientsetType{
				kubefake.NewSimpleClientset(),
				draClientset,
			},
			mutex: sync.Mutex{},
		}

		alerter, _ := newAlerter(&testCase.filterFlags, tainter)

		t.Log(testCase.testName)

		if err := tainter.setTaintsFromFlags(&testCase.cliFlags); err != nil {
			t.Errorf("ERROR, from tainter.setTaintsFromFlags(): %v", err)
		}

		applyNotifications(t, alerter, testCase.files)

		klog.V(5).Infof("Test gas.Spec: %+v", gas.Spec)

		crdconfig := &intelcrd.GpuAllocationStateConfig{
			Name:      fakeNode,
			Namespace: testCase.namespace,
		}
		// get updated GAS content for taint result checks
		gasu := intelcrd.NewGpuAllocationState(crdconfig, draClientset)
		if err := gasu.Get(ctx); err != nil {
			t.Errorf("ERROR, failed to Get() updated state for gas.Spec: %v", err)
		}

		klog.V(5).Infof("Updated gas.Spec: %+v", gasu.Spec)

		if len(gasu.Spec.TaintedDevices) != len(testCase.expected) {
			t.Errorf("ERROR, taint count=%d, expected=%d",
				len(gasu.Spec.TaintedDevices), len(testCase.expected))
		}

		for _, uid := range testCase.expected {
			if _, found := gasu.Spec.TaintedDevices[uid]; !found {
				t.Errorf("ERROR, taint missing for '%s'", uid)
			}
		}
	}
}

func TestMain(m *testing.M) {
	// To be able to see and set driver logging level, e.g:
	// go test -v -fastfail github.com/intel/intel-resource-drivers-for-kubernetes/cmd/alert-webhook -args -v=5
	klog.InitFlags(nil)
	os.Exit(m.Run())
}
