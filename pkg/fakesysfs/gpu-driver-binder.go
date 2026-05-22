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
	"path/filepath"
	"strings"
	"testing"

	"github.com/fsnotify/fsnotify"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
)

// WatchDriverBindUnbind returns watcher that monitors fake sysfs drivers' bind and unbind.
// When the write to either of them is detected, fakesysfs is updatedrespectively.
// It is the caller's responsibility to close the watcher when the testcase comes to an end.
func WatchDriverBindUnbind(t *testing.T, sysfsRoot string, devfsRoot string, realDevices bool) *fsnotify.Watcher {
	// Create new watcher.
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatalf("could not create new watcher: %v", err)
	}

	// Find all <fakesysfs_root>/bus/pci/drivers/*/[bind|unbind] and and watch them.
	sysfsDriversDir := filepath.Join(sysfsRoot, "/bus/pci/drivers/")
	files, err := os.ReadDir(sysfsDriversDir)
	if err != nil {
		t.Fatalf("could not read files in %v: %v", sysfsDriversDir, err)
	}

	for _, driverDir := range files {
		driverName := driverDir.Name()
		bindPath := filepath.Join(sysfsDriversDir, driverName, "bind")
		unbindPath := filepath.Join(sysfsDriversDir, driverName, "unbind")
		if _, err := os.Stat(bindPath); os.IsNotExist(err) {
			t.Fatalf("misconfigured fakesysfs: no bind for driver %v at %v", driverName, bindPath)
		}
		if _, err := os.Stat(unbindPath); os.IsNotExist(err) {
			t.Fatalf("misconfigured fakesysfs: no unbind for driver %v at %v", driverName, unbindPath)
		}

		err = watcher.Add(bindPath)
		if err != nil {
			t.Fatalf("could not watch %v, err: %v", bindPath, err)
		}
		err = watcher.Add(unbindPath)
		if err != nil {
			t.Fatalf("could not watch %v, err: %v", unbindPath, err)
		}
	}

	go watchDriverBindUnbind(t, sysfsRoot, devfsRoot, watcher, realDevices)

	return watcher
}

// updateTestRootOnDriverBindUnbindWrite handles updates of bind and unbind files in fake sysfs.
// - truncates file
// - calls fakeBindPCIDeviceToDriver if write to bind happened
// - calls fakeUnbindPCIDeviceFromDriver if write to unbind happened
// - does nothing if there was no value - its own truncation caused event.
func updateTestRootOnDriverBindUnbindWrite(t *testing.T, sysfsRoot, devfsRoot, bindUnbindFilePath string, realDevices bool) {
	fileName := filepath.Base(bindUnbindFilePath)
	driverName := filepath.Base(filepath.Dir(bindUnbindFilePath))
	log.Printf("detected write to %v\n", bindUnbindFilePath)

	pciAddrBytes, err := os.ReadFile(bindUnbindFilePath)
	if err != nil {
		t.Errorf("could not read file %v: %v", bindUnbindFilePath, err)
	}

	pciAddrStr := strings.TrimSpace(string(pciAddrBytes))
	if len(pciAddrStr) == 0 {
		log.Printf("file %v was truncated, ignoring", bindUnbindFilePath)
		return
	}
	log.Printf("detected new pci address value %v: '%v'", bindUnbindFilePath, pciAddrStr)

	// Truncate fhe file immediately, real sysfs file is written with appending,
	// so the values will accumulate over time if it's not truncated.
	f, err := os.OpenFile(bindUnbindFilePath, os.O_TRUNC, os.ModeAppend)
	if err != nil {
		t.Errorf("could not open file %v for truncation: %v", bindUnbindFilePath, err)
		// Do not do anything else, fake sysfs is not alright.
		return
	}
	if err = f.Close(); err != nil {
		t.Errorf("could not close file handler for %v after truncation: %v", bindUnbindFilePath, err)
		// Do not do anything else, fake sysfs is not alright.
		return
	}

	if fileName == "bind" {
		log.Printf("handling bind for driver %v and PCI address %v\n", driverName, pciAddrStr)
		if err := fakeBindPCIDeviceToDriver(sysfsRoot, devfsRoot, driverName, pciAddrStr, realDevices); err != nil {
			t.Errorf("could not bind fake PCI device: %v", err)
		}
	} else { // unbind
		log.Printf("handling unbind for driver %v and PCI address %v\n", driverName, pciAddrStr)
		if err := fakeUnbindPCIDeviceFromDriver(sysfsRoot, devfsRoot, driverName, pciAddrStr); err != nil {
			t.Errorf("could not unbind fake PCI device: %v", err)
		}
	}
}

