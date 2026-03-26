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
	"sort"
	"strconv"

	"github.com/google/uuid"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/helpers"
)

const (
	// Uninitialized renderDidx is zero, real renderD index starts with 128.
	NoRenderDev        = "renderD0"
	meiGSCDriverName   = "mei_gsc_proxy"
	meiLBDriverName    = "mei_lb"
	meiHDCPDriverName  = "mei_hdcp"
	meiPXPDriverName   = "mei_pxp"
	meiAuxDriverName   = "mei_gsc"
	xeGSCFIIdx         = uint64(1024)
	i915GSCIdx         = uint64(2304)
	i915HostPCIAddress = "0000:00:16.0"
	xeGSCFIClientCount = 2
	i915GSCClientCount = 2
	i915GSCFICount     = 1
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
	if err := fakeGpuDRIDevices(devfsRoot, cardName, renderdName, realDevices); err != nil {
		return fmt.Errorf("creating fake devfs: %v", err)
	}

	return createDevfsSymlinks(devfsRoot, cardName, renderdName, gpu.PCIAddress)
}

func fakeGpuMEI(sysfsRoot string, devfsRoot string, gpu *device.DeviceInfo, realDevices bool) error {
	if gpu.MEIName == "" {
		return nil
	}

	driverUUIDs := makeMEIDriverUUIDs()

	switch gpu.Driver {
	case device.SysfsXeDriverName:
		return fakeGpuMEIAux(sysfsRoot, devfsRoot, gpu, realDevices, gpu.MEIName, driverUUIDs,
			fmt.Sprintf("xe.mei-gscfi.%d", xeGSCFIIdx),
			map[int]string{
				0: meiGSCDriverName,
				1: meiLBDriverName,
			},
			nil,
		)
	case device.SysfsI915DriverName:
		if err := fakeGpuMEIAux(sysfsRoot, devfsRoot, gpu, realDevices, gpu.MEIName, driverUUIDs,
			fmt.Sprintf("i915.mei-gsc.%d", i915GSCIdx),
			map[int]string{
				0: meiHDCPDriverName,
				1: meiPXPDriverName,
			},
			map[int]string{
				0: meiHDCPDriverName,
				1: meiPXPDriverName,
			},
		); err != nil {
			return err
		}

		return fakeGpuMEIAux(sysfsRoot, devfsRoot, gpu, realDevices, gpu.MEIName, driverUUIDs,
			fmt.Sprintf("i915.mei-gscfi.%d", i915GSCIdx),
			map[int]string{
				0: meiLBDriverName,
			},
			nil,
		)
	default:
		return nil
	}
}

func fakeGpuMEIAux(sysfsRoot string, devfsRoot string, gpu *device.DeviceInfo, realDevices bool, meiName string, driverUUIDs map[string]string, auxName string, clientDrivers map[int]string, hostDrivers map[int]string) error {
	devicesDir := path.Join(sysfsRoot, "devices", gpu.PCIRoot, gpu.PCIAddress)
	auxDir := path.Join(devicesDir, auxName)
	meiDeviceDir := path.Join(auxDir, "mei", meiName)
	meiClassDir := path.Join(sysfsRoot, "class/mei")
	clientUUIDs, err := makeClientUUIDs(clientDrivers, hostDrivers, driverUUIDs)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(meiClassDir, 0750); err != nil {
		return fmt.Errorf("creating directory %v: %v", meiClassDir, err)
	}

	if err := createFakeMEIDeviceTree(meiDeviceDir, meiClassDir, auxName, meiName); err != nil {
		return err
	}
	if err := createFakeMEIAuxTree(sysfsRoot, auxDir); err != nil {
		return err
	}
	if err := createFakeMEIDevfs(devfsRoot, meiName, realDevices); err != nil {
		return err
	}

	if err := fakeGpuMEIBus(sysfsRoot, auxName, auxDir, clientUUIDs, clientDrivers, hostDrivers); err != nil {
		return err
	}

	return nil
}

