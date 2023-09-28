/*
 * Copyright (c) 2023, Intel Corporation.  All Rights Reserved.
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

package sriov

import (
	"fmt"
	"strconv"
	"strings"

	"k8s.io/klog/v2"
)

const (
	// PF: 0000:03:00.0-0xXXXX is 19.
	// VF: 0000:03:00.0-0xXXXX-vfY is 23.
	PFUIDLength = 19
	// The length of VF UID is at least 23, can be more if number of VF is double-digit.
	MinVFUIDLength = 23
)

// PfUIDFromVfUID returns PF UID parsed from VF UID.
// VF contains fully PF UID and VF index, for instance '0000:03:00.0-0x1234-vf0'.
func PfUIDFromVfUID(uid string) (string, error) {
	if len(uid) < MinVFUIDLength {
		return "", fmt.Errorf("not a VF")
	}

	parts := strings.Split(uid, "-vf")
	if len(parts) != 2 {
		klog.Errorf("malformed VF UID: %v", uid)
		return "", fmt.Errorf("malformed VF UID: %v", uid)
	}

	return parts[0], nil
}

// VfIndexFromUID returns PCI VF index from device UID.
// VF UID contains full PF UID and VF index, for instance '0000:03:00.0-0x1234-vf0'.
func VFIndexFromUID(uid string) (uint64, error) {
	if len(uid) < MinVFUIDLength {
		return 0, fmt.Errorf("not a VF")
	}

	parts := strings.Split(uid, "-vf")
	if len(parts) != 2 {
		klog.Errorf("malformed VF UID: %v", uid)
		return 0, fmt.Errorf("malformed VF UID: %v", uid)
	}

	vfIndex, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil {
		klog.Errorf("failed to parse VF index from VF UID %v", uid)
		return 0, fmt.Errorf("failed to parse VF index from VF UID %v", uid)
	}

	return vfIndex, nil
}
