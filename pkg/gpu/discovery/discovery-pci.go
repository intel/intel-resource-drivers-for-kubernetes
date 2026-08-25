//
// Copyright (C) 2026 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0
//

package discovery

import (
	"fmt"
	"os"
	"path"
	"strings"

	deviceAttribute "k8s.io/dynamic-resource-allocation/deviceattribute"
	"k8s.io/klog/v2"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/drm"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/mei"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/helpers"
)

func processSysfsPCIDevices(sysfsRoot, namingStyle string) map[string]*device.DeviceInfo {
	devices := make(map[string]*device.DeviceInfo)

	sysfsDevicesDir := path.Join(sysfsRoot, device.SysfsPCIDevicesPath)

	klog.V(5).Infof("Looking for devices in %v", sysfsDevicesDir)
	files, err := os.ReadDir(sysfsDevicesDir)
	if err != nil {
		if os.IsNotExist(err) {
			klog.V(5).Infof("No Intel GPU devices found supported by %v on this host", sysfsDevicesDir)
			return devices
		}
		klog.Errorf("could not read sysfs directory: %v", err)
		return devices
	}

	for _, pciAddress := range files {
		devicePCIAddress := pciAddress.Name()
		// Only process PCI address-named directories.
		if !device.PciRegexp.MatchString(devicePCIAddress) {
			continue
		}

		deviceSysfsDir := path.Join(sysfsDevicesDir, devicePCIAddress)
		newDeviceInfo, err := DiscoverPCIDevice(deviceSysfsDir, sysfsRoot)
		if err != nil {
			continue
		}
		devices[DetermineDeviceName(newDeviceInfo, namingStyle)] = newDeviceInfo
	}

	return devices
}

// DiscoverPCIDevice scans single PCI device sysfs directory and returns DeviceInfo.
func DiscoverPCIDevice(deviceSysfsDir, sysfsRoot string) (*device.DeviceInfo, error) {
	currentDriver := GetPCIDeviceDriver(deviceSysfsDir)
	devicePCIAddress := path.Base(deviceSysfsDir)
	vendorId, deviceId, classId := readPCIInfo(deviceSysfsDir)
	if vendorId != device.PCIVendorId || !device.IsGPUClass(classId) {
		klog.V(5).Infof("ignoring device %v (vendorId: %v, classId: %v): not an Intel GPU", devicePCIAddress, vendorId, classId)
		return nil, fmt.Errorf("not an Intel GPU")
	}

	// If the GPU is not bound to the DRM driver, check if the original driver is known based on the device ID.
	originalDriver := currentDriver
	if currentDriver != device.SysfsI915DriverName && currentDriver != device.SysfsXeDriverName {
		modelDetails, found := device.ModelDetails[deviceId]
		if !found {
			klog.V(5).Infof("ignoring device %v (vendorId: %v, deviceId: %v): unknown default driver", devicePCIAddress, vendorId, deviceId)
			return nil, fmt.Errorf("ignoring device %v (vendorId: %v, deviceId: %v): unknown default driver", devicePCIAddress, vendorId, deviceId)

		}
		originalDriver = modelDetails["driver"]
	}

	newDeviceInfo := &device.DeviceInfo{
		PCIAddress:    devicePCIAddress,
		MemoryMiB:     0,
		Millicores:    initialMillicores,
		DeviceType:    device.GpuDeviceType, // presume GPU, detect the physfn / parent later
		CardName:      "",
		RenderDName:   "",
		Driver:        originalDriver,
		CurrentDriver: currentDriver,
		HealthStatus:  map[string]string{},
	}

	uid := helpers.DeviceUIDFromPCIinfo(devicePCIAddress, deviceId)
	newDeviceInfo.UID = uid
	newDeviceInfo.Model = deviceId
	newDeviceInfo.SetModelInfo()
	newDeviceInfo.SubVendorId, newDeviceInfo.SubDeviceId = readPCISubsystemInfo(deviceSysfsDir)

	newDeviceInfo.Survivability = isInSurvivabilityMode(deviceSysfsDir)

	switch {
	case newDeviceInfo.Survivability:
		newDeviceInfo.HealthStatus[device.HealthStatusSurvivability] = device.HealthUnhealthy
		newDeviceInfo.MEIName = mei.DiscoverMEIDeviceForGPU(deviceSysfsDir, deviceSysfsDir)
	case newDeviceInfo.IsDRMBound():
		cardName, renderDName, err := drm.DeduceCardAndRenderDNames(deviceSysfsDir)
		if err != nil {
			klog.Errorf("device %v bound to %v: failed to detect DRM devices: %v", devicePCIAddress, currentDriver, err)
			return nil, fmt.Errorf("device %v bound to %v: failed to detect DRM devices: %v", devicePCIAddress, currentDriver, err)
		}
		newDeviceInfo.CardName = cardName
		newDeviceInfo.RenderDName = renderDName
		newDeviceInfo.MEIName = mei.DiscoverMEIDeviceForGPU(deviceSysfsDir, deviceSysfsDir)
	case newDeviceInfo.IsVFIOBound():
		vfioDevice, err := GetVFIODevice(devicePCIAddress)
		if err != nil {
			// TODO: try reverting driver change
			// TODO: taint device to notify cluster admin about driver change failure.
			return nil, fmt.Errorf("failed to get VFIO device for PCI address %v: %v", devicePCIAddress, err)
		}
		newDeviceInfo.VFIODevice = vfioDevice

		iommuGroup, err := GetIOMMUGroup(devicePCIAddress)
		if err != nil {
			// TODO: try reverting driver change
			// TODO: taint device to notify cluster admin about driver change failure.
			return nil, fmt.Errorf("failed to get IOMMU group for device %v: %v", devicePCIAddress, err)
		}
		newDeviceInfo.IOMMUGroup = iommuGroup
	}

	pciRootAttribute, err := deviceAttribute.GetPCIeRootAttributeByPCIBusID(devicePCIAddress, deviceAttribute.WithFSFromRoot(sysfsRoot))
	if err != nil {
		klog.Warningf("could not detect PCI root complex for %v: %v", devicePCIAddress, err)
	} else {
		newDeviceInfo.PCIRoot = *pciRootAttribute.Value.StringValue
	}

	sysfsDevicesDir := path.Dir(deviceSysfsDir)
	detectSRIOV(newDeviceInfo, sysfsDevicesDir, devicePCIAddress, deviceId)
	return newDeviceInfo, nil
}