func fakeGpuMEIBus(sysfsRoot string, auxName string, auxDir string, clientUUIDs []string, clientDrivers map[int]string, hostDrivers map[int]string) error {
	driverNames := collectMEIDriverNames(clientDrivers, hostDrivers)
	busMEIDevicesDir, busMEIDriversDir, err := createFakeMEIBusScaffolding(sysfsRoot, driverNames)
	if err != nil {
		return err
	}

	if err := createFakeMEIClientEntries(sysfsRoot, auxName, auxDir, busMEIDevicesDir, busMEIDriversDir, clientUUIDs, clientDrivers); err != nil {
		return err
	}

	if err := createFakeMEIHostEntries(sysfsRoot, busMEIDriversDir, clientUUIDs, hostDrivers); err != nil {
		return err
	}

	return nil
}

func createFakeMEIDeviceTree(meiDeviceDir, meiClassDir, auxName, meiName string) error {
	if err := os.MkdirAll(meiDeviceDir, 0750); err != nil {
		return fmt.Errorf("creating fake mei target dir, err: %v", err)
	}

	for _, fileName := range []string{"dev", "dev_state", "fw_status", "fw_ver", "hbm_ver", "hbm_ver_drv", "kind", "trc", "tx_queue_limit", "uevent"} {
		if writeErr := helpers.WriteFile(path.Join(meiDeviceDir, fileName), ""); writeErr != nil {
			return fmt.Errorf("creating fake mei device file %v: %v", fileName, writeErr)
		}
	}

	if err := createFakePowerStateTree(meiDeviceDir); err != nil {
		return err
	}

	if err := os.Symlink(path.Join("..", "..", "..", auxName), path.Join(meiDeviceDir, "device")); err != nil {
		if !os.IsExist(err) {
			return fmt.Errorf("creating fake mei device symlink: %v", err)
		}
	}

	if err := createRelativeSymlink(meiClassDir, path.Join(meiDeviceDir, "subsystem")); err != nil {
		return fmt.Errorf("creating fake mei subsystem symlink: %v", err)
	}

	if err := createRelativeSymlink(meiDeviceDir, path.Join(meiClassDir, meiName)); err != nil {
		return fmt.Errorf("creating fake mei symlink, err: %v", err)
	}

	return nil
}

func createFakeMEIAuxTree(sysfsRoot, auxDir string) error {
	if err := createFakePowerStateTree(auxDir); err != nil {
		return err
	}

	if writeErr := helpers.WriteFile(path.Join(auxDir, "uevent"), ""); writeErr != nil {
		return fmt.Errorf("creating fake mei aux uevent: %v", writeErr)
	}

	if err := createRelativeSymlink(path.Join(sysfsRoot, "bus", "auxiliary"), path.Join(auxDir, "subsystem")); err != nil {
		return fmt.Errorf("creating fake mei aux subsystem symlink: %v", err)
	}

	if err := createRelativeSymlink(path.Join(sysfsRoot, "bus", "auxiliary", "drivers", meiAuxDriverName), path.Join(auxDir, "driver")); err != nil {
		return fmt.Errorf("creating fake mei aux driver symlink: %v", err)
	}

	if err := os.MkdirAll(path.Join(sysfsRoot, "bus", "auxiliary", "drivers", meiAuxDriverName), 0750); err != nil {
		return fmt.Errorf("creating fake auxiliary driver dir: %v", err)
	}

	if err := createFakeModuleDriverEntry(sysfsRoot, meiAuxDriverName, "auxiliary", meiAuxDriverName); err != nil {
		return fmt.Errorf("creating fake module entry for %v: %v", meiAuxDriverName, err)
	}

	return nil
}

func createFakeMEIDevfs(devfsRoot, meiName string, realDevices bool) error {
	if err := os.MkdirAll(devfsRoot, 0750); err != nil {
		return fmt.Errorf("creating fake devfs root: %v", err)
	}

	if err := createDevice(path.Join(devfsRoot, meiName), realDevices); err != nil {
		return fmt.Errorf("creating device %v: %v", meiName, err)
	}

	return nil
}

