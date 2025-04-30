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

package fakesysfs

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/helpers"
)

var perDeviceIdTilesDirs = map[string][]string{
	"0x56c0": {"gt"}, // Flex170
	"0x56c1": {"gt"}, // Flex140

	"0x0b69": {"gt0", "gt1"}, // Ponte Vecchio XL (2 Tile) [Data Center GPU Max 1450]
	"0x0bd0": {"gt0", "gt1"}, // Ponte Vecchio XL (2 Tile)
	"0x0bd5": {"gt0", "gt1"}, // Ponte Vecchio XT (2 Tile) [Data Center GPU Max 1550]
	"0x0bd6": {"gt0", "gt1"}, // Ponte Vecchio XT (2 Tile) [Data Center GPU Max 1550]

	"0x0bd9": {"gt0"}, // Ponte Vecchio XT (1 Tile) [Data Center GPU Max 1100] 48G
	"0x0bda": {"gt0"}, // Ponte Vecchio XT (1 Tile) [Data Center GPU Max 1100] 48G
	"0x0bdb": {"gt0"}, // Ponte Vecchio XT (1 Tile) [Data Center GPU Max 1100] 48G
	"0x0000": {},      // No-tile dummy to simulate discovery issues
}

// deduceHighestCardAndRenderDIndexes returns current highest DRM card and renderD indexes.
func deduceHighestCardAndRenderDIndexes(fakeSysfsRoot string) (uint64, uint64, error) {
	highestCardIdx := uint64(0)
	highestRenderDidx := uint64(0)

	drmDir := filepath.Join(fakeSysfsRoot, "class/drm")
	drmFiles, err := os.ReadDir(drmDir)
	if err != nil { // ignore this device
		return 0, 0, fmt.Errorf("cannot read device folder %v: %v", drmDir, err)
	}

	for _, drmFile := range drmFiles {
		drmFileName := drmFile.Name()
		if device.CardRegexp.MatchString(drmFileName) {
			cardIdx, err := strconv.ParseUint(drmFileName[4:], 10, 64)
			if err != nil {
				return 0, 0, fmt.Errorf("failed to parse index of DRM card device '%v', skipping", drmFileName)
			}
			if cardIdx > highestCardIdx {
				highestCardIdx = cardIdx
			}
		} else if device.RenderdRegexp.MatchString(drmFileName) {
			renderDidx, err := strconv.ParseUint(drmFileName[7:], 10, 64)
			if err != nil {
				return 0, 0, fmt.Errorf("failed to parse renderDN device: %v, skipping", drmFileName)
			}
			if renderDidx > highestRenderDidx {
				highestRenderDidx = renderDidx
			}
		}
	}

	return highestCardIdx, highestRenderDidx, nil
}

func fakeSysfsPF(deviceUID string, gpu *device.DeviceInfo, numvfs int, i915DevDir string) error {
	if gpu.MaxVFs <= 0 {
		return nil
	}

	writeErr1 := helpers.WriteFile(path.Join(i915DevDir, "sriov_numvfs"), fmt.Sprint(numvfs))
	writeErr2 := helpers.WriteFile(path.Join(i915DevDir, "sriov_totalvfs"), fmt.Sprint(gpu.MaxVFs))
	writeErr3 := helpers.WriteFile(path.Join(i915DevDir, "sriov_drivers_autoprobe"), "1")

	if writeErr1 != nil || writeErr2 != nil || writeErr3 != nil {
		return fmt.Errorf("creating fake sysfs, err(s): '%v', '%v', '%v'", writeErr1, writeErr2, writeErr3)
	}

	cardName := fmt.Sprintf("card%v", gpu.CardIdx)
	prelimIovDir := path.Join(i915DevDir, "drm", cardName, "prelim_iov")
	pfDir := path.Join(prelimIovDir, "pf")
	if err := os.MkdirAll(pfDir, 0750); err != nil {
		return fmt.Errorf("creating fake sysfs, err: %v", err)
	}

	if writeErr := helpers.WriteFile(path.Join(pfDir, "auto_provisioning"), "1"); writeErr != nil {
		return fmt.Errorf("creating fake sysfs, err: %v", writeErr)
	}

	for drmVFIndex := 1; drmVFIndex <= int(gpu.MaxVFs); drmVFIndex++ {
		drmVFDir := path.Join(prelimIovDir, fmt.Sprintf("vf%d", drmVFIndex))
		tileDirs, found := perDeviceIdTilesDirs[gpu.Model]
		if !found {
			return fmt.Errorf("device %v (id %v) is not in perDeviceIdTilesDirs map", gpu.UID, gpu.Model)
		}

		for _, vfTileDir := range tileDirs {
			drmVFgtDir := path.Join(drmVFDir, vfTileDir)
			if err := os.MkdirAll(drmVFgtDir, 0750); err != nil {
				return fmt.Errorf("creating fake sysfs, err: %v", err)
			}

			for _, vfAttr := range device.VfAttributeFiles {
				if writeErr := helpers.WriteFile(path.Join(drmVFgtDir, vfAttr), "0"); writeErr != nil {
					return fmt.Errorf("creating fake sysfs, err: %v", writeErr)
				}
			}
		}
	}

	return nil
}

