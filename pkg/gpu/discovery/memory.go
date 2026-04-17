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

package discovery

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"

	"k8s.io/klog/v2"
)

const (
	IoctlXeDeviceQuery    = 0xc0286440
	IoctlI915Query        = 0xc0106479
	XeQueryTypeMemRegions = 0x1
	XeMemRegionClassVRAM  = 0x1
	I915QueryMemRegions   = 0x4
	I915MemClassDevice    = 0x1
)

// Xe KMD uAPI: https://docs.kernel.org/gpu/driver-uapi.html#drm-xe-uapi .
type XeDeviceQuery struct {
	Extensions uint64
	Query      uint32
	Size       uint32
	Data       uint64
	Reserved   [2]uint64
}
type XeMemRegion struct {
	Mem_class        uint16
	Instance         uint16
	Min_page_size    uint32
	Total_size       uint64
	Used             uint64
	Cpu_visible_size uint64
	Cpu_visible_used uint64
	Reserved         [6]uint64
}
type XeQueryMemRegions struct {
	Mem_regions uint32
	Pad         uint32
}

// i915 structs KMD uAPI: https://docs.kernel.org/gpu/driver-uapi.html#drm-i915-uapi .
type I915Query struct {
	Num_items uint32
	Flags     uint32
	Items_ptr uint64
}
type I915QueryItem struct {
	Query_id uint64
	Length   int32
	Flags    uint32
	Data_ptr uint64
}
type I915QueryMemoryRegions struct {
	Regions uint32
	Rsvd    [3]uint32
}
type I915MemoryRegionInfo struct {
	Region           I915GemMemClassInstance
	Rsvd0            uint32
	Probed_size      uint64
	Unallocated_size uint64
	Rsvd1            [8]uint64
}
type I915GemMemClassInstance struct {
	Class    uint16
	Instance uint16
}

// xeDeviceQueryBuf issues a two-step DRM_IOCTL_XE_DEVICE_QUERY:
// step 1 with size=0 to get required buffer size, step 2 to fill the buffer.
func xeDeviceQueryBuf(fd uintptr, queryType uint32) ([]byte, error) {
	klog.V(5).Info("xe ioctl", "queryType", queryType)
	q := XeDeviceQuery{Query: queryType}
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, IoctlXeDeviceQuery,
		uintptr(unsafe.Pointer(&q))); errno != 0 { //nolint:gosec // unsafe.Pointer required for DRM_IOCTL_XE_DEVICE_QUERY ioctl
		klog.V(5).Info("xe ioctl size-probe failed", "queryType", queryType, "errno", errno) //nolint:gosec // G706: errno is syscall.Errno, not user input
		return nil, errno
	}
	if q.Size == 0 {
		return nil, fmt.Errorf("xe query %d: zero size", queryType)
	}
	buf := make([]byte, q.Size)
	q.Data = uint64(uintptr(unsafe.Pointer(&buf[0]))) //nolint:gosec // unsafe.Pointer required for DRM_IOCTL_XE_DEVICE_QUERY ioctl data
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, IoctlXeDeviceQuery,
		uintptr(unsafe.Pointer(&q))); errno != 0 { //nolint:gosec // unsafe.Pointer required for DRM_IOCTL_XE_DEVICE_QUERY ioctl
		klog.V(5).Info("xe ioctl data call failed", "queryType", queryType, "errno", errno) //nolint:gosec // G706: errno is syscall.Errno, not user input
		return nil, errno
	}
	klog.V(5).Info("xe ioctl done", "queryType", queryType, "bytes", q.Size)
	return buf, nil
}

func xeReadMemoryMiB(fd uintptr) (uint64, error) {
	klog.V(5).Info("xeReadMemory")

	buf, err := xeDeviceQueryBuf(fd, XeQueryTypeMemRegions)
	if err != nil {
		return 0, err
	}

	hdrSize := int(unsafe.Sizeof(XeQueryMemRegions{}))
	regionSize := int(unsafe.Sizeof(XeMemRegion{}))
	if len(buf) < hdrSize {
		return 0, fmt.Errorf("buffer too small for mem regions header: %v bytes", len(buf))
	}

	var totalMemoryBytes uint64
	hdr := (*XeQueryMemRegions)(unsafe.Pointer(&buf[0])) //nolint:gosec // unsafe.Pointer required for DRM xe mem_regions buffer cast
	for i := range int(hdr.Mem_regions) {
		off := hdrSize + i*regionSize
		if off+regionSize > len(buf) {
			break
		}
		r := (*XeMemRegion)(unsafe.Pointer(&buf[off])) //nolint:gosec // unsafe.Pointer required for DRM xe mem_region buffer cast
		if r.Mem_class == XeMemRegionClassVRAM {
			klog.V(5).Infof("Detected VRAM memory region with total size: %v bytes", r.Total_size)
			totalMemoryBytes += r.Total_size
		}
	}

	return totalMemoryBytes / (1024 * 1024), nil
}

