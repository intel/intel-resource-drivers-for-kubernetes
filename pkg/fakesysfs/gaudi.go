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

func FakeSysFsGaudiContents(sysfsRoot string, devfsRoot string, gaudis device.DevicesInfo, realDeviceFiles bool) error {
	if err := sanitizeFakeSysFsDir(sysfsRoot); err != nil {
		return err
	}

	return fakeSysFsGaudiDevices(sysfsRoot, devfsRoot, gaudis, realDeviceFiles)
}

// fakeSysFsGaudiDevices creates PCI and DRM devices layout in existing fake sysfsRoot.
// This will be called when fake sysfs is being created and when more devices added
// to existing fake sysfs.
func fakeSysFsGaudiDevices(sysfsRoot string, devfsRoot string, gaudis device.DevicesInfo, realDeviceFiles bool) error {
	for _, gaudi := range gaudis {
		if err := setupPCIDriverDevice(sysfsRoot, gaudi); err != nil {
			return err
		}

		if err := setupVirtualAccelDevice(sysfsRoot, gaudi); err != nil {
			return err
		}

		if err := setupAccelClassLinks(sysfsRoot, gaudi); err != nil {
			return err
		}

		if err := fakeGaudiDevfs(devfsRoot, gaudi, realDeviceFiles); err != nil {
			return err
		}
	}

	return nil
}

func setupPCIDriverDevice(sysfsRoot string, gaudi *device.DeviceInfo) error {
	// bus/pci/driver/<device> setup
	pciDriverDevDir := path.Join(sysfsRoot, "bus/pci/drivers/habanalabs/", gaudi.PCIAddress)
	if err := os.MkdirAll(pciDriverDevDir, 0755); err != nil {
		return fmt.Errorf("creating fake sysfs, err: %v", err)
	}

	if writeErr := helpers.WriteFile(path.Join(pciDriverDevDir, "device"), gaudi.Model); writeErr != nil {
		return fmt.Errorf("creating fake sysfs dir, err: %v", writeErr)
	}

	return nil
}

func setupVirtualAccelDevice(sysfsRoot string, gaudi *device.DeviceInfo) error {
	// devices/virtual/accel/<device> setup
	deviceName := fmt.Sprintf("accel%v", gaudi.DeviceIdx)
	controlDeviceName := fmt.Sprintf("accel_controlD%v", gaudi.DeviceIdx)
	// devices/virtual/accel/<device> setup
	dirPath := path.Join(sysfsRoot, "devices/virtual/accel", deviceName, "device")
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return fmt.Errorf("creating fake sysfs dir, err: %v", err)
	}
	// $ cat /sys/devices/virtual/accel/accel0/device/pci_addr
	// 0000:0f:00.0
	if writeErr := helpers.WriteFile(path.Join(dirPath, "pci_addr"), gaudi.PCIAddress); writeErr != nil {
		return fmt.Errorf("creating fake sysfs dir, err: %v", writeErr)
	}

	if writeErr := helpers.WriteFile(path.Join(dirPath, "module_id"), fmt.Sprintf("%v", gaudi.DeviceIdx)); writeErr != nil {
		return fmt.Errorf("creating fake sysfs dir, err: %v", writeErr)
	}

	dirPath = path.Join(sysfsRoot, "devices/virtual/accel", controlDeviceName)
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return fmt.Errorf("creating fake sysfs, err: %v", err)
	}

	return nil
}

func setupAccelClassLinks(sysfsRoot string, gaudi *device.DeviceInfo) error {
	// class/accel setup
	deviceName := fmt.Sprintf("accel%v", gaudi.DeviceIdx)
	controlDeviceName := fmt.Sprintf("accel_controlD%v", gaudi.DeviceIdx)

	sysfsAccelClassDir := path.Join(sysfsRoot, "class/accel")
	if err := os.MkdirAll(sysfsAccelClassDir, 0755); err != nil {
		return fmt.Errorf("creating fake sysfs, err: %v", err)
	}

	// links setup
	accelDirDeviceFile := path.Join(sysfsAccelClassDir, deviceName)
	accelDirControlDeviceFile := path.Join(sysfsAccelClassDir, controlDeviceName)

	if err := os.Symlink(fmt.Sprintf("../../devices/virtual/accel/%v", deviceName), accelDirDeviceFile); err != nil {
		return fmt.Errorf("creating fake sysfs, err: %v", err)
	}

	if err := os.Symlink(fmt.Sprintf("../../devices/virtual/accel/%v", controlDeviceName), accelDirControlDeviceFile); err != nil {
		return fmt.Errorf("creating fake sysfs, err: %v", err)
	}

	return nil
}

func fakeGaudiDevfs(devfsRoot string, gaudi *device.DeviceInfo, realDevices bool) error {
	accelDevPath := path.Join(devfsRoot, "accel")
	if err := os.MkdirAll(accelDevPath, 0755); err != nil {
		return fmt.Errorf("creating fake devs, err: %v", err)
	}

	if realDevices {
		return fakeGaudiDeviceFiles(devfsRoot, accelDevPath, gaudi.DeviceIdx)
	}

	return fakeGaudiPlainDeviceFiles(devfsRoot, accelDevPath, gaudi.DeviceIdx)
}

func fakeGaudiDeviceFiles(devfsRoot, accelDevPath string, accelIdx uint64) error {
	if err := createDevice(path.Join(accelDevPath, fmt.Sprintf("accel%v", accelIdx))); err != nil {
		return fmt.Errorf("creating fake devfs, err: %v", err)
	}
	if err := createDevice(path.Join(accelDevPath, fmt.Sprintf("accel_controlD%v", accelIdx))); err != nil {
		return fmt.Errorf("creating fake devfs, err: %v", err)
	}

	if err := createDevice(path.Join(devfsRoot, fmt.Sprintf("hl%d", accelIdx))); err != nil {
		return fmt.Errorf("creating fake devfs, err: %v", err)
	}
	if err := createDevice(path.Join(devfsRoot, fmt.Sprintf("hl_controlD%d", accelIdx))); err != nil {
		return fmt.Errorf("creating fake devfs, err: %v", err)
	}

	return nil
}

func fakeGaudiPlainDeviceFiles(devfsRoot, accelDevPath string, accelIdx uint64) error {
	if err := helpers.WriteFile(path.Join(accelDevPath, fmt.Sprintf("accel%v", accelIdx)), ""); err != nil {
		return fmt.Errorf("creating fake devfs, err: %v", err)
	}
	if err := helpers.WriteFile(path.Join(accelDevPath, fmt.Sprintf("accel_controlD%v", accelIdx)), ""); err != nil {
		return fmt.Errorf("creating fake devfs, err: %v", err)
	}

	if err := helpers.WriteFile(path.Join(devfsRoot, fmt.Sprintf("hl%d", accelIdx)), ""); err != nil {
		return fmt.Errorf("creating fake devfs, err: %v", err)
	}
	if err := helpers.WriteFile(path.Join(devfsRoot, fmt.Sprintf("hl_controlD%d", accelIdx)), ""); err != nil {
		return fmt.Errorf("creating fake devfs, err: %v", err)
	}

	return nil
}
