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
	"strconv"
	"strings"
)

// newPCIAddress finds next available free PCI address in given directory.
// Returns partial PCI address without function, "0000:00:00.", used in loop
// when fake VFs are generated.
func newPCIAddress(driverDir string, currentAddress string) (string, error) {
	domain, err1 := strconv.ParseUint(currentAddress[:4], 10, 64)
	bus, err2 := strconv.ParseUint(currentAddress[5:7], 10, 64)
	device, err3 := strconv.ParseUint(currentAddress[8:10], 10, 64)

	if err1 != nil || err2 != nil || err3 != nil {
		return "", fmt.Errorf("could not parse current PCI address %v", currentAddress)
	}

	for ; domain <= 65535; domain++ {
		for ; bus <= 255; bus++ {
			for ; device <= 255; device++ {
				// partial PCI address without function
				newAddress := fmt.Sprintf("%04x:%02x:%02x.", domain, bus, device)
				// add zero for PCI function part of the address
				newSysfsDeviceDir := path.Join(driverDir, fmt.Sprintf("%s0", newAddress))
				if _, err := os.Stat(newSysfsDeviceDir); err != nil {
					return newAddress, nil
				}
			}
		}
	}

	return "", fmt.Errorf("no addresses left")
}

// sanitizeFakeSysFsDir ensuring the /tmp location of fake sysfs.
func sanitizeFakeSysFsDir(sysfsRootUntrusted string) error {
	// fake sysfsroot should be deletable.
	// To prevent disaster mistakes, it is enforced to be in /tmp.
	sysfsRoot := path.Join(sysfsRootUntrusted)
	if !strings.HasPrefix(sysfsRoot, "/tmp") {
		return fmt.Errorf("fake sysfsroot can only be in /tmp, got: %v", sysfsRoot)
	}

	return nil
}