func collectMEIDriverNames(clientDrivers, hostDrivers map[int]string) map[string]struct{} {
	driverNames := map[string]struct{}{}
	for _, driverName := range clientDrivers {
		driverNames[driverName] = struct{}{}
	}
	for _, driverName := range hostDrivers {
		driverNames[driverName] = struct{}{}
	}

	return driverNames
}

func createFakeMEIBusScaffolding(sysfsRoot string, driverNames map[string]struct{}) (string, string, error) {
	busMEIDir := path.Join(sysfsRoot, "bus", "mei")
	busMEIDevicesDir := path.Join(busMEIDir, "devices")
	busMEIDriversDir := path.Join(busMEIDir, "drivers")

	dirs := []string{busMEIDevicesDir}
	for driverName := range driverNames {
		dirs = append(dirs, path.Join(busMEIDriversDir, driverName))
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0750); err != nil {
			return "", "", fmt.Errorf("creating fake mei bus dir %v: %v", dir, err)
		}
	}

	filePaths := []string{
		path.Join(busMEIDir, "drivers_autoprobe"),
		path.Join(busMEIDir, "drivers_probe"),
		path.Join(busMEIDir, "uevent"),
	}
	for driverName := range driverNames {
		filePaths = append(filePaths,
			path.Join(busMEIDriversDir, driverName, "bind"),
			path.Join(busMEIDriversDir, driverName, "unbind"),
			path.Join(busMEIDriversDir, driverName, "uevent"),
		)
	}
	for _, filePath := range filePaths {
		if writeErr := helpers.WriteFile(filePath, ""); writeErr != nil {
			return "", "", fmt.Errorf("creating fake mei bus file %v: %v", filePath, writeErr)
		}
	}

	for driverName := range driverNames {
		moduleLink := path.Join(busMEIDriversDir, driverName, "module")
		if err := createRelativeSymlink(path.Join(sysfsRoot, "module", driverName), moduleLink); err != nil {
			return "", "", fmt.Errorf("creating fake mei module symlink %v: %v", moduleLink, err)
		}
		if err := createFakeModuleDriverEntry(sysfsRoot, driverName, "mei", driverName); err != nil {
			return "", "", fmt.Errorf("creating fake module entry for %v: %v", driverName, err)
		}
	}

	return busMEIDevicesDir, busMEIDriversDir, nil
}

func createFakeMEIClientEntries(sysfsRoot, auxName, auxDir, busMEIDevicesDir, busMEIDriversDir string, clientUUIDs []string, clientDrivers map[int]string) error {
	clientIndexes := make([]int, 0, len(clientDrivers))
	for index := range clientDrivers {
		clientIndexes = append(clientIndexes, index)
	}
	sort.Ints(clientIndexes)

	for _, index := range clientIndexes {
		clientUUID := clientUUIDs[index]
		clientName := fmt.Sprintf("%s-%s", auxName, clientUUID)
		clientDir := path.Join(auxDir, clientName)
		if err := os.MkdirAll(clientDir, 0750); err != nil {
			return fmt.Errorf("creating fake mei client dir %v: %v", clientName, err)
		}

		for _, fileName := range []string{"fixed", "max_conn", "max_len", "modalias", "name", "uevent", "version", "vtag"} {
			if writeErr := helpers.WriteFile(path.Join(clientDir, fileName), ""); writeErr != nil {
				return fmt.Errorf("creating fake mei client file %v/%v: %v", clientName, fileName, writeErr)
			}
		}
		if writeErr := helpers.WriteFile(path.Join(clientDir, "uuid"), clientUUID); writeErr != nil {
			return fmt.Errorf("creating fake mei uuid for %v: %v", clientName, writeErr)
		}
		if err := createFakePowerStateTree(clientDir); err != nil {
			return err
		}
		if err := createRelativeSymlink(path.Join(sysfsRoot, "bus", "mei"), path.Join(clientDir, "subsystem")); err != nil {
			return fmt.Errorf("creating fake mei client subsystem symlink: %v", err)
		}

		driverName := clientDrivers[index]
		if err := createRelativeSymlink(path.Join(busMEIDriversDir, driverName), path.Join(clientDir, "driver")); err != nil {
			return fmt.Errorf("creating fake %v driver link: %v", driverName, err)
		}

		driverLink := path.Join(busMEIDriversDir, driverName, clientName)
		if err := createRelativeSymlink(clientDir, driverLink); err != nil {
			return fmt.Errorf("creating fake %v bus link: %v", driverName, err)
		}

		deviceLink := path.Join(busMEIDevicesDir, clientName)
		if err := createRelativeSymlink(clientDir, deviceLink); err != nil {
			return fmt.Errorf("creating fake mei bus device symlink, err: %v", err)
		}
	}

	return nil
}

