/*
 * Copyright (c) 2026, Intel Corporation.  All Rights Reserved.
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

package drm

import (
	"testing"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/fakesysfs"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
	testhelpers "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/plugintesthelpers"
)

func TestDeduceCardAndRenderDNames(t *testing.T) {

	testDirs, err := testhelpers.NewTestDirs(device.DriverName)
	defer testhelpers.CleanupTest(t, "TestGPUFakeSysfs", testDirs.TestRoot)
	if err != nil {
		t.Errorf("could not create fake system dirs: %v", err)
		return
	}

	if err := fakesysfs.FakeSysFsGpuContents(
		testDirs.SysfsRoot,
		testDirs.DevfsRoot,
		device.DevicesInfo{
			"0000-00-02-0-0x56c0": {Model: "0x56c0", MemoryMiB: 8192, DeviceType: "gpu", CardName: "card0", RenderDName: "renderD128", UID: "0000-00-02-0-0x56c0", MaxVFs: 16, Driver: "i915"},
			"0000-00-03-0-0x56c0": {Model: "0x56c0", MemoryMiB: 8192, DeviceType: "gpu", CardName: "card1", RenderDName: "renderD129", UID: "0000-00-03-0-0x56c0", MaxVFs: 16, Driver: "xe"},
		},
		false,
	); err != nil {
		t.Errorf("setup error: could not create fake sysfs: %v", err)
		return
	}

	cardName, renderDName, err := DeduceCardAndRenderDNames(testDirs.SysfsRoot + "/bus/pci/drivers/i915/0000:00:02.0")
	if err != nil {
		t.Errorf("DeduceCardAndRenderDNames failed: %v", err)
		return
	}

	if cardName != "card0" || renderDName != "renderD128" {
		t.Errorf("DeduceCardAndRenderDNames returned wrong names: got card %v and render %v, want card0 and renderD128", cardName, renderDName)
	}

	cardName, renderDName, err = DeduceCardAndRenderDNames(testDirs.SysfsRoot + "/bus/pci/drivers/xe/0000:00:03.0")
	if err != nil {
		t.Errorf("DeduceCardAndRenderDNames failed: %v", err)
		return
	}

	if cardName != "card1" || renderDName != "renderD129" {
		t.Errorf("DeduceCardAndRenderDNames returned wrong names: got card %v and render %v, want card1 and renderD129", cardName, renderDName)
	}
}
