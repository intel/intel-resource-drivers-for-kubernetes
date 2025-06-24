package helpers

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"k8s.io/klog/v2"
)

const (
	SysfsEnvVarName  = "SYSFS_ROOT"
	sysfsDefaultRoot = "/sys"

	DevfsEnvVarName  = "DEVFS_ROOT"
	devfsDefaultRoot = "/dev"

	PCIAddressLength = len("0000:00:00.0")
)

// GetSysfsRoot tries to get path where sysfs is mounted from
// env var, or fallback to hardcoded path.
func GetSysfsRoot(sysfsPath string) string {
	sysfsRoot, found := os.LookupEnv(SysfsEnvVarName)

	if found {
		if _, err := os.Stat(path.Join(sysfsRoot, sysfsPath)); err == nil {
			fmt.Printf("using custom sysfs location: %v\n", sysfsRoot)
			return sysfsRoot
		} else {
			fmt.Printf("could not find sysfs at '%v' from %v env var: %v\n", sysfsPath, SysfsEnvVarName, err)
		}
	}

	fmt.Printf("using default sysfs location: %v\n", sysfsDefaultRoot)
	// If /sys is not available, devices discovery will fail gracefully.
	return sysfsDefaultRoot
}

func GetDevRoot(devfsRootEnvVarName string, devPath string) string {
	devfsRoot, found := os.LookupEnv(devfsRootEnvVarName)

	if found {
		if _, err := os.Stat(path.Join(devfsRoot, devPath)); err == nil {
			fmt.Printf("using custom devfs location: %v\n", devfsRoot)
			return devfsRoot
		} else {
			fmt.Printf("could not find devfs at '%v' from %v env var: %v\n", devPath, devfsRootEnvVarName, err)
		}
	}

	fmt.Printf("using default devfs root: %v\n", devfsDefaultRoot)
	return devfsDefaultRoot
}

func PciInfoFromDeviceUID(deviceUID string) (string, string) {
	// 0000-00-01-0-0x0000 -> 0000:00:01.0, 0x0000
	rfc1123PCIaddress := deviceUID[:PCIAddressLength]
	pciAddress := strings.Replace(strings.Replace(rfc1123PCIaddress, "-", ":", 2), "-", ".", 1)
	deviceId := deviceUID[PCIAddressLength+1:]

	return pciAddress, deviceId
}

func DeviceUIDFromPCIinfo(pciAddress string, pciid string) string {
	// 0000:00:01.0, 0x0000 -> 0000-00-01-0-0x0000
	// Replace colons and the dot in PCI address with hyphens.
	rfc1123PCIaddress := strings.ReplaceAll(strings.ReplaceAll(pciAddress, ":", "-"), ".", "-")
	newUID := fmt.Sprintf("%v-%v", rfc1123PCIaddress, pciid)

	return newUID
}

func DeterminePCIRoot(link string) string {
	// e.g. /sys/devices/pci0000:16/0000:16:02.0/0000:17:00.0/0000:18:00.0/0000:19:00.0
	linkTarget, err := filepath.EvalSymlinks(link)
	if err != nil {
		klog.Errorf("Could not determine PCI root complex ID from '%v': %v", link, err)
		return ""
	}
	klog.V(5).Infof("PCI device location: %v", linkTarget)
	parts := strings.Split(linkTarget, "/")
	if len(parts) > 3 && parts[0] == "" && parts[2] == "devices" {
		return strings.Replace(parts[3], "pci0000:", "", 1)
	}
	klog.Warningf("could not parse sysfs link target %v: %v", linkTarget, parts)

	return ""
}