func i915ReadMemoryMiB(fd uintptr) (uint64, error) {
	klog.V(5).Info("i915ReadMemory")

	// Step 1: ask for size with length=0.
	item := I915QueryItem{Query_id: I915QueryMemRegions}
	q := I915Query{
		Num_items: 1,
		Items_ptr: uint64(uintptr(unsafe.Pointer(&item))), //nolint:gosec // unsafe.Pointer required for DRM_IOCTL_I915_QUERY items_ptr
	}
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, IoctlI915Query,
		uintptr(unsafe.Pointer(&q))); errno != 0 { //nolint:gosec // unsafe.Pointer required for DRM_IOCTL_I915_QUERY ioctl
		return 0, fmt.Errorf("i915 query mem regions size probe failed: %v", errno) //nolint:gosec // G706: errno is syscall.Errno, not user input
	}
	if item.Length <= 0 {
		return 0, fmt.Errorf("i915 query mem regions returned non-positive length: %v", item.Length)
	}

	// Step 2: allocate and fill buffer.
	buf := make([]byte, item.Length)
	item.Data_ptr = uint64(uintptr(unsafe.Pointer(&buf[0]))) //nolint:gosec // unsafe.Pointer required for DRM_IOCTL_I915_QUERY data_ptr
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, IoctlI915Query,
		uintptr(unsafe.Pointer(&q))); errno != 0 { //nolint:gosec // unsafe.Pointer required for DRM_IOCTL_I915_QUERY ioctl
		return 0, fmt.Errorf("i915 query mem regions data call failed: %v", errno) //nolint:gosec // G706: errno is syscall.Errno, not user input
	}

	hdrSize := int(unsafe.Sizeof(I915QueryMemoryRegions{}))
	regionSize := int(unsafe.Sizeof(I915MemoryRegionInfo{}))
	if len(buf) < hdrSize {
		return 0, fmt.Errorf("buffer too small for mem regions header: %v bytes", len(buf))
	}

	var totalMemoryBytes uint64
	hdr := (*I915QueryMemoryRegions)(unsafe.Pointer(&buf[0])) //nolint:gosec // unsafe.Pointer required for DRM i915 memory_regions buffer cast
	for i := range int(hdr.Regions) {
		off := hdrSize + i*regionSize
		if off+regionSize > len(buf) {
			break
		}
		r := (*I915MemoryRegionInfo)(unsafe.Pointer(&buf[off])) //nolint:gosec // unsafe.Pointer required for DRM i915 memory_region_info buffer cast
		if r.Region.Class == I915MemClassDevice {
			totalMemoryBytes += r.Probed_size
		}
	}

	return totalMemoryBytes / (1024 * 1024), nil
}

// GetI915DeviceMemoryMiB queries memory for an i915-driver GPU.
// Argument drmCardDev is a full path to the DRM device, e.g. /dev/dri/card0.
func GetI915DeviceMemoryMiB(drmCardDev string) (uint64, error) {
	f, err := openDRMDevice(drmCardDev)
	if err != nil {
		return 0, err
	}
	defer f.Close() //nolint:errcheck // DRM device Close does not return meaningful errors

	fd := f.Fd()
	return i915ReadMemoryMiB(fd)
}

// GetXeDeviceMemoryMiB queries memory for an xe-driver GPU.
// Argument drmCardDev is a full path to the DRM device, e.g. /dev/dri/card0.
func GetXeDeviceMemoryMiB(drmCardDev string) (uint64, error) {
	f, err := openDRMDevice(drmCardDev)
	if err != nil {
		return 0, err
	}
	defer f.Close() //nolint:errcheck // DRM device Close does not return meaningful errors

	fd := f.Fd()
	return xeReadMemoryMiB(fd)
}

// openDRMDevice opens the card DRM device node directly.
// Render nodes (renderD*) block xe-driver ioctls with EACCES; the card node
// works for both xe and i915 without requiring DRM master.
func openDRMDevice(drmCardDev string) (*os.File, error) {
	klog.V(5).Infof("openDRMDevice dev: %v", drmCardDev)
	f, err := os.Open(drmCardDev)
	// Error is normal when unprivileged mode, therefore print only when verbose logging is enabled.
	if err != nil && klog.V(5).Enabled() {
		klog.Errorf("openDRMDevice failed. dev: %v, err: %v", drmCardDev, err)
	}
	return f, err
}