// watchDriverBindUnbind starts listening for events by watching file changes.
func watchDriverBindUnbind(t *testing.T, sysfsRoot, devfsRoot string, watcher *fsnotify.Watcher, realDevices bool) {
	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok { // channel was closed
				return
			}
			if event.Has(fsnotify.Write) {
				updateTestRootOnDriverBindUnbindWrite(t, sysfsRoot, devfsRoot, event.Name, realDevices)
			}
		case err, ok := <-watcher.Errors:
			if !ok { // channel was closed
				return
			}
			log.Printf("fsnotify driverBindUnbind watcher error: %v\n", err)
		}
	}
}

func fakeBindPCIDeviceToDriver(sysfsRoot, devfsRoot, driverName, pciAddrStr string, realDevices bool) error {
	if !deviceExists(sysfsRoot, pciAddrStr) {
		return fmt.Errorf("no sysfs device tree for %v", pciAddrStr)
	}

	// - if the device is bound to requested driver - do nothing
	// - if the device bound to another driver - do nothing
	// - if the device is not bound - bind it
	deviceDriverLink := filepath.Join(sysfsRoot, "/bus/pci/devices/", pciAddrStr, "driver")
	if _, err := os.Stat(deviceDriverLink); err == nil {
		linkTarget, err := os.Readlink(deviceDriverLink)
		if err != nil {
			return fmt.Errorf("could not read driver link for device %v: %v", pciAddrStr, err)
		}
		currentDriver := filepath.Base(linkTarget)
		if driverName == currentDriver {
			return nil
		}

		// The device is already bound to another driver, do nothing.
		log.Printf("driverBindUnbind Watcher: device %v is already bound to another driver %v, cannot bind to %v\n", pciAddrStr, currentDriver, driverName)
		return nil
	}

	switch driverName {
	case device.SysfsI915DriverName, device.SysfsXeDriverName:
		if err := fakeBindPCIDeviceToDRMDriver(sysfsRoot, devfsRoot, driverName, pciAddrStr, realDevices); err != nil {
			return fmt.Errorf("could not bind PCI device %v to DRM driver %v: %v", pciAddrStr, driverName, err)
		}
	case device.SysfsVFIODriverName, device.SysfsXeVFIODriverName:
		if err := fakeBindPCIDeviceToVFIODriver(sysfsRoot, devfsRoot, driverName, pciAddrStr, realDevices); err != nil {
			return fmt.Errorf("could not bind PCI device %v to VFIO driver %v: %v", pciAddrStr, driverName, err)
		}
	default:
		return fmt.Errorf("unknown driver name %v", driverName)
	}

	return nil
}

