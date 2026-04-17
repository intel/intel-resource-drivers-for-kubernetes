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

func TestDeduceCardAndRenderdIndexes(t *testing.T) {

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
			"0000-00-02-0-0x56c0": {Model: "0x56c0", MemoryMiB: 8192, DeviceType: "gpu", CardIdx: 0, RenderdIdx: 128, UID: "0000-00-02-0-0x56c0", MaxVFs: 16, Driver: "i915"},
			"0000-00-03-0-0x56c0": {Model: "0x56c0", MemoryMiB: 8192, DeviceType: "gpu", CardIdx: 1, RenderdIdx: 129, UID: "0000-00-03-0-0x56c0", MaxVFs: 16, Driver: "xe"},
		},
		false,
	); err != nil {
		t.Errorf("setup error: could not create fake sysfs: %v", err)
		return
	}

	cardIdx, renderIdx, err := DeduceCardAndRenderdIndexes(testDirs.SysfsRoot + "/bus/pci/drivers/i915/0000:00:02.0")
	if err != nil {
		t.Errorf("DeduceCardAndRenderdIndexes failed: %v", err)
		return
	}

	if cardIdx != 0 || renderIdx != 128 {
		t.Errorf("DeduceCardAndRenderdIndexes returned wrong indexes: got cardIdx %v and renderIdx %v, want cardIdx 0 and renderIdx 128", cardIdx, renderIdx)
	}

	cardIdx, renderIdx, err = DeduceCardAndRenderdIndexes(testDirs.SysfsRoot + "/bus/pci/drivers/xe/0000:00:03.0")
	if err != nil {
		t.Errorf("DeduceCardAndRenderdIndexes failed: %v", err)
		return
	}

	if cardIdx != 1 || renderIdx != 129 {
		t.Errorf("DeduceCardAndRenderdIndexes returned wrong indexes: got cardIdx %v and renderIdx %v, want cardIdx 1 and renderIdx 129", cardIdx, renderIdx)
	}
}
