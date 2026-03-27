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

package drm

import (
	"fmt"
	"os"
	"path"
	"strconv"

	"k8s.io/klog/v2"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
)

// DeduceCardAndRenderdIndexes arg is device "<sysfs>/bus/pci/drivers/i915/<DBDF>/drm/" path.
func DeduceCardAndRenderdIndexes(sysfsDeviceDir string) (uint64, uint64, error) {
	var cardIdx uint64
	var renderDidx uint64

	// get card and renderD indexes
	drmDir := path.Join(sysfsDeviceDir, "drm")
	drmFiles, err := os.ReadDir(drmDir)
	if err != nil { // ignore this device
		return 0, 0, fmt.Errorf("cannot read device folder %v: %v", drmDir, err)
	}

	for _, drmFile := range drmFiles {
		drmFileName := drmFile.Name()
		if device.CardRegexp.MatchString(drmFileName) {
			cardIdx, err = strconv.ParseUint(drmFileName[4:], 10, 64)
			if err != nil {
				return 0, 0, fmt.Errorf("failed to parse index of DRM card device '%v', skipping", drmFileName)
			}
		} else if device.RenderdRegexp.MatchString(drmFileName) {
			renderValue, err := strconv.ParseUint(drmFileName[7:], 10, 64)
			if err != nil {
				klog.Errorf("failed to parse renderDN device: %v, skipping", drmFileName)
				continue
			}
			renderDidx = renderValue
		}
	}

	return cardIdx, renderDidx, nil
}