func createFakeMEIHostEntries(sysfsRoot, busMEIDriversDir string, clientUUIDs []string, hostDrivers map[int]string) error {
	for index, driverName := range hostDrivers {
		clientUUID := clientUUIDs[index]
		hostClientName := fmt.Sprintf("%s-%s", i915HostPCIAddress, clientUUID)
		hostClientDir := path.Join(sysfsRoot, "devices", "pci0000:00", i915HostPCIAddress, hostClientName)
		if err := os.MkdirAll(hostClientDir, 0750); err != nil {
			return fmt.Errorf("creating fake host mei client dir %v: %v", hostClientName, err)
		}
		if err := createRelativeSymlink(path.Join(busMEIDriversDir, driverName), path.Join(hostClientDir, "driver")); err != nil {
			return fmt.Errorf("creating fake host %v driver link: %v", driverName, err)
		}
		if err := createRelativeSymlink(hostClientDir, path.Join(busMEIDriversDir, driverName, hostClientName)); err != nil {
			return fmt.Errorf("creating fake host %v bus link: %v", driverName, err)
		}
	}

	return nil
}

func makeMEIDriverUUIDs() map[string]string {
	return map[string]string{
		meiGSCDriverName:  uuid.NewString(),
		meiLBDriverName:   uuid.NewString(),
		meiHDCPDriverName: uuid.NewString(),
		meiPXPDriverName:  uuid.NewString(),
	}
}

func makeClientUUIDs(clientDrivers map[int]string, hostDrivers map[int]string, driverUUIDs map[string]string) ([]string, error) {
	maxIndex := -1
	for index := range clientDrivers {
		if index > maxIndex {
			maxIndex = index
		}
	}
	for index := range hostDrivers {
		if index > maxIndex {
			maxIndex = index
		}
	}

	if maxIndex < 0 {
		return nil, fmt.Errorf("no MEI clients requested")
	}

	clientUUIDs := make([]string, maxIndex+1)
	allDrivers := map[int]string{}
	for index, driverName := range clientDrivers {
		allDrivers[index] = driverName
	}
	for index, driverName := range hostDrivers {
		allDrivers[index] = driverName
	}

	for index, driverName := range allDrivers {
		driverUUID, found := driverUUIDs[driverName]
		if !found {
			return nil, fmt.Errorf("missing MEI UUID for driver %v", driverName)
		}
		clientUUIDs[index] = driverUUID
	}

	return clientUUIDs, nil
}

func createFakeModuleDriverEntry(sysfsRoot, moduleName, busName, driverName string) error {
	moduleDir := path.Join(sysfsRoot, "module", moduleName)

	for _, subdir := range []string{"drivers", "holders", "notes", "sections"} {
		dir := path.Join(moduleDir, subdir)
		if err := os.MkdirAll(dir, 0750); err != nil {
			return fmt.Errorf("creating module directory %v: %v", dir, err)
		}
	}

	for _, fileName := range []string{"coresize", "initsize", "initstate", "refcnt", "srcversion", "taint", "uevent"} {
		if writeErr := helpers.WriteFile(path.Join(moduleDir, fileName), ""); writeErr != nil {
			return fmt.Errorf("creating module file %v/%v: %v", moduleName, fileName, writeErr)
		}
	}

	driverLinkName := fmt.Sprintf("%s:%s", busName, driverName)
	driverLinkPath := path.Join(moduleDir, "drivers", driverLinkName)
	driverPath := path.Join(sysfsRoot, "bus", busName, "drivers", driverName)
	if err := createRelativeSymlink(driverPath, driverLinkPath); err != nil {
		return fmt.Errorf("creating module driver symlink %v: %v", driverLinkPath, err)
	}

	return nil
}

