//
// Copyright (C) 2023-2026 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0
//

package drm

import (
	"fmt"
	"os"
	"path"

	"k8s.io/klog/v2"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
)

// DeduceCardAndRenderDNames returns DRM device names such as card0 and renderD128.
func DeduceCardAndRenderDNames(sysfsDeviceDir string) (string, string, error) {
	cardName := ""
	renderDName := ""

	// get card and renderD names
	drmDir := path.Join(sysfsDeviceDir, "drm")
	drmFiles, err := os.ReadDir(drmDir)
	if err != nil { // ignore this device
		return "", "", fmt.Errorf("cannot read device folder %v: %v", drmDir, err)
	}

	for _, drmFile := range drmFiles {
		drmFileName := drmFile.Name()
		if device.CardRegexp.MatchString(drmFileName) {
			cardName = drmFileName
		} else if device.RenderdRegexp.MatchString(drmFileName) {
			renderDName = drmFileName
		}
	}

	if cardName == "" {
		return "", "", fmt.Errorf("failed to find DRM card device in %v", drmDir)
	}

	if renderDName == "" {
		klog.V(5).Infof("No renderD device found in %v", drmDir)
	}

	return cardName, renderDName, nil
}
