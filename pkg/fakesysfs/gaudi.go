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
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/testhelpers"
)

func FakeSysFsGaudiContents(sysfsRoot string, gaudis device.DevicesInfo) error {
	if err := sanitizeFakeSysFsDir(sysfsRoot); err != nil {
		return err
	}

	return fakeSysFsGaudiDevices(sysfsRoot, gaudis)
}

// fakeSysFsGaudiDevices creates PCI and DRM devices layout in existing fake sysfsRoot.
// This will be called when fake sysfs is being created and when more devices added
// to existing fake sysfs.
func fakeSysFsGaudiDevices(sysfsRoot string, gaudis device.DevicesInfo) error {
	for _, gaudi := range gaudis {
		// bus/pci/driver/<device> setup
		pciDriverDevDir := path.Join(sysfsRoot, "bus/pci/drivers/habanalabs/", gaudi.PCIAddress)
		if err := os.MkdirAll(pciDriverDevDir, 0750); err != nil {
			return fmt.Errorf("creating fake sysfs, err: %v", err)
		}

		if writeErr := testhelpers.WriteFile(path.Join(pciDriverDevDir, "device"), gaudi.Model); writeErr != nil {
			return fmt.Errorf("creating fake sysfs dir, err: %v", writeErr)
		}

		deviceDirName := fmt.Sprintf("accel%v", gaudi.DeviceIdx)
		controlDeviceDirName := fmt.Sprintf("accel_controlD%v", gaudi.DeviceIdx)
		// devices/virtual/accel/<device> setup
		dirPath := path.Join(sysfsRoot, "devices/virtual/accel", deviceDirName, "device")
		if err := os.MkdirAll(dirPath, 0755); err != nil {
			return fmt.Errorf("creating fake sysfs dir, err: %v", err)
		}
		// $ cat /sys/devices/virtual/accel/accel0/device/pci_addr
		// 0000:0f:00.0
		if writeErr := testhelpers.WriteFile(path.Join(dirPath, "pci_addr"), gaudi.PCIAddress); writeErr != nil {
			return fmt.Errorf("creating fake sysfs dir, err: %v", writeErr)
		}

		dirPath = path.Join(sysfsRoot, "devices/virtual/accel", controlDeviceDirName)
		if err := os.MkdirAll(dirPath, 0755); err != nil {
			return fmt.Errorf("creating fake sysfs, err: %v", err)
		}

		// class/accel setup
		sysfsAccelClassDir := path.Join(sysfsRoot, "class/accel")
		if err := os.MkdirAll(sysfsAccelClassDir, 0750); err != nil {
			return fmt.Errorf("creating fake sysfs, err: %v", err)
		}

		// links setup
		accelDirDeviceFile := path.Join(sysfsAccelClassDir, deviceDirName)
		accelDirControlDeviceFile := path.Join(sysfsAccelClassDir, controlDeviceDirName)

		if err := os.Symlink(fmt.Sprintf("../../devices/virtual/accel/%v", deviceDirName), accelDirDeviceFile); err != nil {
			return fmt.Errorf("creating fake sysfs, err: %v", err)
		}

		if err := os.Symlink(fmt.Sprintf("../../devices/virtual/accel/%v", controlDeviceDirName), accelDirControlDeviceFile); err != nil {
			return fmt.Errorf("creating fake sysfs, err: %v", err)
		}
	}

	return nil
}
