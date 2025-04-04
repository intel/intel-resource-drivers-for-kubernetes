/* Copyright (C) 2024 Intel Corporation
 * SPDX-License-Identifier: Apache-2.0
 */

package fakesysfs

import (
	"fmt"
	"os"
	"path"
	"strconv"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/helpers"
)

const (
	sysfsDevicePath  = "bus/pci/devices"
	sysfsDriverPath  = "bus/pci/drivers"
	moduleName       = "4xxx"
	vfioPCI          = "vfio-pci"
	vfioBind         = vfioPCI + "/bind"
	vfioUnbind       = vfioPCI + "/unbind"
	pciDevicePattern = "????:??:??.?"
	qatState         = "qat/state"
	qatServices      = "qat/cfg_services"
	driverOverride   = "driver_override"
	numVFs           = "sriov_numvfs"
	totalVFs         = "sriov_totalvfs"
	vfDevicePattern  = "virtfn"
	vfDriver         = "driver"
	vfIOMMUpath      = "kernel/iommu_groups"
	vfIOMMU          = "iommu_group"
	vfDeviceNode     = "vfio"
)

type QATDevices []*PFDevice

type PFDevice struct {
	Device   string
	State    string
	Services string
	TotalVFs int
	NumVFs   int
}

type pcidevicefiles struct {
	relpath string
	value   string
}

func writesysfsfiles(driverdevdir string, devicefiles []pcidevicefiles) error {
	for _, files := range devicefiles {

		reldir := path.Dir(files.relpath)
		if reldir != "" {
			if err := os.MkdirAll(path.Join(driverdevdir, reldir), 0755); err != nil {
				return fmt.Errorf("creating fake sysfs rel dir: %v", err)
			}
		}

		if err := helpers.WriteFile(path.Join(driverdevdir, files.relpath), files.value); err != nil {
			return fmt.Errorf("creating fake sysfs dir, err: %v", err)
		}

	}

	return nil
}

func pcipath(device string) string {
	return "devices/pci" + device[0:7]
}

func FakeSysFsQATVFContents(sysfsRoot string, pcipath string, totalvfs int, device string, iommu *int) error {
	// ...bus/pci/devices
	devicepath := path.Join(sysfsRoot, sysfsDevicePath)
	// ...kernel/iommu_groups
	vfiopath := path.Join(sysfsRoot, vfIOMMUpath)
	// ...bus/pci/drivers/vfio-pci
	vfiopcipath := path.Join(sysfsRoot, sysfsDriverPath, vfioPCI)
	// ...devices/pcixxxx:xx
	pcidevpath := path.Join(sysfsRoot, pcipath)

	for i := 1; i <= totalvfs; i++ {

		vfdev := device[:7] + fmt.Sprintf(":%02x.%1x", i/8, i%8)

		vfpath := path.Join(pcidevpath, vfdev)

		// ...devices/pcixxxx:xx/xxxx:xx:xx.x
		if err := os.MkdirAll(vfpath, 0755); err != nil {
			return fmt.Errorf("creating fake sysfs vf device directory: %v", err)
		}
		// ...devices/pcixxxx:xx/xxxx:xx:xx.x -> .../bus/pci/devices/xxxx:xx:xx.x
		if err := os.Symlink(vfpath, path.Join(devicepath, vfdev)); err != nil {
			return fmt.Errorf("creating fake sysfs vf device symlink '%s': %v", vfpath, err)
		}

		*iommu++
		vfiommupath := path.Join(vfiopath, strconv.Itoa(*iommu))
		if err := os.MkdirAll(vfiommupath, 0755); err != nil {
			return fmt.Errorf("cannot create iommu dir in '%s'", vfiopath)
		}
		vfiommu := path.Join(vfpath, vfIOMMU)
		if err := os.Symlink(vfiommupath, vfiommu); err != nil {
			return fmt.Errorf("creating vfiommu symlink '%s'", vfiommu)
		}
		vfdriver := path.Join(vfpath, vfDriver)
		if err := os.Symlink(vfiopcipath, vfdriver); err != nil {
			return fmt.Errorf("creating vfio driver symlink '%s'", vfdriver)
		}

		vfname := fmt.Sprintf("%s%d", vfDevicePattern, i)
		pflinkpath := path.Join(pcidevpath, device, vfname)
		// ...devices/pcixxxx:xx/xxxx:xx:yy.y -> ...devices/pcixxxx:xx/xxxx:xx:xx.x/vfio<x>
		if err := os.Symlink(vfpath, pflinkpath); err != nil {
			return fmt.Errorf("creating fake sysfs vf device driver link: %v", err)
		}
	}

	return nil
}

