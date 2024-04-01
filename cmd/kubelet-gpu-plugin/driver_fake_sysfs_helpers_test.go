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

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/fsnotify/fsnotify"
	intelcrd "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gpu/v1alpha2/api"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/sriov"
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

func writeTestFile(t *testing.T, filePath string, fileContents string) {
	fhandle, err := os.Create(filePath)
	if err != nil {
		t.Errorf("could not create test file %v: %v", filePath, err)
		return
	}

	if _, err = fhandle.WriteString(fileContents); err != nil {
		t.Errorf("could not write to test file %v: %v", filePath, err)
		return
	}

	if err := fhandle.Close(); err != nil {
		t.Errorf("could not close file %v: %v", filePath, err)
	}
}

func countVFs(devices DevicesInfo) map[string]int {
	perDeviceNumvfs := map[string]int{}
	for deviceUID, device := range devices {
		if device.DeviceType == intelcrd.VfDeviceType {
			perDeviceNumvfs[deviceUID] += 1
		}
	}
	return perDeviceNumvfs
}

func removeFakeVFDRM(t *testing.T, sysfsI915DeviceDir string) {
	deviceDRMDir := filepath.Join(sysfsI915DeviceDir, "drm")
	drmFiles, err := os.ReadDir(deviceDRMDir)

	if err != nil {
		t.Errorf("cannot read device folder %v: %v", deviceDRMDir, err)
		return
	}

	// sysfsI915DeviceDir in here is, for example, this:
	// /tmp/fakesysfs/bus/pci/drivers/i915/0000:00:00.0/
	// getting /tmp/fakesysfs takes five times parent dir.
	fakesysfsRoot := path.Dir(path.Dir(path.Dir(path.Dir(path.Dir(sysfsI915DeviceDir)))))

	for _, drmFile := range drmFiles {
		drmFileName := drmFile.Name()
		if cardRegexp.MatchString(drmFileName) {
			drmDir := path.Join(fakesysfsRoot, "class/drm/", drmFileName)
			t.Logf("deleting DRM dir %v", drmDir)
			if err := os.RemoveAll(drmDir); err != nil {
				t.Fatalf("could not cleanup fake VF DRM dir %v: %v", drmDir, err)
			}

			return
		}
	}
}

// removeFakeVFsOnParent imitates i915's deletion of PCI and DRM devices for
// SR-IOV VFs in fake sysfs, where there is no real i915 is running. It takes
// full sriov_numvfs file path as an argument.
//
// WARNING:
// For now, the sysfs files are plain files, and failure to write wrong values
// cannot be faked, as well as multiple writes to fake sysfs files do not
// overwrite previous values, but get appended to the end. Fake syfs needs
// to be re-created or be different for every testcase when fake-sysfs watcher
// is used, especially with loop-based test functions that have many scenarios
// in them.
func removeFakeVFsOnParent(t *testing.T, numvfsFilePath string) {
	// Find VF symlinks in PF.
	sysfsI915DeviceDir := path.Dir(numvfsFilePath)
	sysfsI915Dir := path.Dir(sysfsI915DeviceDir)
	virtfnsPattern := filepath.Join(sysfsI915DeviceDir, "virtfn*")
	files, _ := filepath.Glob(virtfnsPattern)

	for _, virtfn := range files {
		t.Logf("checking %v", virtfn)
		virtfnTarget, err := os.Readlink(virtfn)
		if err != nil {
			t.Errorf("failed reading virtfn symlink %v: %v. Skipping", virtfn, err)
			continue
		}

		// ../0000:00:02.1  # 15 chars
		if len(virtfnTarget) != 15 {
			t.Errorf("symlink target does not match expected length: %v", virtfnTarget)
			continue
		}

		sysfsI915VFDir := filepath.Join(sysfsI915Dir, virtfnTarget[3:])

		// Cleanup DRM files from separate directory hierarchy.
		removeFakeVFDRM(t, sysfsI915VFDir)

		// Cleanup PCI device.
		if err := os.RemoveAll(sysfsI915VFDir); err != nil {
			t.Fatalf("could not cleanup fake VF PCI dir %v: %v", sysfsI915VFDir, err)
		}

		// Finally cleanup VF symlink.
		if err := os.Remove(virtfn); err != nil {
			t.Fatalf("could not cleanup fake VF symlink %v: %v", virtfn, err)
		}
	}
}

