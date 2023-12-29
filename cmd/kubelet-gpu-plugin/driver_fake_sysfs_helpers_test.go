/*
 * Copyright (c) 2023, Intel Corporation.  All Rights Reserved.
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
		if device.deviceType == intelcrd.VfDeviceType {
			perDeviceNumvfs[deviceUID] += 1
		}
	}
	return perDeviceNumvfs
}

func removeFakeVFDRM(t *testing.T, sysfsI915DeviceDir string) {
	deviceDRMDir := filepath.Join(sysfsI915DeviceDir, "drm")
	drmFiles, err := os.ReadDir(deviceDRMDir)

	if err != nil {
		t.Errorf("Cannot read device folder %v: %v", deviceDRMDir, err)
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
			t.Logf("Deleting DRM dir %v", drmDir)
			if err := os.RemoveAll(drmDir); err != nil {
				t.Fatalf("could not cleanup fake VF DRM dir %v: %v", drmDir, err)
			}

			return
		}
	}
}

func removeFakeVFsOnParent(t *testing.T, numvfsFile string) {
	// Find VF symlinks in PF.
	sysfsI915DeviceDir := path.Dir(numvfsFile)
	sysfsI915Dir := path.Dir(sysfsI915DeviceDir)
	virtfnsPattern := filepath.Join(sysfsI915DeviceDir, "virtfn*")
	files, _ := filepath.Glob(virtfnsPattern)

	for _, virtfn := range files {
		t.Logf("Checking %v", virtfn)
		virtfnTarget, err := os.Readlink(virtfn)
		if err != nil {
			t.Errorf("Failed reading virtfn symlink %v: %v. Skipping", virtfn, err)
			continue
		}

		// ../0000:00:02.1  # 15 chars
		if len(virtfnTarget) != 15 {
			t.Errorf("Symlink target does not match expected length: %v", virtfnTarget)
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

func addFakeVFsOnParent(t *testing.T, numvfsFile string) {
	t.Error("addFakeVFsOnParent is not implemented")
}

func fakeSysfsSRIOVContents(t *testing.T, sysfsRoot string, devices DevicesInfo) {
	perDeviceNumvfs := countVFs(devices)
	for deviceUID, device := range devices {
		i915DevDir := path.Join(sysfsRoot, "bus/pci/drivers/i915/", deviceUID[:pciDBDFLength])
		switch device.deviceType {
		case "gpu":
			if device.maxvfs <= 0 {
				continue
			}
			writeTestFile(t, path.Join(i915DevDir, "sriov_numvfs"), fmt.Sprint(perDeviceNumvfs[deviceUID]))
			writeTestFile(t, path.Join(i915DevDir, "sriov_totalvfs"), fmt.Sprint(device.maxvfs))
			writeTestFile(t, path.Join(i915DevDir, "sriov_drivers_autoprobe"), "1")

			cardName := fmt.Sprintf("card%v", device.cardidx)
			prelimIovDir := path.Join(i915DevDir, "drm", cardName, "prelim_iov")
			pfDir := path.Join(prelimIovDir, "pf")
			if err := os.MkdirAll(pfDir, 0750); err != nil {
				t.Errorf("setup error: creating fake sysfs, err: %v", err)
				return
			}
			writeTestFile(t, path.Join(pfDir, "auto_provisioning"), "1")

			for drmVFIndex := 1; drmVFIndex <= int(device.maxvfs); drmVFIndex++ {
				drmVFDir := path.Join(prelimIovDir, fmt.Sprintf("vf%d", drmVFIndex))
				drmVFgtDir := path.Join(drmVFDir, sriov.GtDirFromModel(device.model))
				if err := os.MkdirAll(drmVFgtDir, 0750); err != nil {
					t.Errorf("setup error: creating fake sysfs, err: %v", err)
					return
				}
				for _, vfAttr := range sriov.VfAttributeFiles {
					writeTestFile(t, path.Join(drmVFgtDir, vfAttr), "0")
				}
			}

		case "vf":
			if _, found := devices[device.parentuid]; !found {
				t.Errorf("parent device %v of VF %v is not found", device.parentuid, deviceUID)
			}

			if err := os.Symlink(fmt.Sprintf("../%s", device.parentuid[:pciDBDFLength]), path.Join(i915DevDir, "physfn")); err != nil {
				t.Errorf("setup error: creating fake sysfs, err: %v", err)
			}

			parentI915DevDir := path.Join(sysfsRoot, "bus/pci/drivers/i915/", device.parentuid[:pciDBDFLength])

			parentLinkName := path.Join(parentI915DevDir, fmt.Sprintf("virtfn%d", device.vfindex))
			targetName := fmt.Sprintf("../%s", deviceUID[:pciDBDFLength])

			if err := os.Symlink(targetName, parentLinkName); err != nil {
				t.Errorf("setup error: creating fake sysfs, err: %v", err)
			}
		default:
			t.Errorf("setup error: unsupported device type: %v (device %v)", device.deviceType, deviceUID)
		}
	}
}

func fakeSysFsContents(t *testing.T, sysfsRootUntrusted string, devices DevicesInfo) {

	// fake sysfsroot should be deletable.
	// To prevent disaster mistakes, it is enforced to be in /tmp.
	sysfsRoot := path.Join(sysfsRootUntrusted)
	if !strings.HasPrefix(sysfsRoot, "/tmp") {
		t.Errorf("fake sysfsroot can only be in /tmp, got: %v", sysfsRoot)
		return
	}

	// Fail immediately, if the directory exists to prevent data loss when
	// fake sysfs would need to be deleted.
	if _, err := os.Stat(sysfsRoot); err == nil {
		t.Errorf("cannot create fake sysfs, path exists: %v\n", sysfsRoot)
		return
	}

	if err := os.Mkdir(sysfsRoot, 0750); err != nil {
		t.Errorf("could not create fake sysfs root %v: %v", sysfsRoot, err)
		return
	}

	for deviceUID, device := range devices {
		// driver setup
		i915DevDir := path.Join(sysfsRoot, "bus/pci/drivers/i915/", deviceUID[:pciDBDFLength])
		if err := os.MkdirAll(i915DevDir, 0750); err != nil {
			t.Errorf("setup error: creating fake sysfs, err: %v", err)
			return
		}
		writeTestFile(t, path.Join(i915DevDir, "device"), device.model)

		cardName := fmt.Sprintf("card%v", device.cardidx)

		if err := os.MkdirAll(path.Join(i915DevDir, "drm", cardName), 0750); err != nil {
			t.Errorf("setup error: creating fake sysfs, err: %v", err)
			return
		}

		if device.renderdidx != 0 { // some GPUs do not have render device
			renderdName := fmt.Sprintf("renderD%v", device.renderdidx)
			if err := os.MkdirAll(path.Join(i915DevDir, "drm", renderdName), 0750); err != nil {
				t.Errorf("setup error: creating fake sysfs, err: %v", err)
				return
			}
		}
		// DRM setup
		sysfsDRMClassDir := path.Join(sysfsRoot, "class/drm")
		if err := os.MkdirAll(sysfsDRMClassDir, 0750); err != nil {
			t.Errorf("could not create directory %v: %v", sysfsDRMClassDir, err)
			return
		}

		drmDirLinkSource := path.Join(sysfsRoot, "class/drm", cardName)
		drmDirLinkTarget := path.Join(i915DevDir, "drm", cardName)

		if err := os.Symlink(drmDirLinkTarget, drmDirLinkSource); err != nil {
			t.Errorf("setup error: creating fake sysfs, err: %v", err)
			return
		}

		localMemoryStr := fmt.Sprint(device.memoryMiB * 1024 * 1024)
		writeTestFile(t, path.Join(drmDirLinkTarget, "lmem_total_bytes"), localMemoryStr)
	}

	fakeSysfsSRIOVContents(t, sysfsRoot, devices)
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
		numvfsFile := filepath.Join(deviceI915Dir, "sriov_numvfs")
		_, err := os.ReadFile(numvfsFile)
		if err != nil {
			continue
		}
		err = watcher.Add(numvfsFile)
		if err != nil {
			t.Fatalf("could not add file to watch, err: %v", err)
		}
	}

	return watcher
}

func updateVFsOnWrite(t *testing.T, numvfsFile string) {
	t.Logf("Updating SR-IOV setup of fake device %v\n", numvfsFile)
	numvfsBytes, err := os.ReadFile(numvfsFile)
	if err != nil {
		t.Errorf("could not read numvfs file %v: %v", numvfsFile, err)
	}

	numvfsInt, err := strconv.ParseUint(strings.TrimSpace(string(numvfsBytes)), 10, 64)
	if err != nil {
		t.Errorf("Could not convert string into int: %s", string(numvfsBytes))
		return
	}

	if numvfsInt == 0 {
		removeFakeVFsOnParent(t, numvfsFile)
	} else {
		addFakeVFsOnParent(t, numvfsFile)
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