func fakeGpuDRI(sysfsRoot string, devfsRoot string, gpu *device.DeviceInfo, i915DevDir string, realDevices bool) error {

	cardName := fmt.Sprintf("card%v", gpu.CardIdx)
	renderdName := fmt.Sprintf("renderD%v", gpu.RenderdIdx)
	if err := os.MkdirAll(path.Join(i915DevDir, "drm", cardName), 0750); err != nil {
		return fmt.Errorf("creating fake sysfs, err: %v", err)
	}
	if gpu.RenderdIdx != 0 { // some GPUs do not have render device
		if err := os.MkdirAll(path.Join(i915DevDir, "drm", renderdName), 0750); err != nil {
			return fmt.Errorf("creating fake sysfs, err: %v", err)
		}
	}

	// DRM setup
	sysfsDRMClassDir := path.Join(sysfsRoot, "class/drm")
	if err := os.MkdirAll(sysfsDRMClassDir, 0750); err != nil {
		return fmt.Errorf("creating directory %v: %v", sysfsDRMClassDir, err)
	}

	drmDirLinkSource := path.Join(sysfsDRMClassDir, cardName)
	drmDirLinkTarget := path.Join(i915DevDir, "drm", cardName)

	if err := os.Symlink(drmDirLinkTarget, drmDirLinkSource); err != nil {
		return fmt.Errorf("creating fake sysfs, err: %v", err)
	}

	localMemoryStr := fmt.Sprint(gpu.MemoryMiB * 1024 * 1024)
	if writeErr := helpers.WriteFile(path.Join(drmDirLinkTarget, "lmem_total_bytes"), localMemoryStr); writeErr != nil {
		return fmt.Errorf("creating fake sysfs, err: %v", writeErr)
	}

	if err := os.MkdirAll(path.Join(devfsRoot, "dri/by-path"), 0750); err != nil {
		return fmt.Errorf("creating card symlink, err: %v", err)
	}

	// devfs setup
	if realDevices {
		if err := fakeGpuDRIDeviceFiles(devfsRoot, cardName, renderdName); err != nil {
			return fmt.Errorf("creating fake devfs: %v", err)
		}
	} else {
		if err := fakeGpuDRIPlainFiles(devfsRoot, cardName, renderdName); err != nil {
			return fmt.Errorf("creating fake devfs: %v", err)
		}
	}

	return createDevfsSymlinks(devfsRoot, cardName, renderdName, gpu.PCIAddress)
}

func createDevfsSymlinks(devfsRoot, cardName, renderdName, pciAddress string) error {
	if err := os.Symlink(fmt.Sprintf("../%v", cardName), path.Join(devfsRoot, "dri/by-path/", fmt.Sprintf("pci-%v-card", pciAddress))); err != nil {
		return fmt.Errorf("creating fake sysfs, err: %v", err)
	}

	if renderdName != "renderD0" { // some GPUs do not have render device
		if err := os.Symlink(fmt.Sprintf("../%v", renderdName), path.Join(devfsRoot, "dri/by-path/", fmt.Sprintf("pci-%v-render", pciAddress))); err != nil {
			return fmt.Errorf("creating renderD symlink, err: %v", err)
		}
	}

	return nil

}

func fakeGpuDRIDeviceFiles(devfsRoot, cardName, renderdName string) error {
	if err := createDevice(path.Join(devfsRoot, "dri", cardName)); err != nil {
		return fmt.Errorf("creating card device, err: %v", err)
	}

	if renderdName != "renderD0" { // some GPUs do not have render device
		if err := createDevice(path.Join(devfsRoot, "dri", renderdName)); err != nil {
			return fmt.Errorf("creating renderD device, err: %v", err)
		}
	}

	return nil
}

func fakeGpuDRIPlainFiles(devfsRoot, cardName, renderdName string) error {
	if err := helpers.WriteFile(path.Join(devfsRoot, "dri", cardName), ""); err != nil {
		return fmt.Errorf("creating card text file, err: %v", err)
	}

	if renderdName != "renderD0" { // some GPUs do not have render device
		if err := helpers.WriteFile(path.Join(devfsRoot, "dri", renderdName), ""); err != nil {
			return fmt.Errorf("creating renderD text file, err: %v", err)
		}
	}

	return nil
}

// fakeSysFsGpuDevices creates PCI and DRM devices layout in existing fake sysfsRoot.
// This will be called when fake sysfs is being created and when more devices added
// to existing fake sysfs.
func fakeSysFsGpuDevices(sysfsRoot string, devfsRoot string, gpus device.DevicesInfo, realDevices bool) error {
	for _, gpu := range gpus {
		if gpu.PCIAddress == "" {
			gpu.PCIAddress, _ = helpers.PciInfoFromDeviceUID(gpu.UID)
		}

		// driver setup
		driverDeviceDir := path.Join(sysfsRoot, "bus/pci/drivers", gpu.Driver, gpu.PCIAddress)
		if err := os.MkdirAll(driverDeviceDir, 0750); err != nil {
			return fmt.Errorf("creating fake sysfs, err: %v", err)
		}

		if writeErr := helpers.WriteFile(path.Join(driverDeviceDir, "device"), gpu.Model); writeErr != nil {
			return fmt.Errorf("creating fake sysfs, err: %v", writeErr)
		}

		if err := fakeGpuDRI(sysfsRoot, devfsRoot, gpu, driverDeviceDir, realDevices); err != nil {
			return err
		}
	}

	return fakeSysfsSRIOVContents(sysfsRoot, gpus)
}

func FakeSysFsGpuContents(sysfsRoot string, devfsRoot string, gpus device.DevicesInfo, realDevices bool) error {
	if err := sanitizeFakeSysFsDir(sysfsRoot); err != nil {
		return err
	}

	return fakeSysFsGpuDevices(sysfsRoot, devfsRoot, gpus, realDevices)
}
