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
	"strings"
	"testing"

	"github.com/fsnotify/fsnotify"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/discovery"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/helpers"
)

func countVFs(devices device.DevicesInfo) map[string]int {
	perDeviceNumvfs := map[string]int{}
	for deviceUID, gpu := range devices {
		if gpu.DeviceType == device.VfDeviceType {
			perDeviceNumvfs[deviceUID] += 1
		}
	}
	return perDeviceNumvfs
}

func removeFakeVFDRM(devfsRoot string, sysfsI915DeviceDir string) error {
	deviceDRMDir := filepath.Join(sysfsI915DeviceDir, "drm")
	pciAddress := path.Base(sysfsI915DeviceDir)
	drmFiles, err := os.ReadDir(deviceDRMDir)
	if err != nil {
		return fmt.Errorf("cannot read device DRM folder %v: %v", deviceDRMDir, err)
	}

	// sysfsI915DeviceDir in here is, for example, this:
	// /tmp/fakesysfs/bus/pci/drivers/i915/0000:00:00.0/
	// getting /tmp/fakesysfs takes five times parent dir.
	fakesysfsRoot := path.Dir(path.Dir(path.Dir(path.Dir(path.Dir(sysfsI915DeviceDir)))))

	for _, drmFile := range drmFiles {
		drmFileName := drmFile.Name()
		if device.CardRegexp.MatchString(drmFileName) {
			drmDir := path.Join(fakesysfsRoot, "class/drm/", drmFileName)
			if err := os.RemoveAll(drmDir); err != nil {
				return fmt.Errorf("could not cleanup VF DRM dir %v: %v", drmDir, err)
			}
			// delete devfs/dri/card and by-path/ card link
			if err := os.Remove(path.Join(devfsRoot, "dri", drmFileName)); err != nil {
				return fmt.Errorf("could not cleanup VF DRI file: %v", err)
			}
			if err := os.Remove(path.Join(devfsRoot, "dri/by-path", fmt.Sprintf("pci-%s-card", pciAddress))); err != nil {
				return fmt.Errorf("could not cleanup VF DRI file: %v", err)
			}
		} else if device.RenderdRegexp.MatchString(drmFileName) {
			// delete devfs/dri/render and by-path/ card link
			if err := os.Remove(path.Join(devfsRoot, "dri", drmFileName)); err != nil {
				return fmt.Errorf("could not cleanup VF DRI file: %v", err)
			}
			if err := os.Remove(path.Join(devfsRoot, "dri/by-path", fmt.Sprintf("pci-%s-render", pciAddress))); err != nil {
				return fmt.Errorf("could not cleanup VF DRI file: %v", err)
			}
		}
	}

	return nil
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
func removeFakeVFsOnParent(devfsRoot string, numvfsFilePath string) error {
	// Find VF symlinks in PF.
	sysfsI915DeviceDir := path.Dir(numvfsFilePath)
	sysfsI915Dir := path.Dir(sysfsI915DeviceDir)
	virtfnsPattern := filepath.Join(sysfsI915DeviceDir, "virtfn*")
	files, _ := filepath.Glob(virtfnsPattern)

	for _, virtfn := range files {
		virtfnTarget, err := os.Readlink(virtfn)
		if err != nil {
			return fmt.Errorf("failed reading virtfn symlink %v: %v. Skipping", virtfn, err)
		}

		// ../0000:00:02.1  # 15 chars
		if len(virtfnTarget) != 15 {
			return fmt.Errorf("symlink target does not match expected length: %v", virtfnTarget)
		}

		sysfsI915VFDir := filepath.Join(sysfsI915Dir, virtfnTarget[3:])

		// Cleanup DRM files from separate directory hierarchy.
		if err := removeFakeVFDRM(devfsRoot, sysfsI915VFDir); err != nil {
			return fmt.Errorf("could not cleanup fake DRM: %v", err)
		}

		// Cleanup PCI device.
		if err := os.RemoveAll(sysfsI915VFDir); err != nil {
			return fmt.Errorf("could not cleanup fake VF PCI dir %v: %v", sysfsI915VFDir, err)
		}

		// Finally cleanup VF symlink.
		if err := os.Remove(virtfn); err != nil {
			return fmt.Errorf("could not cleanup fake VF symlink %v: %v", virtfn, err)
		}
	}

	return nil
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
func addFakeVFsOnParent(numvfsFilePath string, devfsRoot string, numVFs uint64, realDevices bool) error {
	sysfsI915DeviceDir := path.Dir(numvfsFilePath)
	parentPCIAddress := path.Base(sysfsI915DeviceDir)
	sysfsI915Dir := path.Dir(sysfsI915DeviceDir)
	fakeSysfsRoot := path.Join(sysfsI915Dir, "../../../../")

	// get GPU model
	modelFilePath := path.Join(sysfsI915DeviceDir, "device")
	modelBytes, err := os.ReadFile(modelFilePath)
	if err != nil {
		return fmt.Errorf("could not read fake sysfs model file %v: %v", modelFilePath, err)
	}
	model := strings.TrimSpace(string(modelBytes))

	// construct parent's DRM VFs dir path
	parentCardIdx, _, err := discovery.DeduceCardAndRenderdIndexes(sysfsI915DeviceDir)
	if err != nil {
		return fmt.Errorf("could not detect drm/cardX index in %v: %v", sysfsI915DeviceDir, err)
	}
	parentVFsDir := path.Join(sysfsI915DeviceDir, "drm", fmt.Sprintf("card%d", parentCardIdx), "prelim_iov")

	// check if auto_provisioning is enabled
	automatic, err := autoProvisioningEnabled(parentVFsDir)
	if err != nil {
		return fmt.Errorf("could not detect auto_provisioning: %v", err)
	}
	if automatic {
		// TODO: implement automatic provisioning, without VF profiles
		fmt.Println("WARNING: auto_provisioning in fake sysfs is not implemented")
	}

	// generate DeviceInfo for VFs.
	newDevices := device.DevicesInfo{}
	currentPCIdev := parentPCIAddress[:len(parentPCIAddress)-1]
	highestCardIdx, highestRenderDIdx, err := deduceHighestCardAndRenderDIndexes(fakeSysfsRoot)
	if err != nil {
		return fmt.Errorf("could not get current DRM card and renderD devices indexes: %v", err)
	}

	// PCI VF index is 0-based.
	for vfIdx := uint64(0); vfIdx < numVFs; vfIdx++ {
		fmt.Printf("creating object for VF %v\n", vfIdx)
		vfUID := ""

		//                 VF indexes in PCI addresses
		// parent DBDF [PF 0  1  2  3  4  5  6 ]  0000:00:01:[0-7]
		//   next DBDF [7  8  9  10 11 12 13 14]  0000:00:02:[0-7]
		//   next DBDF [15 16 17 18 19 ...     ]  0000:00:03:[0-7]...
		pciFunctionIdx := (vfIdx + 1) % 8
		if pciFunctionIdx == 0 {
			currentPCIdev, err = newPCIAddress(sysfsI915Dir, currentPCIdev)
			if err != nil {
				return fmt.Errorf("could not create new PCI address for new VF: %v", err)
			}
		}

		vfPCIAddress := fmt.Sprintf("%s%d", currentPCIdev, pciFunctionIdx)
		vfUID = helpers.DeviceUIDFromPCIinfo(vfPCIAddress, model)

		vfMem, err := getVFMemoryAmountMiB(parentVFsDir, vfIdx)
		if err != nil {
			return fmt.Errorf("could not get lmem_quota from VF%d on %v: %v", vfIdx, parentPCIAddress, err)
		}

		newDevices[vfUID] = &device.DeviceInfo{
			PCIAddress: vfPCIAddress,
			Model:      model,
			MemoryMiB:  vfMem,
			DeviceType: "vf",
			CardIdx:    highestCardIdx + vfIdx + 1,
			RenderdIdx: highestRenderDIdx + vfIdx + 1,
			UID:        vfUID,
			VFIndex:    vfIdx,
			ParentUID:  helpers.DeviceUIDFromPCIinfo(parentPCIAddress, model),
		}
	}

	if err := fakeSysFsGpuDevices(fakeSysfsRoot, devfsRoot, newDevices, realDevices); err != nil {
		return fmt.Errorf("creating new VFs: %v", err)
	}

	return nil
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
func fakeSysfsSRIOVContents(sysfsRoot string, gpus device.DevicesInfo) error {
	perDeviceNumvfs := countVFs(gpus)
	for deviceUID, gpu := range gpus {
		if gpu.PCIAddress == "" {
			// attempt gettinging it from UID
			if len(gpu.UID) != device.UIDLength {
				return fmt.Errorf("cannot determine PCI address for device: %v. Neither PCIAddress nor UID contain valid PCI address", gpu)
			}
			gpu.PCIAddress, _ = helpers.PciInfoFromDeviceUID(deviceUID)
		}
		driverDevDir := path.Join(sysfsRoot, "bus/pci/drivers/", gpu.Driver, gpu.PCIAddress)

		switch gpu.DeviceType {
		case "gpu":
			if err := fakeSysfsPF(deviceUID, gpu, perDeviceNumvfs[deviceUID], driverDevDir); err != nil {
				return fmt.Errorf("error creating fake sysfs, err: %v", err)
			}
		case "vf":
			if _, found := gpus[gpu.ParentUID]; !found {
				// check if PF already exists
				if _, err := os.Stat(path.Join(driverDevDir, "../", gpu.ParentPCIAddress())); err != nil {
					return fmt.Errorf("parent device %v of VF %v is not found and will not be created", gpu.ParentUID, deviceUID)
				}
			}
			if err := fakeSysfsVF(gpu, perDeviceNumvfs[deviceUID], sysfsRoot, driverDevDir); err != nil {
				return fmt.Errorf("creating fake sysfs, err: %v", err)
			}
		default:
			return fmt.Errorf("unsupported device type: %v (device %v)", gpu.DeviceType, deviceUID)
		}
	}

	return nil
}

func fakeSysfsVF(vf *device.DeviceInfo, numvfs int, sysfsRoot string, i915DevDir string) error {
	if vf.Driver != "i915" {
		return fmt.Errorf("Fake SR-IOV only supported for i915 KMD")
	}

	if err := os.Symlink(fmt.Sprintf("../%s", vf.ParentPCIAddress()), path.Join(i915DevDir, "physfn")); err != nil {
		return fmt.Errorf("creating fake sysfs, err: %v", err)
	}

	parentI915DevDir := path.Join(sysfsRoot, "bus/pci/drivers/i915/", vf.ParentPCIAddress())

	parentLinkName := path.Join(parentI915DevDir, fmt.Sprintf("virtfn%d", vf.VFIndex))
	if vf.PCIAddress == "" {
		if len(vf.UID) != device.UIDLength {
			return fmt.Errorf("cannot determine PCI address for VF: %v. Neither PCIAddress nor UID contain valid PCI address", vf)
		}
		vf.PCIAddress, _ = helpers.PciInfoFromDeviceUID(vf.UID)
	}
	targetName := fmt.Sprintf("../%s", vf.PCIAddress)

	if err := os.Symlink(targetName, parentLinkName); err != nil {
		return fmt.Errorf("creating fake sysfs, err: %v", err)
	}

	return nil
}

// WatchNumvfs returns watcher that monitors numvfs_file and
// updates fakesysfs respectively to written values.
// It is caller's responsibility to close the watcher when the
// testcase comes to an end.
func WatchNumvfs(t *testing.T, sysfsRoot string, devfsRoot string, realDevices bool) *fsnotify.Watcher {
	// Create new watcher.
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}

	go watchPFnumvfs(t, devfsRoot, watcher, realDevices)

	// find all sriov_numvfs and watch them
	sysfsI915Dir := filepath.Join(sysfsRoot, "/bus/pci/drivers/i915/")
	files, err := os.ReadDir(sysfsI915Dir)
	if err != nil {
		t.Fatalf("could not monitor sriov_numvfs files in %v: %v", sysfsI915Dir, err)
	}

	for _, pciDBDF := range files {
		deviceDBDF := pciDBDF.Name()
		// check if file is pci device
		if !device.PciRegexp.MatchString(deviceDBDF) {
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

// updateVFsOnWrite handles updates of sriov_numvfs file in fake sysfs.
// - truncates file
// - calls removeFakeVFsOnParent if 0 VFs were requested
// - calls addFakeVFsOnParent if > 0 VFs were requested
// - does nothing if there was no value - its own truncation caused event.
func updateVFsOnWrite(t *testing.T, devfsRoot string, numvfsFilePath string, realDevices bool) {
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
		if err := removeFakeVFsOnParent(devfsRoot, numvfsFilePath); err != nil {
			t.Errorf("could not remove fake VFs: %v", err)
		}
	} else {
		if err := addFakeVFsOnParent(numvfsFilePath, devfsRoot, numvfsInt, realDevices); err != nil {
			t.Errorf("could not add fake VFs: %v", err)
		}
	}
}

// watchPFnumvfs starts listening for events by watching file changes.
func watchPFnumvfs(t *testing.T, devfsRoot string, watcher *fsnotify.Watcher, realDevices bool) {
	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok { // channel was closed
				return
			}
			if event.Has(fsnotify.Write) {
				updateVFsOnWrite(t, devfsRoot, event.Name, realDevices)
			}
		case err, ok := <-watcher.Errors:
			if !ok { // channel was closed
				return
			}
			t.Logf("fsnotify watcher error: %v\n", err)
		}
	}
}