// addFakeVFsOnParent imitates i915's creation of PCI and DRM devices for SR-IOV
// VFs in fake sysfs, where there is no real i915 is running. It takes full path
// to the sriov_numvfs file, and a number of VFs requested.
//
// WARNING:
// For now, the sysfs files are plain files, and failure to write wrong values
// cannot be faked, as well as multiple writes to fake sysfs files do not
// overwrite previous values, but get appended to the end. Fake syfs needs
// to be re-created or be different for every testcase when fake-sysfs watcher
// is used, especially with loop-based test functions that have many scenarios
// in them.
func addFakeVFsOnParent(t *testing.T, numvfsFilePath string, numVFs uint64) {
	sysfsI915DeviceDir := path.Dir(numvfsFilePath)
	parentDBDF := path.Base(sysfsI915DeviceDir)
	sysfsI915Dir := path.Dir(sysfsI915DeviceDir)
	fakeSysfsRoot := path.Join(sysfsI915Dir, "../../../../")
	t.Logf("fake sysfs root: %v", fakeSysfsRoot)

	// get GPU model
	modelFilePath := path.Join(sysfsI915DeviceDir, "device")
	modelBytes, err := os.ReadFile(modelFilePath)
	if err != nil {
		t.Errorf("could not read fake sysfs model file %v: %v", modelFilePath, err)
		return
	}
	model := strings.TrimSpace(string(modelBytes))

	// construct parent's DRM VFs dir path
	parentCardIdx, _, err := deduceCardAndRenderdIndexes(sysfsI915DeviceDir)
	if err != nil {
		t.Errorf("could not detect drm/cardX index in %v: %v", sysfsI915DeviceDir, err)
		return
	}
	parentVFsDir := path.Join(sysfsI915DeviceDir, "drm", fmt.Sprintf("card%d", parentCardIdx), "prelim_iov")

	// check if auto_provisioning is enabled
	automatic, err := autoProvisioningEnabled(parentVFsDir)
	if err != nil {
		t.Errorf("could not detect auto_provisioning: %v", err)
		return
	}
	if automatic {
		// TODO: implement automatic provisioning, without VF profiles
		t.Log("WARNING: auto_provisioning in fake sysfs is not implemented")
	}

	// generate DeviceInfo for VFs.
	devices := DevicesInfo{}
	currentPCIdev := parentDBDF[:len(parentDBDF)-1]
	highestCardIdx, highestRenderDIdx, err := deduceHighestCardAndRenderDIndexes(fakeSysfsRoot)
	if err != nil {
		t.Errorf("could not get current DRM card and renderD devices indexes: %v", err)
		return
	}

	// PCI VF index is 0-based.
	for vfIdx := uint64(0); vfIdx < numVFs; vfIdx++ {
		t.Logf("creating object for VF %v", vfIdx)
		vfUID := ""

		//                 VF indexes in PCI addresses
		// parent DBDF [PF 0  1  2  3  4  5  6 ]  0000:00:01:[0-7]
		//   next DBDF [7  8  9  10 11 12 13 14]  0000:00:02:[0-7]
		//   next DBDF [15 16 17 18 19 ...     ]  0000:00:03:[0-7]...
		pciFunctionIdx := (vfIdx + 1) % 8
		if pciFunctionIdx == 0 {
			currentPCIdev, err = newPCIAddress(sysfsI915Dir, currentPCIdev)
			if err != nil {
				t.Errorf("could not create new PCI address for new VF: %v", err)
				return
			}
		}

		vfUID = fmt.Sprintf("%s%d-%s", currentPCIdev, pciFunctionIdx, model)

		vfMem, err := getVFMemoryAmountMiB(parentVFsDir, vfIdx)
		if err != nil {
			t.Errorf("could not get lmem_quota from VF%d on %v: %v", vfIdx, parentDBDF, err)
			return
		}

		devices[vfUID] = &DeviceInfo{
			Model:      model,
			MemoryMiB:  vfMem,
			DeviceType: "vf",
			CardIdx:    highestCardIdx + vfIdx + 1,
			RenderdIdx: highestRenderDIdx + vfIdx + 1,
			UID:        vfUID,
			VFIndex:    vfIdx,
			ParentUID:  fmt.Sprintf("%s-%s", parentDBDF, model),
		}
	}

	if err := fakeSysFsDevices(t, fakeSysfsRoot, devices); err != nil {
		t.Error("creating new VFs")
	}
}