func fakeBindPCIDeviceToDRMDriver(sysfsRoot, devfsRoot, driverName, pciAddrStr string, realDevices bool) error {
	highestCardIdx, highestRenderDIdx, err := deduceHighestCardAndRenderDIndexes(sysfsRoot)
	if err != nil {
		return fmt.Errorf("could not get current DRM card and renderD devices indexes: %v", err)
	}
	cardName := fmt.Sprintf("card%d", highestCardIdx+1)
	renderDName := fmt.Sprintf("renderD%d", highestRenderDIdx+1)

	// deduce PCIe root.
	// /sys/bus/pci/devices/0000:00:01.0 -> /sys/devices/pci0000:00/0000:00:01.0
	linkTarget, err := os.Readlink(filepath.Join(sysfsRoot, "/bus/pci/devices/", pciAddrStr))
	if err != nil {
		return fmt.Errorf("could not read PCI device link for %v: %v", pciAddrStr, err)
	}
	pciRoot := filepath.Base(filepath.Dir(linkTarget))

	// DRM setup: sysfs + devfs
	if err := fakeDeviceBinding(sysfsRoot, devfsRoot, driverName, pciRoot, pciAddrStr); err != nil {
		return fmt.Errorf("creating fake sysfs PCI driver binding, err: %v", err)
	}
	if err := fakeGpuDRI(sysfsRoot, devfsRoot, pciAddrStr, cardName, renderDName, realDevices); err != nil {
		return fmt.Errorf("creating fake sysfs DRI devices, err: %v", err)
	}

	// MEI setup
	meiMax, err := deduceHighestMeiIndex(sysfsRoot)
	if err != nil {
		return fmt.Errorf("could not get current MEI devices indexes: %v", err)
	}
	meiName := fmt.Sprintf("mei%d", meiMax+1)
	if err := fakeGpuMEI(sysfsRoot, devfsRoot, pciRoot, pciAddrStr, meiName, driverName, realDevices); err != nil {
		return fmt.Errorf("creating fake mei sysfs: %v", err)
	}

	return nil
}

func fakeBindPCIDeviceToVFIODriver(sysfsRoot, devfsRoot, driverName, pciAddrStr string, realDevices bool) error {
	// deduce PCIe root.
	// /sys/bus/pci/devices/0000:00:01.0 -> /sys/devices/pci0000:00/0000:00:01.0
	linkTarget, err := os.Readlink(filepath.Join(sysfsRoot, "/bus/pci/devices/", pciAddrStr))
	if err != nil {
		return fmt.Errorf("could not read PCI device link for %v: %v", pciAddrStr, err)
	}
	pciRoot := filepath.Base(filepath.Dir(linkTarget))

	if err := fakeDeviceBinding(sysfsRoot, devfsRoot, driverName, pciRoot, pciAddrStr); err != nil {
		return fmt.Errorf("creating fake sysfs PCI driver binding, err: %v", err)
	}

	vfioMax, err := deduceHighestVFIOIndex(sysfsRoot)
	if err != nil {
		return fmt.Errorf("could not get current VFIO devices index: %v", err)
	}
	vfioDevice := fmt.Sprintf("vfio%d", vfioMax+1)

	iommuGroupMax, err := deduceHighestIOMMUGroupIndex(devfsRoot)
	if err != nil {
		return fmt.Errorf("could not get current IOMMU group index: %v", err)
	}
	iommuGroup := fmt.Sprintf("%d", iommuGroupMax+1)

	log.Printf("new vfio device %v, iommu group %v for PCI device %v\n", vfioDevice, iommuGroup, pciAddrStr)
	return fakeGpuVFIO(sysfsRoot, devfsRoot, vfioDevice, iommuGroup, pciAddrStr, realDevices)
}

func fakeUnbindPCIDeviceFromDriver(sysfsRoot, devfsRoot, driverName, pciAddrStr string) error {
	driverDeviceLink := filepath.Join(sysfsRoot, "/bus/pci/drivers/", driverName, pciAddrStr)
	if _, err := os.Stat(driverDeviceLink); os.IsNotExist(err) {
		return fmt.Errorf("misconfigured fakesysfs: no device link for driver %v at %v", driverName, driverDeviceLink)
	}

	// Remove devfs devices, sysfs driver link from device dir and device PCI address link from driver dir.
	switch driverName {
	case device.SysfsI915DriverName, device.SysfsXeDriverName:
		log.Printf("cleaning up DRM sysfs for %v\n", pciAddrStr)
		if cleanupDRMDeviceErr := cleanupDRMDevice(sysfsRoot, devfsRoot, pciAddrStr); cleanupDRMDeviceErr != nil {
			return fmt.Errorf("could not clean up DRM device for %v: %v", pciAddrStr, cleanupDRMDeviceErr)
		}
	case device.SysfsVFIODriverName, device.SysfsXeVFIODriverName:
		log.Printf("cleaning up VFIO sysfs for %v\n", pciAddrStr)
		if cleanupVFIODeviceErr := cleanupVFIODevice(sysfsRoot, devfsRoot, pciAddrStr); cleanupVFIODeviceErr != nil {
			return fmt.Errorf("could not clean up VFIO device for %v: %v", pciAddrStr, cleanupVFIODeviceErr)
		}
	}

	if err := os.Remove(driverDeviceLink); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("could not remove driver device link at %v: %v", driverDeviceLink, err)
	}

	deviceDriverLink := filepath.Join(sysfsRoot, "/bus/pci/devices/", pciAddrStr, "driver")
	if err := os.Remove(deviceDriverLink); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("could not remove driver link at %v: %v", deviceDriverLink, err)
	}

	return nil
}

