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
	"log"
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

	"0xe211": {"gt0"}, // Arc Pro B60

	"0x0000": {}, // No-tile dummy to simulate discovery issues
}

// deduceHighestCardAndRenderDIndexes returns current highest DRM card and renderD indexes.
func deduceHighestCardAndRenderDIndexes(sysfsRoot string) (uint64, uint64, error) {
	highestCardIdx := uint64(0)
	highestRenderDidx := uint64(0)

	drmDir := filepath.Join(sysfsRoot, "class/drm")
	drmFiles, err := os.ReadDir(drmDir)
	if err != nil {
		return 0, 0, fmt.Errorf("cannot read sysfs DRM class dir %v: %v", drmDir, err)
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

// deduceHighestMeiIndex returns current highest MEI index.
func deduceHighestMeiIndex(sysfsRoot string) (uint64, error) {
	highestMeiIdx := uint64(0)

	meiDir := filepath.Join(sysfsRoot, "class/mei")
	meiFiles, err := os.ReadDir(meiDir)
	if err != nil {
		return 0, fmt.Errorf("cannot read sysfs MEI class dir %v: %v", meiDir, err)
	}

	for _, meiFile := range meiFiles {
		meiFileName := meiFile.Name()
		if device.MEIRegexp.MatchString(meiFileName) {
			meiIdx, err := strconv.ParseUint(meiFileName[3:], 10, 64)
			if err != nil {
				return 0, fmt.Errorf("failed to parse index of MEI device '%v', skipping", meiFileName)
			}
			if meiIdx > highestMeiIdx {
				highestMeiIdx = meiIdx
			}
		}
	}

	return highestMeiIdx, nil
}

// deduceHighestVFIOIndex returns current highest VFIO index.
func deduceHighestVFIOIndex(sysfsRoot string) (uint64, error) {
	highestVFIOIdx := uint64(0)

	vfioDir := filepath.Join(sysfsRoot, "class/vfio")
	vfioFiles, err := os.ReadDir(vfioDir)
	if err != nil {
		return 0, fmt.Errorf("cannot read sysfs VFIO class dir %v: %v", vfioDir, err)
	}

	for _, vfioFile := range vfioFiles {
		vfioFileName := vfioFile.Name()
		if device.VFIORegexp.MatchString(vfioFileName) {
			vfioIdx, err := strconv.ParseUint(vfioFileName[4:], 10, 64)
			if err != nil {
				return 0, fmt.Errorf("failed to parse index of VFIO device '%v', skipping", vfioFileName)
			}
			if vfioIdx > highestVFIOIdx {
				highestVFIOIdx = vfioIdx
			}
		}
	}

	return highestVFIOIdx, nil
}

// deduceHighestIOMMUGroupIndex returns current highest IOMMU group index.
func deduceHighestIOMMUGroupIndex(devfsRoot string) (uint64, error) {
	highestIOMMUGroupIdx := uint64(0)

	vfioDir := filepath.Join(devfsRoot, "vfio")
	vfioFiles, err := os.ReadDir(vfioDir)
	if err != nil {
		return 0, fmt.Errorf("cannot read devfs vfio dir %v: %v", vfioDir, err)
	}

	for _, vfioFile := range vfioFiles {
		vfioFileName := vfioFile.Name()
		if device.IOMMUGroupRegexp.MatchString(vfioFileName) {
			groupIdx, err := strconv.ParseUint(vfioFileName, 10, 64)
			if err != nil {
				return 0, fmt.Errorf("failed to parse index of IOMMU group '%v', skipping", vfioFileName)
			}
			if groupIdx > highestIOMMUGroupIdx {
				highestIOMMUGroupIdx = groupIdx
			}
		}
	}

	return highestIOMMUGroupIdx, nil
}

func fakeSysfsPF(gpu *device.DeviceInfo, numvfs int, i915DevDir string) error {
	if gpu.MaxVFs <= 0 {
		return nil
	}

	writeErr1 := helpers.WriteFile(path.Join(i915DevDir, "sriov_numvfs"), fmt.Sprint(numvfs))
	writeErr2 := helpers.WriteFile(path.Join(i915DevDir, "sriov_totalvfs"), fmt.Sprint(gpu.MaxVFs))
	writeErr3 := helpers.WriteFile(path.Join(i915DevDir, "sriov_drivers_autoprobe"), "1")

	if writeErr1 != nil || writeErr2 != nil || writeErr3 != nil {
		return fmt.Errorf("creating fake sysfs, err(s): '%v', '%v', '%v'", writeErr1, writeErr2, writeErr3)
	}

	prelimIovDir := path.Join(i915DevDir, "drm", gpu.CardName, "prelim_iov")
	pfDir := path.Join(prelimIovDir, "pf")
	if err := os.MkdirAll(pfDir, 0750); err != nil {
		return fmt.Errorf("creating fake sysfs, err: %v", err)
	}

	if writeErr := helpers.WriteFile(path.Join(pfDir, "auto_provisioning"), "1"); writeErr != nil {
		return fmt.Errorf("creating fake sysfs, err: %v", writeErr)
	}

	return createFakeSysfsForVFs(prelimIovDir, gpu)
}

func createFakeSysfsForVFs(prelimIovDir string, gpu *device.DeviceInfo) error {
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

func fakeGpuDRI(sysfsRoot, devfsRoot, pciAddress, cardName, renderDName string, realDevices bool) error {
	if cardName == "" {
		return fmt.Errorf("CardName must be specified for DRI-bound GPUs")
	}

	pciDeviceDir := path.Join(sysfsRoot, device.SysfsPCIDevicesPath, pciAddress)
	if err := os.MkdirAll(path.Join(pciDeviceDir, "drm", cardName), 0750); err != nil {
		return fmt.Errorf("creating fake sysfs, err: %v", err)
	}
	if renderDName != "" { // some GPUs do not have render device
		if err := os.MkdirAll(path.Join(pciDeviceDir, "drm", renderDName), 0750); err != nil {
			return fmt.Errorf("creating fake sysfs, err: %v", err)
		}
	}

	// DRM setup
	drmDirLinkSource := path.Join(sysfsRoot, "class/drm", cardName)
	drmDirLinkTarget := path.Join(pciDeviceDir, "drm", cardName)
	if err := os.Symlink(drmDirLinkTarget, drmDirLinkSource); err != nil {
		return fmt.Errorf("creating fake sysfs, err: %v", err)
	}

	// devfs setup
	return fakeGpuDRIDevices(devfsRoot, cardName, renderDName, pciAddress, realDevices)
}

func fakeGpuMEI(sysfsRoot, devfsRoot, pciRoot, pciAddress, meiName, driverName string, realDevices bool) error {
	if meiName == "" {
		return nil
	}

	meiClassDir := path.Join(sysfsRoot, device.SysfsMEIpath)
	if err := os.MkdirAll(meiClassDir, 0750); err != nil {
		return fmt.Errorf("creating MEI class directory %v: %v", meiClassDir, err)
	}

	auxDirName := "mei"
	switch driverName {
	case device.SysfsI915DriverName:
		auxDirName = "i915.mei-gscfi.2304"
	case device.SysfsXeDriverName:
		auxDirName = "xe.mei-gscfi.768"
	}

	meiDeviceDir := path.Join(sysfsRoot, "devices", pciRoot, pciAddress, auxDirName, "mei", meiName)
	if err := os.MkdirAll(meiDeviceDir, 0750); err != nil {
		return fmt.Errorf("creating MEI device directory %v: %v", meiDeviceDir, err)
	}

	meiClassLink := path.Join(meiClassDir, meiName)
	if err := createRelativeSymlink(meiDeviceDir, meiClassLink); err != nil {
		return fmt.Errorf("creating MEI class symlink %v: %v", meiClassLink, err)
	}

	if err := os.MkdirAll(devfsRoot, 0750); err != nil {
		return fmt.Errorf("creating fake devfs root: %v", err)
	}

	if err := createDevice(path.Join(devfsRoot, meiName), realDevices); err != nil {
		return fmt.Errorf("creating device %v: %v", meiName, err)
	}

	return nil
}

func fakeGpuVFIO(sysfsRoot, devfsRoot, vfioDevice, iommuGroup, pciAddress string, realDevices bool) error {
	if vfioDevice == "" || iommuGroup == "" {
		return fmt.Errorf("VFIO device and IOMMU group must be specified for VFIO-bound GPUs")
	}

	pciDeviceDir := path.Join(sysfsRoot, device.SysfsPCIDevicesPath, pciAddress)
	pciDeviceVfioDevDir := path.Join(pciDeviceDir, "vfio-dev", vfioDevice)
	log.Printf("Creating fake VFIO device %v\n", pciDeviceVfioDevDir)
	if err := os.MkdirAll(pciDeviceVfioDevDir, 0750); err != nil {
		return fmt.Errorf("creating fake sysfs, err: %v", err)
	}

	iommuDirLinkSource := path.Join(path.Join(pciDeviceDir, "iommu"))
	iommuDirLinkTarget := "../../virtual/iommu/dmar1"
	log.Printf("creating iommu link %v", iommuDirLinkSource)
	if err := os.Symlink(iommuDirLinkTarget, iommuDirLinkSource); err != nil {
		return fmt.Errorf("creating fake sysfs, err: %v", err)
	}

	iommuGroupDirLinkSource := path.Join(path.Join(pciDeviceDir, "iommu_group"))
	iommuGroupDirLinkTarget := fmt.Sprintf("../../../kernel/iommu_groups/%v", iommuGroup)
	if err := os.Symlink(iommuGroupDirLinkTarget, iommuGroupDirLinkSource); err != nil {
		return fmt.Errorf("creating fake sysfs, err: %v", err)
	}

	// VFIO setup
	sysfsDevicesVirtualVFIODir := path.Join(sysfsRoot, "devices/virtual/vfio", vfioDevice)
	if err := os.MkdirAll(sysfsDevicesVirtualVFIODir, 0750); err != nil {
		return fmt.Errorf("creating directory %v: %v", sysfsDevicesVirtualVFIODir, err)
	}

	vfioDirLinkSource := path.Join(sysfsRoot, "class/vfio", vfioDevice)
	vfioDirLinkTarget := fmt.Sprintf("../../devices/virtual/vfio/%v", vfioDevice)
	if err := os.Symlink(vfioDirLinkTarget, vfioDirLinkSource); err != nil {
		return fmt.Errorf("creating fake sysfs, err: %v", err)
	}

	// devfs setup
	return fakeGpuVFIODevices(devfsRoot, vfioDevice, iommuGroup, realDevices)
}

func createRelativeSymlink(targetPath string, linkPath string) error {
	relTarget, err := filepath.Rel(path.Dir(linkPath), targetPath)
	if err != nil {
		return fmt.Errorf("failed to resolve relative symlink from %v to %v: %v", linkPath, targetPath, err)
	}

	if err := os.Symlink(relTarget, linkPath); err != nil {
		if os.IsExist(err) {
			return nil
		}
		return fmt.Errorf("creating symlink from %v to %v: %v", linkPath, targetPath, err)
	}

	return nil
}

func createDevfsBypathSymlinks(devfsRoot, cardName, renderdName, pciAddress string) error {
	if err := os.Symlink(fmt.Sprintf("../%v", cardName), path.Join(devfsRoot, "dri/by-path/", fmt.Sprintf("pci-%v-card", pciAddress))); err != nil {
		return fmt.Errorf("creating fake sysfs, err: %v", err)
	}

	if renderdName != "" { // some GPUs do not have render device
		if err := os.Symlink(fmt.Sprintf("../%v", renderdName), path.Join(devfsRoot, "dri/by-path/", fmt.Sprintf("pci-%v-render", pciAddress))); err != nil {
			return fmt.Errorf("creating renderD symlink, err: %v", err)
		}
	}

	return nil

}

// fakeSysFsGpuDevices creates PCI and DRM devices layout in existing fake sysfsRoot.
// This will be called when fake sysfs is being created and when more devices added
// to existing fake sysfs.
func fakeSysFsGpuDevices(sysfsRoot string, devfsRoot string, gpus device.DevicesInfo, realDevices bool) error { // nolint:cyclop
	for _, gpu := range gpus {
		if gpu.PCIAddress == "" {
			gpu.PCIAddress, _ = helpers.PciInfoFromDeviceUID(gpu.UID)
		}
		if gpu.PCIRoot == "" {
			gpu.PCIRoot = "pci0000:00"
		}
		if err := fakePCIDeviceSymlink(sysfsRoot, gpu.PCIRoot, gpu.PCIAddress); err != nil {
			return fmt.Errorf("creating fake sysfs PCI devices symlinks, err: %v", err)
		}

		pciDeviceDir := path.Join(sysfsRoot, device.SysfsPCIDevicesPath, gpu.PCIAddress)
		fileWrites := map[string]string{
			path.Join(pciDeviceDir, "device"):          gpu.Model,
			path.Join(pciDeviceDir, "vendor"):          device.PCIVendorId,
			path.Join(pciDeviceDir, "class"):           device.PCIVGAClassID,
			path.Join(pciDeviceDir, "driver_override"): "",
		}
		for filePath, content := range fileWrites {
			if writeErr := helpers.WriteFile(filePath, content); writeErr != nil {
				return fmt.Errorf("creating fake sysfs file %v content, err: %v", filePath, writeErr)
			}
		}

		// driver setup
		if gpu.CurrentDriver != "" {
			if err := fakeDeviceBinding(sysfsRoot, devfsRoot, gpu.CurrentDriver, gpu.PCIRoot, gpu.PCIAddress); err != nil {
				return fmt.Errorf("creating fake sysfs PCI driver binding, err: %v", err)
			}

			if gpu.IsDRMBound() {
				if err := fakeGpuDRI(sysfsRoot, devfsRoot, gpu.PCIAddress, gpu.CardName, gpu.RenderDName, realDevices); err != nil {
					return fmt.Errorf("creating fake sysfs DRI devices, err: %v", err)
				}
				if gpu.DeviceType == device.GpuDeviceType {
					if err := fakeGpuMEI(sysfsRoot, devfsRoot, gpu.PCIRoot, gpu.PCIAddress, gpu.MEIName, gpu.CurrentDriver, realDevices); err != nil {
						return fmt.Errorf("creating fake mei sysfs: %v", err)
					}
				}
			} else if gpu.IsVFIOBound() {
				if err := fakeGpuVFIO(sysfsRoot, devfsRoot, gpu.VFIODevice, gpu.IOMMUGroup, gpu.PCIAddress, realDevices); err != nil {
					return fmt.Errorf("creating fake sysfs VFIO devices, err: %v", err)
				}
			}
		}
	}

	return fakeSysfsSRIOVContents(sysfsRoot, gpus)
}

func fakeGpuDRIDevices(devfsRoot, cardName, renderdName, pciAddress string, real bool) error {
	devices := []string{
		path.Join(devfsRoot, "dri", cardName),
	}
	if renderdName != "" { // some GPUs do not have render device
		devices = append(devices, path.Join(devfsRoot, "dri", renderdName))
	}

	for _, device := range devices {
		if err := createDevice(device, real); err != nil {
			return fmt.Errorf("creating device: %v", err)
		}
	}

	return createDevfsBypathSymlinks(devfsRoot, cardName, renderdName, pciAddress)
}

func fakeGpuVFIODevices(devfsRoot, deviceName, iommuGroupName string, real bool) error {
	if deviceName == "" || iommuGroupName == "" {
		return fmt.Errorf("deviceName and iommuGroupName must be provided for fakeGpuVFIODevices")
	}
	devices := []string{
		path.Join(devfsRoot, "vfio", iommuGroupName),
		path.Join(devfsRoot, "vfio/vfio"),
		path.Join(devfsRoot, "vfio/devices", deviceName),
	}

	for _, device := range devices {
		if err := createDevice(device, real); err != nil {
			return fmt.Errorf("creating device: %v", err)
		}
	}
	return nil
}

func FakeSysFsGpuContents(sysfsRoot string, devfsRoot string, gpus device.DevicesInfo, realDevices bool) error {
	if err := sanitizeFakeSysFsDir(sysfsRoot); err != nil {
		return err
	}

	// Pre-create all supported drivers' and classes dirs.
	drivers := []string{device.SysfsI915DriverName, device.SysfsXeDriverName, device.SysfsVFIODriverName, device.SysfsXeVFIODriverName}
	for _, driver := range drivers {
		if err := os.MkdirAll(path.Join(sysfsRoot, "bus/pci/drivers", driver), 0750); err != nil {
			return fmt.Errorf("creating fake sysfs PCI driver dir, err: %v", err)
		}
		for _, action := range []string{"bind", "unbind"} {
			actionFile := path.Join(sysfsRoot, "bus/pci/drivers", driver, action)
			if err := helpers.WriteFile(actionFile, ""); err != nil {
				return fmt.Errorf("creating fake sysfs PCI driver action file %v, err: %v", actionFile, err)
			}
		}
		for _, class := range []string{"drm", "vfio", "mei"} {
			classDir := path.Join(sysfsRoot, "class", class)
			if err := os.MkdirAll(classDir, 0750); err != nil {
				return fmt.Errorf("creating fake sysfs class dir %v, err: %v", classDir, err)
			}
		}
		for _, devfsDir := range []string{"dri/by-path", "vfio/devices"} {
			if err := os.MkdirAll(path.Join(devfsRoot, devfsDir), 0750); err != nil {
				return fmt.Errorf("creating fake devfs dir %v, err: %v", devfsDir, err)
			}
		}
	}

	// Create device-specific contents.
	return fakeSysFsGpuDevices(sysfsRoot, devfsRoot, gpus, realDevices)
}

// Create
// /sys/bus/pci/drivers/<driver>/<pciAddrStr> -> /sys/devices/<pciRoot>/<pciAddrStr>
// /sys/bus/pci/devices/<pciAddrStr>/driver -> /sys/bus/pci/drivers/<driver> .
func fakeDeviceBinding(sysfsRoot, devfsRoot, driverName, pciRoot, pciAddrStr string) error {
	// driver setup
	pciDriverDir := path.Join(sysfsRoot, "bus/pci/drivers", driverName)
	driverDeviceDir := path.Join(pciDriverDir, pciAddrStr)

	driverDeviceLinkDst := fmt.Sprintf("../../../../devices/%v/%v", pciRoot, pciAddrStr)
	if err := os.Symlink(driverDeviceLinkDst, driverDeviceDir); err != nil {
		return fmt.Errorf("creating fake sysfs PCI driver device symlink to PCI device, err: %v", err)
	}

	driverLinkDst := fmt.Sprintf("../../../bus/pci/drivers/%v", driverName)
	driverLinkSrc := path.Join(driverDeviceDir, "driver")
	if err := os.Symlink(driverLinkDst, driverLinkSrc); err != nil {
		return fmt.Errorf("creating fake sysfs device driver symlink, err: %v", err)
	}

	return nil
}