func readPCIInfo(sysfsDevicePath string) (vendorId, deviceId, classId string) {
	vendorIdBytes, err := os.ReadFile(path.Join(sysfsDevicePath, "vendor"))
	if err != nil {
		klog.Errorf("could not read vendor file for device at %s: %v", sysfsDevicePath, err)
		return "", "", ""
	}
	vendorId = strings.TrimSpace(string(vendorIdBytes))

	deviceIdBytes, err := os.ReadFile(path.Join(sysfsDevicePath, "device"))
	if err != nil {
		klog.Errorf("could not read device file for device at %s: %v", sysfsDevicePath, err)
		return "", "", ""
	}
	deviceId = strings.TrimSpace(string(deviceIdBytes))

	classIdBytes, err := os.ReadFile(path.Join(sysfsDevicePath, "class"))
	if err != nil {
		klog.Errorf("could not read class file for device at %s: %v", sysfsDevicePath, err)
		return "", "", ""
	}
	classId = strings.TrimSpace(string(classIdBytes))

	return vendorId, deviceId, classId
}

// readPCISubsystemInfo reads the subsystem_vendor and subsystem_device IDs of the device.
func readPCISubsystemInfo(sysfsDevicePath string) (subVendorId, subDeviceId string) {
	subVendorIdBytes, err := os.ReadFile(path.Join(sysfsDevicePath, "subsystem_vendor"))
	if err != nil {
		klog.V(5).Infof("could not read subsystem_vendor file for device at %s: %v", sysfsDevicePath, err)
	} else {
		subVendorId = strings.TrimSpace(string(subVendorIdBytes))
	}

	subDeviceIdBytes, err := os.ReadFile(path.Join(sysfsDevicePath, "subsystem_device"))
	if err != nil {
		klog.V(5).Infof("could not read subsystem_device file for device at %s: %v", sysfsDevicePath, err)
	} else {
		subDeviceId = strings.TrimSpace(string(subDeviceIdBytes))
	}

	return subVendorId, subDeviceId
}

// isInSurvivabilityMode tells whether the KMD probed the device in survivability mode, which
// happens when the device firmware is broken. The survivability_mode sysfs file exists only
// when the mode is on, and it disappears after a successful firmware reflashing and reprobe.
func isInSurvivabilityMode(sysfsDevicePath string) bool {
	_, err := os.Stat(path.Join(sysfsDevicePath, device.SysfsSurvivabilityModeFile))
	if err == nil {
		return true
	}

	if os.IsNotExist(err) {
		return false
	}

	klog.Warningf("could not stat survivability mode file for device at %s: %v", sysfsDevicePath, err)

	return false
}

func GetPCIDeviceDriver(sysfsDevicePath string) string {
	linkTarget, err := os.Readlink(path.Join(sysfsDevicePath, "driver"))
	if err != nil {
		if os.IsNotExist(err) {
			klog.V(5).Infof("device %v is unbound", path.Base(sysfsDevicePath))
		} else {
			klog.Errorf("could not read sysfs directory: %v", err)
		}
		return ""
	}
	return path.Base(linkTarget)
}