func cleanupDRMDevice(sysfsRoot, devfsRoot, pciAddrStr string) error {
	// Read cardX and renderDX from pciDeviceDRMDir and remove them from devfsRoot.
	pciDeviceDRMDir := filepath.Join(sysfsRoot, "/bus/pci/devices/", pciAddrStr, "drm")
	files, err := os.ReadDir(pciDeviceDRMDir)
	if err != nil {
		return fmt.Errorf("could not read files in %v: %v", pciDeviceDRMDir, err)
	}

	for _, file := range files {
		if strings.HasPrefix(file.Name(), "card") || strings.HasPrefix(file.Name(), "renderD") {
			devfsDevicePath := filepath.Join(devfsRoot, device.DevfsDriPath, file.Name())
			if err := os.Remove(devfsDevicePath); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("could not remove devfs device at %v: %v", devfsDevicePath, err)
			}
		}
	}

	if err := os.RemoveAll(pciDeviceDRMDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("could not remove DRM dir at %v: %v", pciDeviceDRMDir, err)
	}

	return nil
}

func cleanupVFIODevice(sysfsRoot, devfsRoot, pciAddrStr string) error {
	// Cleanup iommu, iommu_group, vfio-dev from PCI device dir.
	pciDeviceVFIODir := filepath.Join(sysfsRoot, "/bus/pci/devices/", pciAddrStr, "vfio-dev")
	files, err := os.ReadDir(pciDeviceVFIODir)
	if err != nil {
		return fmt.Errorf("could not read files in %v: %v", pciDeviceVFIODir, err)
	}

	for _, file := range files {
		if strings.HasPrefix(file.Name(), "vfio") {
			devfsDevicePath := filepath.Join(devfsRoot, device.DevfsVFIODevicesPath, file.Name())
			if err := os.Remove(devfsDevicePath); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("could not remove devfs device at %v: %v", devfsDevicePath, err)
			}
			break // There is only one device always.
		}
	}

	if err := os.RemoveAll(pciDeviceVFIODir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("could not remove VFIO dir at %v: %v", pciDeviceVFIODir, err)
	}

	iommuGroupLink := filepath.Join(sysfsRoot, "/bus/pci/devices/", pciAddrStr, "iommu_group")
	iommuGroupTarget, err := os.Readlink(iommuGroupLink)
	if err != nil {
		return fmt.Errorf("could not read iommu group link at %v: %v", iommuGroupLink, err)
	}
	groupId := filepath.Base(iommuGroupTarget)
	devfsGroupPath := filepath.Join(devfsRoot, device.DevfsVFIOPath, groupId)
	if err := os.Remove(devfsGroupPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("could not remove devfs iommu group at %v: %v", devfsGroupPath, err)
	}

	if err := os.Remove(iommuGroupLink); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("could not remove iommu group link at %v: %v", iommuGroupLink, err)
	}

	iommuLink := filepath.Join(sysfsRoot, "/bus/pci/devices/", pciAddrStr, "iommu")
	if err := os.Remove(iommuLink); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("could not remove iommu link at %v: %v", iommuLink, err)
	}

	return nil
}

func deviceExists(sysfsRoot, pciAddrStr string) bool {
	deviceSysfsDir := filepath.Join(sysfsRoot, "/bus/pci/devices/", pciAddrStr)
	if _, err := os.Stat(deviceSysfsDir); os.IsNotExist(err) {
		return false
	}

	return true
}
