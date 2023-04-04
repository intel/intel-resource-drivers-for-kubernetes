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
	PFUIDLength    = 19
	MinVFUIDLength = 23
)

// Return PF UID parsed from VF UID.
// VF contains fully PF UID and VF index, for instance '0000:03:00.0-vf0-0x1234'.
func PfUIDFromVfUID(uid string) (string, error) {
	if len(uid) < MinVFUIDLength {
		return "", fmt.Errorf("Not a VF")
	}

	parts := strings.Split(uid, "-vf")
	if len(parts) != 2 {
		klog.Errorf("Malformed VF UID: %v", uid)
		return "", fmt.Errorf("Malformed VF UID: %v", uid)
	}

	return parts[0], nil
}

// Return PF UID parsed from VF UID.
// VF contains fully PF UID and VF index, for instance '0000:03:00.0-vf0-0x1234'.
func VFIndexFromUID(uid string) (int, error) {
	if len(uid) < MinVFUIDLength {
		return 0, fmt.Errorf("Not a VF")
	}

	parts := strings.Split(uid, "-vf")
	if len(parts) != 2 {
		klog.Errorf("Malformed VF UID: %v", uid)
		return 0, fmt.Errorf("Malformed VF UID: %v", uid)
	}

	vfIndex, err := strconv.Atoi(parts[1])
	if err != nil {
		klog.Errorf("Failed to parse VF index from VF UID %v", uid)
		return 0, fmt.Errorf("Failed to parse VF index from VF UID %v", uid)
	}

	return vfIndex, nil
}
