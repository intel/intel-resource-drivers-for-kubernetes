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

package mei

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/helpers"

	"k8s.io/klog/v2"
)

func DiscoverMEIDeviceForGPU(sysfsDriverDir string, sysfsDeviceDir string) string {
	if meiName := discoverMEIDeviceFromClassMEI(sysfsDeviceDir); meiName != "" {
		return meiName
	}

	pattern := path.Join(sysfsDeviceDir, "*mei*", "mei", "mei*")
	meiPath, err := globFirstPath(pattern)
	if err != nil {
		// TODO: For iGPUs, there is no 'xe.mei-gscfi' directory even though /dev/meiX exists.
		// It is also possible that dGPUs do not have MEI implemented yet.
		// It should be investigated whether MEI devices are used for firmware updates even when there is no 'xe.mei-gscfi' directory,
		// and whether there is another method to detect the MEI device bound to the GPU.
		klog.V(5).Infof("failed to scan MEI path with pattern %v: %v", pattern, err)
		return ""
	}

	meiName := path.Base(meiPath)
	if !device.MEIRegexp.MatchString(meiName) {
		klog.V(5).Infof("unexpected MEI path format %v", meiPath)
		return ""
	}

	return meiName
}

func discoverMEIDeviceFromClassMEI(sysfsDeviceDir string) string {
	meiClassDir := path.Join(helpers.GetSysfsRoot(device.SysfsMEIpath), device.SysfsMEIpath)
	entries, err := os.ReadDir(meiClassDir)
	if err != nil {
		return ""
	}

	deviceRealPath, err := filepath.EvalSymlinks(sysfsDeviceDir)
	if err != nil {
		return ""
	}

	for _, entry := range entries {
		meiName := entry.Name()
		if !device.MEIRegexp.MatchString(meiName) {
			continue
		}

		meiRealPath, err := filepath.EvalSymlinks(path.Join(meiClassDir, meiName))
		if err != nil {
			continue
		}

		if strings.HasPrefix(meiRealPath, deviceRealPath+string(os.PathSeparator)) {
			return meiName
		}
	}

	return ""
}

func globFirstPath(pattern string) (string, error) {
	globResult, err := filepath.Glob(pattern)
	if err != nil {
		return "", err
	}

	if len(globResult) == 0 {
		return "", fmt.Errorf("no entries found for pattern %v", pattern)
	}

	sort.Strings(globResult)

	return globResult[0], nil
}
