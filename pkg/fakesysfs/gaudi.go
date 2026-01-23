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

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gaudi/device"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/helpers"
)

func FakeSysFsGaudiContents(root, sysfsRoot, devfsRoot string, gaudis device.DevicesInfo, realDeviceFiles bool) error {
	if err := sanitizeFakeSysFsDir(sysfsRoot); err != nil {
		return err
	}

	if err := createFakeCDIGaudiBits(root); err != nil {
		return err
	}

	return fakeSysFsGaudiDevices(sysfsRoot, devfsRoot, gaudis, realDeviceFiles)
}

// fakeSysFsGaudiDevices creates PCI and DRM devices layout in existing fake sysfsRoot.
// This will be called when fake sysfs is being created and when more devices added
// to existing fake sysfs.
func fakeSysFsGaudiDevices(sysfsRoot string, devfsRoot string, gaudis device.DevicesInfo, realDeviceFiles bool) error {
	for _, gaudi := range gaudis {
		// precaution: PCIRoot is needed for fake sysfs layout.
		if len(gaudi.PCIRoot) == 0 {
			return fmt.Errorf("malformed input: PCIRoot is a mandatory field and is missing in %v. All devices: %+v", gaudi, gaudis)
		}

		// Main dir with all the info.
		if err := setupPCIDevice(sysfsRoot, gaudi); err != nil {
			return fmt.Errorf("error creating sysfs PCI device files: %v", err)
		}

		// symlinks from bus/pci/drivers/habanalabs/<pciAddress> to PCI device.
		if err := setupPCIDriverDirs(sysfsRoot, gaudi); err != nil {
			return fmt.Errorf("error creating sysfs PCI driver files: %v", err)
		}

		if err := setupAccelClassLinks(sysfsRoot, gaudi); err != nil {
			return fmt.Errorf("error creating sysfs accel class files: %v", err)
		}

		if err := fakeGaudiDevfs(devfsRoot, gaudi, realDeviceFiles); err != nil {
			return fmt.Errorf("error creating devfs files: %v", err)
		}
	}

	return nil
}

func setupPCIDevice(sysfsRoot string, gaudi *device.DeviceInfo) error {
	// /sys/devices/<pciRoot>/<pciAddress>/
	// e.g.
	// /sys/devices/pci0000:15/0000:19:00.0/
	pciDevDir := path.Join(sysfsRoot, fmt.Sprintf("devices/%s", gaudi.PCIRoot), gaudi.PCIAddress)

	// /sys/devices/<pciRoot>/<pciAddress>/accel/accel0/
	pciDevAccelDir := path.Join(pciDevDir, "accel", fmt.Sprintf("accel%d", gaudi.DeviceIdx))
	if err := os.MkdirAll(pciDevAccelDir, 0755); err != nil {
		return fmt.Errorf("creating PCI device accel dir: %v", err)
	}

	// /sys/devices/<pciRoot>/<pciAddress>/device
	if writeErr := helpers.WriteFile(path.Join(pciDevDir, "device"), gaudi.Model); writeErr != nil {
		return fmt.Errorf("writing PCI device file: %v", writeErr)
	}

	// /sys/devices/<pciRoot>/<pciAddress>/pci_addr
	if writeErr := helpers.WriteFile(path.Join(pciDevDir, "pci_addr"), gaudi.PCIAddress); writeErr != nil {
		return fmt.Errorf("writing PCI device file: %v", writeErr)
	}

	// /sys/devices/<pciRoot>/<pciAddress>/module_id
	if writeErr := helpers.WriteFile(path.Join(pciDevDir, "module_id"), fmt.Sprintf("%v", gaudi.DeviceIdx)); writeErr != nil {
		return fmt.Errorf("creating PCI device file: %v", writeErr)
	}

	// driver -> /sys/bus/pci/drivers/habanalabs
	// relative from /sys/devices/pci0000:15/0000:19:00.0/.
	driverDeviceLinkSource := path.Join(pciDevDir, "driver")
	if err := os.Symlink("../../../bus/pci/drivers/habanalabs", driverDeviceLinkSource); err != nil {
		return fmt.Errorf("creating PCI device driver symlink: %v", err)
	}

	if err := fakePCIDeviceSymlink(sysfsRoot, gaudi.PCIRoot, gaudi.PCIAddress); err != nil {
		return fmt.Errorf("creating PCI device symlink: %v", err)
	}

	return nil
}