// newPCIAddress finds next available free PCI address in given directory.
// Returns partial PCI address without function, "0000:00:00.", used in loop
// when fake VFs are generated.
func newPCIAddress(sysfsI915Dir string, currentAddress string) (string, error) {
	domain, err1 := strconv.ParseUint(currentAddress[:4], 10, 64)
	bus, err2 := strconv.ParseUint(currentAddress[5:7], 10, 64)
	device, err3 := strconv.ParseUint(currentAddress[8:10], 10, 64)

	if err1 != nil || err2 != nil || err3 != nil {
		return "", fmt.Errorf("could not parse current PCI address %v", currentAddress)
	}

	for ; domain <= 65535; domain++ {
		for ; bus <= 255; bus++ {
			for ; device <= 255; device++ {
				// partial PCI address without function
				newAddress := fmt.Sprintf("%04x:%02x:%02x.", domain, bus, device)
				// add zero for PCI function part of the address
				newSysfsDeviceDir := path.Join(sysfsI915Dir, fmt.Sprintf("%s0", newAddress))
				if _, err := os.Stat(newSysfsDeviceDir); err != nil {
					return newAddress, nil
				}
			}
		}
	}

	return "", fmt.Errorf("no addresses left")
}

// getVFMEmoryAmountMiB returns the amount of local memory the VF should have.
// This is a sum of lmem_quota from all tiles of VF config in parent GPU.
func getVFMemoryAmountMiB(parentVFsDir string, pciVFIdx uint64) (uint64, error) {
	// DRM VF index is 1-based
	drmVFIdx := pciVFIdx + 1
	vfDir := path.Join(parentVFsDir, fmt.Sprintf("vf%d", drmVFIdx))
	vfTilePaths := getGTdirs(vfDir)
	if len(vfTilePaths) == 0 {
		return 0, fmt.Errorf("could not find VF tiles in %v", vfDir)
	}

	lmemTotalMiB := uint64(0)
	for _, vfTilePath := range vfTilePaths {
		lmemQuotaFilePath := path.Join(vfTilePath, "lmem_quota")
		data, err := os.ReadFile(lmemQuotaFilePath)
		if err != nil {
			return 0, fmt.Errorf("could not read file %v: %v", lmemQuotaFilePath, err)
		}

		lmemQuotaBytes, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("could not parse lmem_quota value %v from file %v: %v", string(data), lmemQuotaFilePath, err)
		}

		lmemQuotaMiB := lmemQuotaBytes / (1024 * 1024)

		lmemTotalMiB += lmemQuotaMiB
	}

	return lmemTotalMiB, nil
}