func createFakePowerStateTree(baseDir string) error {
	powerDir := path.Join(baseDir, "power")
	if err := os.MkdirAll(powerDir, 0750); err != nil {
		return fmt.Errorf("creating power directory %v: %v", powerDir, err)
	}

	for _, fileName := range []string{
		"async",
		"autosuspend_delay_ms",
		"control",
		"runtime_active_kids",
		"runtime_active_time",
		"runtime_enabled",
		"runtime_status",
		"runtime_suspended_time",
		"runtime_usage",
	} {
		if writeErr := helpers.WriteFile(path.Join(powerDir, fileName), ""); writeErr != nil {
			return fmt.Errorf("creating fake power file %v/%v: %v", baseDir, fileName, writeErr)
		}
	}

	return nil
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

func createDevfsSymlinks(devfsRoot, cardName, renderdName, pciAddress string) error {
	if err := os.Symlink(fmt.Sprintf("../%v", cardName), path.Join(devfsRoot, "dri/by-path/", fmt.Sprintf("pci-%v-card", pciAddress))); err != nil {
		return fmt.Errorf("creating fake sysfs, err: %v", err)
	}

	if renderdName != NoRenderDev { // some GPUs do not have render device
		if err := os.Symlink(fmt.Sprintf("../%v", renderdName), path.Join(devfsRoot, "dri/by-path/", fmt.Sprintf("pci-%v-render", pciAddress))); err != nil {
			return fmt.Errorf("creating renderD symlink, err: %v", err)
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
		if gpu.PCIRoot == "" {
			gpu.PCIRoot = "pci0000:00"
		}
		if err := fakePCIDeviceSymlink(sysfsRoot, gpu.PCIRoot, gpu.PCIAddress); err != nil {
			return fmt.Errorf("creating fake sysfs PCI devices symlinks, err: %v", err)
		}

		// driver setup
		pciDriverDir := path.Join(sysfsRoot, "bus/pci/drivers", gpu.Driver)
		driverDeviceDir := path.Join(pciDriverDir, gpu.PCIAddress)
		if err := os.MkdirAll(pciDriverDir, 0750); err != nil {
			return fmt.Errorf("creating fake sysfs PCI driver dir, err: %v", err)
		}

		linkDst := fmt.Sprintf("../../../../devices/%v/%v", gpu.PCIRoot, gpu.PCIAddress)
		if err := os.Symlink(linkDst, driverDeviceDir); err != nil {
			return fmt.Errorf("creating fake sysfs PCI driver device symlink to PCI device, err: %v", err)
		}

		if writeErr := helpers.WriteFile(path.Join(driverDeviceDir, "device"), gpu.Model); writeErr != nil {
			return fmt.Errorf("creating fake sysfs driver device contents, err: %v", writeErr)
		}

		if err := fakeGpuDRI(sysfsRoot, devfsRoot, gpu, driverDeviceDir, realDevices); err != nil {
			return fmt.Errorf("creating fake sysfs DRI devices, err: %v", err)
		}

		if gpu.DeviceType == device.GpuDeviceType {
			if err := fakeGpuMEI(sysfsRoot, devfsRoot, gpu, realDevices); err != nil {
				return fmt.Errorf("creating fake mei sysfs: %v", err)
			}
		}

		if writeErr := helpers.WriteFile(path.Join(pciDriverDir, "bind"), ""); writeErr != nil {
			return fmt.Errorf("writing PCI device file: %v", writeErr)
		}

	}

	return fakeSysfsSRIOVContents(sysfsRoot, gpus)
}

func fakeGpuDRIDevices(devfsRoot, cardName, renderdName string, real bool) error {
	devices := []string{
		path.Join(devfsRoot, "dri", cardName),
	}
	if renderdName != NoRenderDev { // some GPUs do not have render device
		devices = append(devices, path.Join(devfsRoot, "dri", renderdName))
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

	return fakeSysFsGpuDevices(sysfsRoot, devfsRoot, gpus, realDevices)
}
