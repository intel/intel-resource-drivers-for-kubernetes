/*
 * Copyright (c) 2026, Intel Corporation.  All Rights Reserved.
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

package vfio

import (
	"fmt"
	"os"
	"path"
	"time"

	"k8s.io/klog/v2"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/helpers"
)

const (
	SysfsVFIODriverName   = "vfio-pci"
	SysfsXeVFIODriverName = "xe-vfio-pci"
	DevVFIOPath           = "vfio"
)

func EnsureKernelModuleLoaded(moduleName string) error {
	if _, err := os.Stat(path.Join(helpers.GetSysfsRoot("bus/pci/devices"), "/bus/pci/drivers", moduleName)); err != nil {
		return fmt.Errorf("%v kernel module is not loaded", moduleName)
	}

	return nil
}

func UnbindDeviceFromKernelDriver(pciAddress string) error {
	driverFilePath := path.Join(helpers.GetSysfsRoot("bus/pci/devices"), "bus/pci/devices", pciAddress, "driver")
	driverLink, err := os.Readlink(driverFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			klog.V(5).Infof("could not read driver link %v: no link found: device not bound to any driver", driverFilePath)
			return nil
		}

		return fmt.Errorf("failed to read current driver for device %v: %v", pciAddress, err)
	}

	driver := path.Base(driverLink)
	klog.V(5).Infof("unbinding %v from driver %v", pciAddress, driver)

	unbindPath := path.Join(helpers.GetSysfsRoot("bus/pci/drivers"), "/bus/pci/drivers", driver, "unbind")
	if _, err := os.Stat(unbindPath); os.IsNotExist(err) {
		return fmt.Errorf("unbind path %v does not exist", unbindPath)
	}

	if err := writeSysfsFile(unbindPath, pciAddress); err != nil {
		return fmt.Errorf("failed to unbind device %v from driver %v: %v", pciAddress, driver, err)
	}

	klog.V(5).Infof("successfully unbound %v from driver %v", pciAddress, driver)
	return nil
}

func OverrideDeviceDriver(pciAddress, newDriver string) error {
	klog.V(5).Infof("overriding driver for %v to %v", pciAddress, newDriver)

	overridePath := path.Join(helpers.GetSysfsRoot("bus/pci/devices"), "/bus/pci/devices", pciAddress, "driver_override")
	if _, err := os.Stat(overridePath); os.IsNotExist(err) {
		return fmt.Errorf("driver override path %v does not exist", overridePath)
	}

	klog.V(5).Infof("override location: %v", overridePath)
	if err := writeSysfsFile(overridePath, newDriver); err != nil {
		return fmt.Errorf("failed to override driver for device %v to %v: %v", pciAddress, newDriver, err)
	}

	klog.V(5).Infof("successfully set driver override for %v to %v", pciAddress, newDriver)
	return nil
}

func BindDeviceToDriver(pciAddress, driver string) error {
	klog.V(5).Infof("binding %v to driver %v", pciAddress, driver)

	driver_override := driver
	if driver != SysfsVFIODriverName && driver != SysfsXeVFIODriverName {
		driver_override = "\n"
	}

	if err := OverrideDeviceDriver(pciAddress, driver_override); err != nil {
		klog.Errorf("failed to override driver for device %v to %v: %v", pciAddress, driver, err)
		return fmt.Errorf("failed to override driver for device %v to %v: %v", pciAddress, driver, err)
	}

	// TODO: replace with reading the driver_override to ensure it was changed.
	klog.V(5).Info("sleeping 1s")
	time.Sleep(1 * time.Second)

	bindPath := path.Join(helpers.GetSysfsRoot("/bus/pci/drivers"), "/bus/pci/drivers", driver, "bind")
	if _, err := os.Stat(bindPath); os.IsNotExist(err) {
		return fmt.Errorf("bind path %v does not exist", bindPath)
	}

	if err := writeSysfsFile(bindPath, pciAddress); err != nil {
		return fmt.Errorf("failed to bind device %v to driver %v: %v", pciAddress, driver, err)
	}

	klog.V(5).Infof("successfully bound %v to driver %v", pciAddress, driver)
	return nil
}

func GetDevVFIOPath() string {
	return path.Join(helpers.GetDevfsRoot(DevVFIOPath), DevVFIOPath)
}

func writeSysfsFile(filePath, content string) error {
	fhandle, err := os.OpenFile(filePath, os.O_APPEND|os.O_WRONLY, os.ModeAppend)
	if err != nil {
		return fmt.Errorf("failed to open %v file: %v", filePath, err)
	}
	defer fhandle.Close()

	if _, err = fhandle.WriteString(content); err != nil {
		return fmt.Errorf("could not write to file %v: %v", filePath, err)
	}

	if err = fhandle.Sync(); err != nil {
		return fmt.Errorf("could not sync file %v to storage: %v", filePath, err)
	}

	return nil
}
