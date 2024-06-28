/*
 * Copyright (c) 2023-2024, Intel Corporation.  All Rights Reserved.
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
	"maps"
	"net/http"
	"os"
	"path"
	"strings"
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
	uidFormat = "0000-%02d-00-0-0x56a0" // 0000:<pci_bdf>-<pci_dev>
	// path to JSON files used in notification tests.
	jsonPath     = "notifications"
	singleReason = "GpuNeedsReset"
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
	reasons := make(map[string]bool)
	reasons[reason] = true
	for _, uid := range uids {
		taints[uid] = intelcrd.TaintedGpu{Reasons: reasons}
	}
	return taints
}

// createFilterFlags returns filterFlags that match test JSON files content.
func createFilterFlags(reasons string) filterFlags {
	filterAlerts := reasons
	filterGroups := "service=xpu-manager,collect-gpu-daemon:namespace=" + testSpace
	return filterFlags{
		alerts: &filterAlerts,
		groups: &filterGroups,
	}
}

type notification struct {
	name   string // file name before prefix
	status int    // http.StatusOK unless Alertmanager should resend the notification.
	msg    string // start of message returned from notification processing.
}

func applyNotifications(t *testing.T, alerter *alerter, files []notification) {
	klog.V(5).Infof("processNotifications(alerter: %+v, files: %v)", alerter, files)
	for _, file := range files {
		path := path.Join(jsonPath, file.name+".json")

		data, err := os.ReadFile(path)
		if err != nil {
			panic(err)
		}

		msg, status := alerter.parseNotification(data)
		if file.status != status {
			t.Errorf("ERROR, expected %d, got %d status from '%s': %s",
				file.status, status, file.name, msg)
		}
		if file.msg == "" {
			panic("test-case file with no message specified")
		}
		if !strings.HasPrefix(msg, file.msg) {
			t.Errorf("ERROR, parsing '%s', expected '%s...', got '%s'",
				file.name, file.msg, msg)
		}
	}
}

func gpuUID(i int) string {
	return fmt.Sprintf(uidFormat, i)
}

// reasonMap returns map of given device IDs, each with given list of taint reasons.
func reasonMap(devices, reasons []string) map[string][]string {
	reasonmap := make(map[string][]string, len(devices))
	for _, uid := range devices {
		reasonmap[uid] = reasons
	}
	return reasonmap
}

func mergedMaps(src1, src2 map[string][]string) map[string][]string {
	dst := make(map[string][]string)
	maps.Copy(dst, src1)
	maps.Copy(dst, src2)
	return dst
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
		devType     gpuv1alpha2.GpuType // type of allocatable devices
		devices     []string            // allocatable GPU devices
		tainted     []string            // devices already tainted (fakeReason reason)
		files       []notification      // notifications content files (different reasons)
		expected    map[string][]string // tainted GPU devices and their expected taint reasons
	}

	// prefixes for handling notifications with single firing/resolved alert
	const (
		msgSingleOK   = "1/1 alerts passed notification processing"
		msgSingleFail = "0/1 alerts passed notification processing,"
	)

	// parent device added when devType is VF, must have different UID from others
	vfParent := gpuUID(0)
	// devices that are supposed to be present
	allDevices := []string{gpuUID(1), gpuUID(2)}
	// valid alert notification files for 'allDevices'
	singleAlerts := []notification{
		{"taint-1", http.StatusOK, msgSingleOK},
		{"taint-2", http.StatusOK, msgSingleOK},
	}

	// devices not in 'allDevices' (non-existing)
	unknownDevices := []string{gpuUID(3)}
	// parses OK as node exists with GPUs, just not this one
	unknownAlerts := []notification{
		{"taint-3", http.StatusOK, msgSingleOK},
	}
	// fails when node has no devices (or is invalid)
	noDevicesAlerts := []notification{
		{"taint-3", http.StatusOK, msgSingleFail},
	}

	// alert notification files for above devices with invalid content
	failingAlerts := []notification{
		{"taint-1-fail-dev", http.StatusOK, msgSingleFail},
		{"taint-1-fail-json", http.StatusBadRequest, "Failed: invalid JSON"},
		{"taint-2-fail-node", http.StatusOK, msgSingleFail},
		{"taint-3-fail-status", http.StatusOK, msgSingleFail},
		{"multi-fail", http.StatusOK, "0/2 alerts passed notification processing,"},
	}

	// files for alerts notifications that do not pass filters
	// given in defaultFlags (fail without errors)
	const msgGroupLabelFail = "Failed: all alerts skipped due to their group label mismatch:"
	filteredAlerts := []notification{
		{"taint-1-filtered", http.StatusBadRequest, msgGroupLabelFail},
		{"taint-2-filtered", http.StatusBadRequest, msgGroupLabelFail},
	}

	// multi-alert notification files for 2 GPUs in 'allDevices'
	multiAlerts := []notification{
		{"multi-taint", http.StatusOK, "6/7 alerts passed notification processing"}, // 1 stale
		{"multi-resolve", http.StatusOK, msgSingleOK},
	}
	// 2nd alert for 1st GPU, and 1st alert for 2nd GPU are resolved,
	// so only these should remain
	multiReasons := map[string][]string{
		gpuUID(1): {"GpuAlert_1_1"},
		gpuUID(2): {"GpuAlert_2_2"},
	}

	// mismatch - try to remove 1st multi-alert for 2nd GPU, for singleAlert case
	resolvedAlerts := []notification{
		{"multi-resolve", http.StatusOK, msgSingleFail},
	}

	fakeNode := testNode
	// for CLI tainting/untainting
	taintAction := "taint"
	untaintAction := "untaint"
	// (pre-existing) taint reasons
	fakeReason := "taintReason"
	// devices & reasons to clear
	themAll := "all"

	// resolve 1 fakeReason alert for 1st allDevices GPU...
	fakeResolved := []notification{
		{"taint-1-resolve", http.StatusOK, msgSingleOK},
	}
	// ...leaving 2nd tainted
	resolvedReasons := map[string][]string{
		gpuUID(2): {fakeReason},
	}

	allReasons := "GpuAlert_1_1,GpuAlert_1_2,GpuAlert_2_1,GpuAlert_2_2," + singleReason + "," + fakeReason

	// this includes only singleReason
	defaultFlags := createFilterFlags(singleReason)

	testCases := []testCase{
		{
			testName:    "clear all device taints with cli flags",
			namespace:   testSpace,
			cliFlags:    cliFlags{action: &untaintAction, nodes: &fakeNode, devices: &themAll, reasons: &themAll},
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
			cliFlags:    cliFlags{action: &taintAction, nodes: &fakeNode, devices: &themAll, reasons: &fakeReason},
			filterFlags: defaultFlags,
			devType:     intelcrd.GpuDeviceType,
			devices:     allDevices,
			tainted:     nil,
			files:       nil,
			expected:    reasonMap(allDevices, []string{fakeReason}),
		},
		{
			testName:    "cli flags taint PF, not VFs",
			namespace:   testSpace,
			cliFlags:    cliFlags{action: &taintAction, nodes: &fakeNode, devices: &themAll, reasons: &fakeReason},
			filterFlags: defaultFlags,
			devType:     intelcrd.VfDeviceType,
			devices:     allDevices,
			tainted:     nil,
			files:       nil,
			expected:    map[string][]string{vfParent: {fakeReason}},
		},
		{
			testName:    "VF alerts are ignored",
			namespace:   testSpace,
			cliFlags:    cliFlags{},
			filterFlags: defaultFlags,
			devType:     intelcrd.VfDeviceType,
			devices:     allDevices,
			tainted:     nil,
			files:       singleAlerts,
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
			files:       singleAlerts,
			expected:    reasonMap(allDevices, []string{singleReason}),
		},
		{
			testName:    "no devices, no taints",
			namespace:   testSpace,
			cliFlags:    cliFlags{},
			filterFlags: defaultFlags,
			devType:     intelcrd.GpuDeviceType,
			devices:     nil,
			tainted:     nil,
			files:       noDevicesAlerts,
			expected:    nil,
		},
		{
			testName:    "do not taint unknown devices",
			namespace:   testSpace,
			cliFlags:    cliFlags{},
			filterFlags: defaultFlags,
			devType:     intelcrd.GpuDeviceType,
			devices:     allDevices,
			tainted:     nil,
			files:       append(singleAlerts, unknownAlerts...),
			expected:    reasonMap(allDevices, []string{singleReason}),
		},
		{
			testName:    "do not remove unknown existing taints",
			namespace:   testSpace,
			cliFlags:    cliFlags{},
			filterFlags: defaultFlags,
			devType:     intelcrd.GpuDeviceType,
			devices:     allDevices,
			tainted:     unknownDevices,
			files:       singleAlerts,
			expected: mergedMaps(reasonMap(unknownDevices, []string{fakeReason}),
				reasonMap(allDevices, []string{singleReason})),
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
			testName:    "invalid alerts for same GPUs do not remove existing taints",
			namespace:   testSpace,
			cliFlags:    cliFlags{},
			filterFlags: defaultFlags,
			devType:     intelcrd.GpuDeviceType,
			devices:     allDevices,
			tainted:     allDevices,
			files:       append(failingAlerts, filteredAlerts...),
			expected:    reasonMap(allDevices, []string{fakeReason}),
		},
		{
			testName:    "resolve alerts do not add taints, nor remove non-matching reasons",
			namespace:   testSpace,
			cliFlags:    cliFlags{},
			filterFlags: createFilterFlags(allReasons),
			devType:     intelcrd.GpuDeviceType,
			devices:     allDevices,
			tainted:     nil,
			files:       append(singleAlerts, resolvedAlerts...),
			expected:    reasonMap(allDevices, []string{singleReason}),
		},
		{
			testName:    "resolve pre-existing taint",
			namespace:   testSpace,
			cliFlags:    cliFlags{},
			filterFlags: createFilterFlags(fakeReason),
			devType:     intelcrd.GpuDeviceType,
			devices:     allDevices,
			tainted:     allDevices,
			files:       fakeResolved,
			expected:    resolvedReasons,
		},
		{
			testName:    "multiple alerts for same GPU, with some resolved",
			namespace:   testSpace,
			cliFlags:    cliFlags{},
			filterFlags: createFilterFlags(allReasons), // accept all reasons
			devType:     intelcrd.GpuDeviceType,
			devices:     allDevices,
			tainted:     nil,
			files:       multiAlerts,
			expected:    multiReasons,
		},
		{
			testName:    "one taint reason from CLI, another from notifications",
			namespace:   testSpace,
			cliFlags:    cliFlags{action: &taintAction, nodes: &fakeNode, devices: &themAll, reasons: &fakeReason},
			filterFlags: defaultFlags,
			devType:     intelcrd.GpuDeviceType,
			devices:     allDevices,
			tainted:     nil,
			files:       singleAlerts,
			expected:    reasonMap(allDevices, []string{fakeReason, singleReason}),
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

		alerter, err := newAlerter(&testCase.filterFlags, tainter)
		if err != nil {
			t.Errorf("ERROR, alerter flags failed: %v", err)
			continue
		}

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
			t.Errorf("ERROR, tainted GPU count=%d, expected=%d\ntainted: %v\nexpected: %v",
				len(gasu.Spec.TaintedDevices), len(testCase.expected),
				gasu.Spec.TaintedDevices, testCase.expected)
		}

		for uid, expected := range testCase.expected {
			if _, found := gasu.Spec.TaintedDevices[uid]; !found {
				t.Errorf("ERROR, taint missing for GPU '%s'", uid)
				continue
			}

			got := gasu.Spec.TaintedDevices[uid].Reasons
			if len(expected) > len(got) {
				t.Errorf("ERROR, not enough taint reasons for GPU '%s': %v", uid, got)
				continue
			}

			if len(expected) < len(got) {
				t.Errorf("ERROR, too many taint reasons for GPU '%s': %v", uid, got)
				continue
			}

			for _, reason := range expected {
				if _, found := got[reason]; !found {
					t.Errorf("ERROR, taint reason '%s' missing for GPU '%s': %v", reason, uid, got)
				}
			}
		}
	}
}

func TestMain(m *testing.M) {
	// To be able to see and set driver logging level, e.g:
	// go test -v github.com/intel/intel-resource-drivers-for-kubernetes/cmd/alert-webhook -args -v=5
	klog.InitFlags(nil)
	os.Exit(m.Run())
}
