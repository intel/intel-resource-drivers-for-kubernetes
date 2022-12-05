/*
 * Copyright (c) 2022, Intel Corporation.  All Rights Reserved.
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
	"fmt"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"
	intelcrd "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/crd/intel/v1alpha/api"
	"k8s.io/klog/v2"
)

const (
	sysfsI915DriverDir = "/sys/bus/pci/drivers/i915"
	pciAddressRE       = `[0-9a-f]{4}:[0-9a-f]{2}:[0-9a-f]{2}\.[0-7]$`
	cardRE             = `^card[0-9]+$`
	renderdIdRE        = `^renderD[0-9]+$`
)

/* detect devices from sysfs drm directory (card id and renderD id) */
func enumerateAllPossibleDevices() (map[string]*DeviceInfo, error) {

	devices := make(map[string]*DeviceInfo)

	files, err := os.ReadDir(sysfsI915DriverDir)
	cardregexp := regexp.MustCompile(cardRE)
	renderdregexp := regexp.MustCompile(renderdIdRE)
	pciregexp := regexp.MustCompile(pciAddressRE)

	if err != nil {
		return nil, fmt.Errorf("can't read sysfs folder: %v", err)
	}

	for _, f := range files {
		// check if file is pci device
		if !pciregexp.MatchString(f.Name()) {
			continue
		}
		klog.V(5).Infof("Found GPU PCI device: " + f.Name())

		uuid := uuid.New().String()
		klog.V(5).Infof("New gpu UUID: %v", uuid)

		drmFiles, err := os.ReadDir(path.Join(sysfsI915DriverDir, f.Name(), "drm"))

		if err != nil {
			return nil, fmt.Errorf("can't read device folder: %v", err)
		}

		devices[uuid] = &DeviceInfo{
			uuid:       uuid,
			model:      "UHDGraphics",
			memory:     1024,
			cdiname:    uuid,
			deviceType: intelcrd.GpuDeviceType,
		}

		for _, drmFile := range drmFiles {
			if cardregexp.MatchString(drmFile.Name()) {
				cardidx, err := strconv.Atoi(strings.Split(drmFile.Name(), "card")[1])
				if err != nil {
					klog.Error("failed to parse cardN device: %v, skipping", drmFile)
					continue
				}
				devices[uuid].cardidx = cardidx
				klog.V(5).Infof("%v's card id: %d", f.Name(), cardidx)
			} else if renderdregexp.MatchString(drmFile.Name()) {
				renderdidx, err := strconv.Atoi(strings.Split(drmFile.Name(), "renderD")[1])
				if err != nil {
					klog.Error("failed to parse renderDN device: %v, skipping", drmFile)
					continue
				}
				devices[uuid].renderdidx = renderdidx
				klog.V(5).Infof("%v's renderd id: %d", f.Name(), renderdidx)
			}
		}
	}
	return devices, nil
}