func setupPCIDriverDirs(sysfsRoot string, gaudi *device.DeviceInfo) error {
	pciDriverDir := path.Join(sysfsRoot, "bus/pci/drivers/habanalabs/")
	if err := os.MkdirAll(pciDriverDir, 0755); err != nil {
		return fmt.Errorf("creating PCI driver dir: %v", err)
	}

	// <pci_addr> -> /sys/devices/<pciRoot>/<pciAddress>/
	// relative from /sys/bus/pci/drivers/habanalabs/.
	symlinkSource := path.Join(pciDriverDir, gaudi.PCIAddress)
	if err := os.Symlink(fmt.Sprintf("../../../../devices/%s/%s", gaudi.PCIRoot, gaudi.PCIAddress), symlinkSource); err != nil {
		return fmt.Errorf("creating PCI driver device symlink: %v", err)
	}

	if writeErr := helpers.WriteFile(path.Join(pciDriverDir, "bind"), ""); writeErr != nil {
		return fmt.Errorf("writing PCI device file: %v", writeErr)
	}

	return nil
}

func setupAccelClassLinks(sysfsRoot string, gaudi *device.DeviceInfo) error {
	// class/accel setup
	deviceName := fmt.Sprintf("accel%v", gaudi.DeviceIdx)

	sysfsAccelClassDir := path.Join(sysfsRoot, "class/accel")
	if err := os.MkdirAll(sysfsAccelClassDir, 0755); err != nil {
		return fmt.Errorf("creating accel class dir: %v", err)
	}

	// accelX -> /sys/devices/<pciRoot>/<pciAddress>
	// relative from /sys/class/accel/.
	accelLinkSource := path.Join(sysfsAccelClassDir, deviceName)
	if err := os.Symlink(fmt.Sprintf("../../devices/%s/%s", gaudi.PCIRoot, gaudi.PCIAddress), accelLinkSource); err != nil {
		return fmt.Errorf("creating accel class device symlink: %v", err)
	}

	return nil
}

func fakeGaudiDevfs(devfsRoot string, gaudi *device.DeviceInfo, real bool) error {
	accelDevPath := path.Join(devfsRoot, "accel")
	if err := os.MkdirAll(accelDevPath, 0755); err != nil {
		return fmt.Errorf("creating dir: %v", err)
	}

	return fakeGaudiDeviceFiles(devfsRoot, accelDevPath, gaudi.DeviceIdx, real)
}

func fakeGaudiDeviceFiles(devfsRoot, accelDevPath string, accelIdx uint64, real bool) error {
	devices := []string{
		path.Join(accelDevPath, fmt.Sprintf("accel%v", accelIdx)),
		path.Join(accelDevPath, fmt.Sprintf("accel_controlD%v", accelIdx)),
		path.Join(devfsRoot, fmt.Sprintf("hl%d", accelIdx)),
		path.Join(devfsRoot, fmt.Sprintf("hl_controlD%d", accelIdx)),
	}

	for _, device := range devices {
		if err := createDevice(device, real); err != nil {
			return fmt.Errorf("creating device: %v", err)
		}
	}

	return nil
}

// createFakeCDIGaudiBits creates two files that can be used as CDI hook and bind-mount,
// they fake the habana-container-hook and gaudinet.json.
func createFakeCDIGaudiBits(targetDir string) error {
	// Ceate blank text file for gaudinet mount.
	gaudinetFile, err := os.OpenFile(path.Join(targetDir, "gaudinet"), os.O_CREATE|os.O_WRONLY, 0660)
	if err != nil {
		return fmt.Errorf("failed to create fake gaudinet: %v", err)
	}
	if err := gaudinetFile.Close(); err != nil {
		return fmt.Errorf("failed to close fake hook %v: %v", gaudinetFile.Name(), err)
	}

	// Create blank executable for CDI hook.
	hookbinPath := path.Join(targetDir, "hookbin")
	hookbinFile, err := os.OpenFile(path.Join(hookbinPath), os.O_CREATE|os.O_WRONLY, 0660)
	if err != nil {
		return fmt.Errorf("failed to create fake hook %v: %v", hookbinPath, err)
	}
	if _, err := hookbinFile.WriteString("#!/bin/sh\necho OK\n"); err != nil {
		return fmt.Errorf("failed to write to fake hook %v: %v", hookbinPath, err)
	}
	if err := hookbinFile.Close(); err != nil {
		return fmt.Errorf("failed to close fake hook %v: %v", hookbinPath, err)
	}
	if err := os.Chmod(hookbinPath, 0o777); err != nil {
		return fmt.Errorf("failed to chmod fake hook %v: %v", hookbinPath, err)
	}

	return nil
}