// Duplicate code from pkg/sriov/sriov.go. TODO: minimize duplication.
// getGTdirs returns directories named gt* in vfTilesDir.
func getGTdirs(vfDir string) []string {
	filePath := filepath.Join(vfDir, "gt*")
	gts, err := filepath.Glob(filePath)
	if err != nil {
		fmt.Printf("could not find gt* dirs in %v. Err: %v", filePath, err)
		return []string{}
	}

	return gts
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
		if cardRegexp.MatchString(drmFileName) {
			cardIdx, err := strconv.ParseUint(drmFileName[4:], 10, 64)
			if err != nil {
				return 0, 0, fmt.Errorf("failed to parse index of DRM card device '%v', skipping", drmFileName)
			}
			if cardIdx > highestCardIdx {
				highestCardIdx = cardIdx
			}
		} else if renderdRegexp.MatchString(drmFileName) {
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

// autoProvisioningEnabled returns true if pf/auto_provisioning contains 1 in
// given directory, false otherwise.
func autoProvisioningEnabled(sysfsVFsDir string) (bool, error) {
	autoProvisioningFilePath := path.Join(sysfsVFsDir, "pf/auto_provisioning")
	autoProvisioning, err := os.ReadFile(autoProvisioningFilePath)
	if err != nil {
		return false, fmt.Errorf("checking auto_provisioning status at %v: %v", autoProvisioningFilePath, err)
	}

	if strings.TrimSpace(string(autoProvisioning)) == "1" {
		return true, nil
	}

	return false, nil
}

// fakeSysfsSRIOVContents adds symlinks and IOV layout for PF and VFs.
func fakeSysfsSRIOVContents(t *testing.T, sysfsRoot string, devices DevicesInfo) error {
	perDeviceNumvfs := countVFs(devices)
	for deviceUID, device := range devices {
		i915DevDir := path.Join(sysfsRoot, "bus/pci/drivers/i915/", deviceUID[:pciDBDFLength])
		switch device.DeviceType {
		case "gpu":
			if device.MaxVFs <= 0 {
				continue
			}
			writeTestFile(t, path.Join(i915DevDir, "sriov_numvfs"), fmt.Sprint(perDeviceNumvfs[deviceUID]))
			writeTestFile(t, path.Join(i915DevDir, "sriov_totalvfs"), fmt.Sprint(device.MaxVFs))
			writeTestFile(t, path.Join(i915DevDir, "sriov_drivers_autoprobe"), "1")

			cardName := fmt.Sprintf("card%v", device.CardIdx)
			prelimIovDir := path.Join(i915DevDir, "drm", cardName, "prelim_iov")
			pfDir := path.Join(prelimIovDir, "pf")
			if err := os.MkdirAll(pfDir, 0750); err != nil {
				return fmt.Errorf("creating fake sysfs, err: %v", err)
			}
			writeTestFile(t, path.Join(pfDir, "auto_provisioning"), "1")

			for drmVFIndex := 1; drmVFIndex <= int(device.MaxVFs); drmVFIndex++ {
				drmVFDir := path.Join(prelimIovDir, fmt.Sprintf("vf%d", drmVFIndex))
				tileDirs, found := perDeviceIdTilesDirs[device.Model]
				if !found {
					return fmt.Errorf("device %v (id %v) is not in perDeviceIdTilesDirs map", device.UID, device.Model)
				}

				for _, vfTileDir := range tileDirs {
					drmVFgtDir := path.Join(drmVFDir, vfTileDir)
					if err := os.MkdirAll(drmVFgtDir, 0750); err != nil {
						return fmt.Errorf("creating fake sysfs, err: %v", err)
					}
					for _, vfAttr := range sriov.VfAttributeFiles {
						writeTestFile(t, path.Join(drmVFgtDir, vfAttr), "0")
					}
				}
			}

		case "vf":
			if _, found := devices[device.ParentUID]; !found {
				// check if PF already exists
				if _, err := os.Stat(path.Join(i915DevDir, "../", device.ParentUID[:pciDBDFLength])); err != nil {
					t.Errorf("parent device %v of VF %v is not found and will not be created", device.ParentUID, deviceUID)
				}
			}

			if err := os.Symlink(fmt.Sprintf("../%s", device.ParentUID[:pciDBDFLength]), path.Join(i915DevDir, "physfn")); err != nil {
				t.Errorf("creating fake sysfs, err: %v", err)
			}

			parentI915DevDir := path.Join(sysfsRoot, "bus/pci/drivers/i915/", device.ParentUID[:pciDBDFLength])

			parentLinkName := path.Join(parentI915DevDir, fmt.Sprintf("virtfn%d", device.VFIndex))
			targetName := fmt.Sprintf("../%s", deviceUID[:pciDBDFLength])

			if err := os.Symlink(targetName, parentLinkName); err != nil {
				t.Errorf("creating fake sysfs, err: %v", err)
			}
		default:
			t.Errorf("unsupported device type: %v (device %v)", device.DeviceType, deviceUID)
		}
	}

	return nil
}

// fakeSysFsContents creates new fake sysfs ensuring there wasn't any previously.
// This should be called in the beginning of the testcase.
func fakeSysFsContents(t *testing.T, sysfsRootUntrusted string, devices DevicesInfo) error {
	// fake sysfsroot should be deletable.
	// To prevent disaster mistakes, it is enforced to be in /tmp.
	sysfsRoot := path.Join(sysfsRootUntrusted)
	if !strings.HasPrefix(sysfsRoot, "/tmp") {
		return fmt.Errorf("fake sysfsroot can only be in /tmp, got: %v", sysfsRoot)
	}

	// Fail immediately, if the directory exists to prevent data loss when
	// fake sysfs would need to be deleted.
	if _, err := os.Stat(sysfsRoot); err == nil {
		return fmt.Errorf("cannot create fake sysfs, path exists: %v\n", sysfsRoot)
	}

	if err := os.Mkdir(sysfsRoot, 0750); err != nil {
		return fmt.Errorf("could not create fake sysfs root %v: %v", sysfsRoot, err)
	}

	return fakeSysFsDevices(t, sysfsRoot, devices)
}

// fakeSysFsDevices creates PCI and DRM devices layout in existing fake sysfsRoot.
// This will be called when fake sysfs is being created and when more devices added
// to existing fake sysfs.
func fakeSysFsDevices(t *testing.T, sysfsRoot string, devices DevicesInfo) error {
	for deviceUID, device := range devices {
		// driver setup
		i915DevDir := path.Join(sysfsRoot, "bus/pci/drivers/i915/", deviceUID[:pciDBDFLength])
		if err := os.MkdirAll(i915DevDir, 0750); err != nil {
			return fmt.Errorf("creating fake sysfs, err: %v", err)
		}
		writeTestFile(t, path.Join(i915DevDir, "device"), device.Model)

		cardName := fmt.Sprintf("card%v", device.CardIdx)

		if err := os.MkdirAll(path.Join(i915DevDir, "drm", cardName), 0750); err != nil {
			return fmt.Errorf("creating fake sysfs, err: %v", err)
		}

		if device.RenderdIdx != 0 { // some GPUs do not have render device
			renderdName := fmt.Sprintf("renderD%v", device.RenderdIdx)
			if err := os.MkdirAll(path.Join(i915DevDir, "drm", renderdName), 0750); err != nil {
				return fmt.Errorf("creating fake sysfs, err: %v", err)
			}
		}
		// DRM setup
		sysfsDRMClassDir := path.Join(sysfsRoot, "class/drm")
		if err := os.MkdirAll(sysfsDRMClassDir, 0750); err != nil {
			return fmt.Errorf("creating directory %v: %v", sysfsDRMClassDir, err)
		}

		drmDirLinkSource := path.Join(sysfsRoot, "class/drm", cardName)
		drmDirLinkTarget := path.Join(i915DevDir, "drm", cardName)

		if err := os.Symlink(drmDirLinkTarget, drmDirLinkSource); err != nil {
			return fmt.Errorf("creating fake sysfs, err: %v", err)
		}

		localMemoryStr := fmt.Sprint(device.MemoryMiB * 1024 * 1024)
		writeTestFile(t, path.Join(drmDirLinkTarget, "lmem_total_bytes"), localMemoryStr)
	}

	return fakeSysfsSRIOVContents(t, sysfsRoot, devices)
}

// watchNumvfs returns watcher that monitors numvfs_file and
// updates fakesysfs respectively to written values.
// It is caller's responsibility to close the watcher when the
// testcase comes to an end.
func watchNumvfs(t *testing.T, fakesysfs string) *fsnotify.Watcher {
	// Create new watcher.
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}

	go watchPFnumvfs(t, watcher)

	// find all sriov_numvfs and watch them
	sysfsI915Dir := filepath.Join(fakesysfs, "/bus/pci/drivers/i915/")
	files, err := os.ReadDir(sysfsI915Dir)
	if err != nil {
		t.Fatalf("could not monitor sriov_numvfs files in %v: %v", sysfsI915Dir, err)
	}

	for _, pciDBDF := range files {
		deviceDBDF := pciDBDF.Name()
		// check if file is pci device
		if !pciRegexp.MatchString(deviceDBDF) {
			continue
		}

		deviceI915Dir := filepath.Join(sysfsI915Dir, deviceDBDF)
		numvfsFilePath := filepath.Join(deviceI915Dir, "sriov_numvfs")
		_, err := os.ReadFile(numvfsFilePath)
		if err != nil {
			continue
		}
		err = watcher.Add(numvfsFilePath)
		if err != nil {
			t.Fatalf("could not add file to watch, err: %v", err)
		}
	}

	return watcher
}

// updateVFsOnWrite handls updates of sriov_numvfs file in fake sysfs.
// - truncates file
// - calls removeFakeVFsOnParent if 0 VFs were requested
// - calls addFakeVFsOnParent if > 0 VFs were requested
// - does nothing if there was no value - its own truncation caused event.
func updateVFsOnWrite(t *testing.T, numvfsFilePath string) {
	numvfsBytes, err := os.ReadFile(numvfsFilePath)
	if err != nil {
		t.Errorf("could not read numvfs file %v: %v", numvfsFilePath, err)
	}

	numvfsStr := strings.TrimSpace(string(numvfsBytes))
	t.Logf("detected new sriov_numvfs value %v: '%v'", numvfsFilePath, numvfsStr)

	if len(numvfsStr) == 0 {
		// File was truncated, nothing to do, it was us.
		return
	}

	// Truncate fhe file immediately, real sysfs file is written with appending,
	// so the values will accumulate over time if it's not truncated.
	f, err := os.OpenFile(numvfsFilePath, os.O_TRUNC, os.ModeAppend)
	if err != nil {
		t.Errorf("could not open file %v for truncation: %v", numvfsFilePath, err)
		// Do not do anything else, fake sysfs is not alright.
		return
	}
	if err = f.Close(); err != nil {
		t.Errorf("could not close file handler for %v after truncation: %v", numvfsFilePath, err)
		// Do not do anything else, fake sysfs is not alright.
		return
	}

	numvfsInt, err := strconv.ParseUint(numvfsStr, 10, 64)
	if err != nil {
		t.Errorf("could not convert string into int: %s", string(numvfsBytes))
		return
	}

	t.Logf("updating SR-IOV setup of fake device %v\n", numvfsFilePath)
	if numvfsInt == 0 {
		removeFakeVFsOnParent(t, numvfsFilePath)
	} else {
		addFakeVFsOnParent(t, numvfsFilePath, numvfsInt)
	}
}

// Start listening for events.
func watchPFnumvfs(t *testing.T, watcher *fsnotify.Watcher) {
	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok { // channel was closed
				return
			}
			if event.Has(fsnotify.Write) {
				updateVFsOnWrite(t, event.Name)
			}
		case err, ok := <-watcher.Errors:
			if !ok { // channel was closed
				return
			}
			t.Logf("fsnotify watcher error: %v\n", err)
		}
	}
}

func writePreparedClaimsToFile(preparedClaimFilePath string, claimAllocations ClaimPreparations) error {
	file, err := os.Create(preparedClaimFilePath)
	if err != nil {
		return fmt.Errorf("error creating file: %v", err)
	}

	defer file.Close()

	encoder := json.NewEncoder(file)
	err = encoder.Encode(claimAllocations)
	if err != nil {
		return fmt.Errorf("error encoding JSON: %v", err)
	}

	return nil
}

func readPreparedClaimsFromFile(preparedClaimFilePath string) (ClaimPreparations, error) {
	file, err := os.Open(preparedClaimFilePath)
	if err != nil {
		return nil, fmt.Errorf("error opening file: %v", err)
	}
	defer file.Close()

	preparedClaims := make(ClaimPreparations)

	decoder := json.NewDecoder(file)

	err = decoder.Decode(&preparedClaims)
	if err != nil {
		return nil, fmt.Errorf("error decoding JSON: %v", err)
	}

	return preparedClaims, nil
}