func FakeSysFsQATContents(qatdevices QATDevices) error {
	os.Setenv("SYSFS_ROOT", helpers.TestSysfsRoot)
	os.Setenv("DEVFS_ROOT", helpers.TestDevfsRoot)

	// ...bus/pci/drivers/<moduleName>
	kerneldriverdir := path.Join(helpers.TestSysfsRoot, sysfsDriverPath, moduleName)
	if err := os.MkdirAll(kerneldriverdir, 0755); err != nil {
		return fmt.Errorf("creating fake sysfs driver dir: %v", err)
	}

	// ...bus/pci/drivers/vfio-pci
	vfiopcidriverdir := path.Join(helpers.TestSysfsRoot, sysfsDriverPath, vfioPCI)
	if err := os.MkdirAll(vfiopcidriverdir, 0755); err != nil {
		return fmt.Errorf("creating fake sysfs pci driver dir: %v", err)
	}

	// ...bus/pci/devices
	pcidevicedir := path.Join(helpers.TestSysfsRoot, sysfsDevicePath)
	if err := os.MkdirAll(pcidevicedir, 0755); err != nil {
		return fmt.Errorf("creating fake sysfs device dir: %v", err)
	}

	iommu := 350
	for _, pf := range qatdevices {
		// ...devices/pci/pcixxx:xx/xxxx:xx:xx.x
		devicedir := path.Join(helpers.TestSysfsRoot, pcipath(pf.Device), pf.Device)
		if err := os.MkdirAll(devicedir, 0755); err != nil {
			return fmt.Errorf("creating fake sysfs device dir: %v", err)
		}

		// .../bus/pci/devices/xxxx:xx:xx.x -> ...devices/pci/pcixxx:xx/xxxx:xx:xx.x
		if err := os.Symlink(devicedir, path.Join(pcidevicedir, pf.Device)); err != nil {
			return fmt.Errorf("creating fake sysfs device driver link: %v", err)
		}

		// .../bus/pci/devices/xxxx:xx:xx.x -> ...bus/pci/drivers/<moduleName>/xxxx:xx:xx.x
		if err := os.Symlink(devicedir, path.Join(kerneldriverdir, pf.Device)); err != nil {
			return fmt.Errorf("creating fake sysfs device driver link: %v", err)
		}

		if err := writesysfsfiles(devicedir, []pcidevicefiles{
			{numVFs, strconv.Itoa(pf.NumVFs)},
			{totalVFs, strconv.Itoa(pf.TotalVFs)},
			{qatState, pf.State},
			{qatServices, pf.Services},
		}); err != nil {
			return fmt.Errorf("creating fake sysfs device driver files: %v", err)
		}

		if err := FakeSysFsQATVFContents(helpers.TestSysfsRoot, pcipath(pf.Device), pf.TotalVFs, pf.Device, &iommu); err != nil {
			return fmt.Errorf("creating fake sysfs VF files: %v", err)
		}
	}

	return nil
}

func FakeFsRemove() {
	os.RemoveAll(helpers.TestSysfsRoot)
	os.RemoveAll(helpers.TestDevfsRoot)
}
