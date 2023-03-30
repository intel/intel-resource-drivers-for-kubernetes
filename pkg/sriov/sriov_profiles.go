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
	"os"
	"path"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"
)

const (
	FairShareProfile = "fairShare"
	bytesInMB        = 1048576
)

var Profiles = map[string]map[string]uint64{
	// Flex 170
	"flex170_m1": {
		"contexts_quota":     1024,
		"doorbells_quota":    240,
		"exec_quantum_ms":    64,
		"ggtt_quota":         4026531840,
		"lmem_quota":         16106127360, // 15360 MiB
		"preempt_timeout_us": 128000,
		"numvfs":             1,
	},
	"flex170_m2": {
		"contexts_quota":     1024,
		"doorbells_quota":    120,
		"exec_quantum_ms":    32,
		"ggtt_quota":         2013265920, // 7680 MiB
		"lmem_quota":         8053063680,
		"preempt_timeout_us": 64000,
		"numvfs":             2,
	},
	"flex170_m4": {
		"contexts_quota":     1024,
		"doorbells_quota":    60,
		"exec_quantum_ms":    16,
		"ggtt_quota":         1006632960,
		"lmem_quota":         4026531840, // 3840 MiB
		"preempt_timeout_us": 32000,
		"numvfs":             4,
	},
	"flex170_m5": {
		"contexts_quota":     1024,
		"doorbells_quota":    48,
		"exec_quantum_ms":    12,
		"ggtt_quota":         805306368,
		"lmem_quota":         3221225472, // 3072 MiB
		"preempt_timeout_us": 24000,
		"numvfs":             5,
	},
	"flex170_m8": {
		"contexts_quota":     1024,
		"doorbells_quota":    30,
		"exec_quantum_ms":    8,
		"ggtt_quota":         503316480,
		"lmem_quota":         2013265920, // 1920 MiB
		"preempt_timeout_us": 16000,
		"numvfs":             8,
	},
	"flex170_m16": {
		"contexts_quota":     1024,
		"doorbells_quota":    15,
		"exec_quantum_ms":    4,
		"ggtt_quota":         251658240,
		"lmem_quota":         1006632960, // 960 MiB
		"preempt_timeout_us": 8000,
		"numvfs":             16,
	},
	// Flex 140
	"flex140_m1": {
		"contexts_quota":     1024,
		"doorbells_quota":    240,
		"exec_quantum_ms":    64,
		"ggtt_quota":         4026531840,
		"lmem_quota":         5368709120, // 5120 MiB
		"preempt_timeout_us": 128000,
		"numvfs":             1,
	},
	"flex140_m3": {
		"contexts_quota":     1024,
		"doorbells_quota":    80,
		"exec_quantum_ms":    22,
		"ggtt_quota":         1342177280,
		"lmem_quota":         1788870656, // 1706 MiB
		"preempt_timeout_us": 44000,
		"numvfs":             3,
	},
	"flex140_m6": {
		"contexts_quota":     1024,
		"doorbells_quota":    40,
		"exec_quantum_ms":    16,
		"ggtt_quota":         671088640,
		"lmem_quota":         893386752, // 852 MiB
		"preempt_timeout_us": 32000,
		"numvfs":             6,
	},
	"flex140_m12": {
		"contexts_quota":     1024,
		"doorbells_quota":    20,
		"exec_quantum_ms":    8,
		"ggtt_quota":         335544320,
		"lmem_quota":         446693376, // 426 MiB
		"preempt_timeout_us": 16000,
		"numvfs":             12,
	},
	// Max per gpu
	"max_c1": {
		"contexts_quota":     1024,
		"doorbells_quota":    240,
		"exec_quantum_ms":    64,
		"ggtt_quota":         4026531840,
		"lmem_quota":         64424509440, // 61440 MiB
		"preempt_timeout_us": 128000,
		"numvfs":             1,
	},
	"max_c2": {
		"contexts_quota":     1024,
		"doorbells_quota":    120,
		"exec_quantum_ms":    64,
		"ggtt_quota":         2013265920,
		"lmem_quota":         32212254720, // 30720 MiB
		"preempt_timeout_us": 128000,
		"numvfs":             2,
	},
	"max_c4": {
		"contexts_quota":     1024,
		"doorbells_quota":    60,
		"exec_quantum_ms":    64,
		"ggtt_quota":         1006632960,
		"lmem_quota":         16106127360, // 15360 MiB
		"preempt_timeout_us": 128000,
		"numvfs":             4,
	},
	"max_c8": {
		"contexts_quota":     1024,
		"doorbells_quota":    30,
		"exec_quantum_ms":    32,
		"ggtt_quota":         503316480,
		"lmem_quota":         8053063680, // 7680 MiB
		"preempt_timeout_us": 64000,
		"numvfs":             8,
	},
	"max_c16": {
		"contexts_quota":     1024,
		"doorbells_quota":    15,
		"exec_quantum_ms":    16,
		"ggtt_quota":         251658240,
		"lmem_quota":         4026531840, // 3840 MiB
		"preempt_timeout_us": 32000,
		"numvfs":             16,
	},
	"max_c32": {
		"contexts_quota":     1024,
		"doorbells_quota":    7,
		"exec_quantum_ms":    8,
		"ggtt_quota":         125829120,
		"lmem_quota":         2013265920, // 1920 MiB
		"preempt_timeout_us": 16000,
		"numvfs":             32,
	},
	"max_c63": {
		"contexts_quota":     1024,
		"doorbells_quota":    3,
		"exec_quantum_ms":    4,
		"ggtt_quota":         63897600,
		"lmem_quota":         1021313024, // 974 MiB
		"preempt_timeout_us": 8000,
		"numvfs":             32,
	},
	FairShareProfile: {
		"contexts_quota":     0,
		"doorbells_quota":    0,
		"exec_quantum_ms":    0,
		"ggtt_quota":         0,
		"lmem_quota":         0,
		"preempt_timeout_us": 0,
		"numvfs":             0,
	},
	// Max per tile TODO: TBD.
}

var VfAttributeFiles = []string{
	"contexts_quota",
	"doorbells_quota",
	"exec_quantum_ms",
	"ggtt_quota",
	"lmem_quota",
	"preempt_timeout_us",
}

// PerDeviceIdProfiles has to be ordered descending by size, the profile picking logic relies on this.
var PerDeviceIdProfiles = map[string][]string{
	// Flex170
	"0x56c0": {"flex170_m1", "flex170_m2", "flex170_m4", "flex170_m5", "flex170_m8", "flex170_m16"},
	// Flex140
	"0x56c1": {"flex140_m1", "flex140_m3", "flex140_m6", "flex140_m12"},
	// Ponte Vecchio XL (2 Tile)
	"0x0bd0": {"max_c1", "max_c2", "max_c4", "max_c8", "max_c16", "max_c32", "max_c63"},
	// Ponte Vecchio XT (2 Tile) [Data Center GPU Max 1550]
	"0x0bd5": {"max_c1", "max_c2", "max_c4", "max_c8", "max_c16", "max_c32", "max_c63"},
	// Ponte Vecchio XT (2 Tile) [Data Center GPU Max 1550]
	"0x0bd6": {"max_c1", "max_c2", "max_c4", "max_c8", "max_c16", "max_c32", "max_c63"},
	// Ponte Vecchio XT (2 Tile) [Data Center GPU Max 1350]
	"0x0bd7": {"max_c1", "max_c2", "max_c4", "max_c8", "max_c16", "max_c32", "max_c63"},
	// Ponte Vecchio XT (2 Tile) [Data Center GPU Max 1350]
	"0x0bd8": {"max_c1", "max_c2", "max_c4", "max_c8", "max_c16", "max_c32", "max_c63"},
	// Ponte Vecchio XT (1 Tile) [Data Center GPU Max 1100]
	"0x0bd9": {"max_c1", "max_c2", "max_c4", "max_c8", "max_c16", "max_c32", "max_c63"},
	// Ponte Vecchio XT (1 Tile) [Data Center GPU Max 1100]
	"0x0bda": {"max_c1", "max_c2", "max_c4", "max_c8", "max_c16", "max_c32", "max_c63"},
	// Ponte Vecchio XT (1 Tile) [Data Center GPU Max 1100]
	"0x0bdb": {"max_c1", "max_c2", "max_c4", "max_c8", "max_c16", "max_c32", "max_c63"},
}

// Based on memory request - select suitable VF profile.
// Can be used in case fair share is not suitable.
func PickVFProfile(deviceId string, vfMemoryRequestMiB int) (string, error) {
	availableProfileNames, found := PerDeviceIdProfiles[deviceId]

	if !found {
		klog.Infof("No VF profiles for device %v, using %v", deviceId, FairShareProfile)
		return FairShareProfile, nil
	}

	lmemRequestBytes := uint64(vfMemoryRequestMiB) * bytesInMB

	// iterate over list of profiles backwards - find the smallest VF that fits
	// request to provision as many VFs as possible
	for profileIdx := len(availableProfileNames) - 1; profileIdx >= 0; profileIdx-- {
		if Profiles[availableProfileNames[profileIdx]]["lmem_quota"] >= lmemRequestBytes {
			klog.V(5).Infof("Picking profile %v", availableProfileNames[profileIdx])
			return availableProfileNames[profileIdx], nil
		}
	}

	return "", fmt.Errorf("Could not select suitable VF Profile")
}

// Return maximum number of VFs that PF resources can be split fairly into to suit
// for requested VFs combination.
// Example 1: 16 GiB GPU memory can be split into 4, 4, 4, 4 to
// serve two VFs that requested 2 and 4 GiB respectively. Four VFs can be provisioned by
// simple fair-share provisioning.
// Example 2: 16GiB GPU memory cannot be evenly split to server VFs that requested
// 8, 2, 2 GiB respectively because minimum fair-share split 16 / 3 would yield less
// memory (5.3 GiB) per VF than the biggest memory request (8).
func MaxFairVFs(deviceId string, vfs []int) (int, error) {
	var profile map[string]uint64
	minFairNum := uint64(len(vfs))

	// before looking for profile, check if profile-specific parameters are present
	needProfiles := false
	for _, vf := range vfs {
		if vf != 0 { // if vf.Memory != 0 {
			needProfiles = true
		}
	}
	if !needProfiles {
		return len(vfs), nil
	}

	fairSplitOK := false
	deviceProfiles, found := PerDeviceIdProfiles[deviceId]
	if !found {
		return 0, fmt.Errorf("No VF profiles for device %v", deviceId)
	}

	// iterate over device profiles, they are ordered by VFs number
	// find maximum number of VFs that still suits requested memory
	for _, vfProfileName := range deviceProfiles {
		if Profiles[vfProfileName]["numvfs"] < minFairNum {
			continue
		}
		profileSuitable := true
		for _, vf := range vfs {
			profileLmemQuotaMiB := Profiles[vfProfileName]["lmem_quota"] / 1048576
			// stop checking rest of VFs if even one cannot fit into profile
			if uint64(vf) > profileLmemQuotaMiB {
				profileSuitable = false
				break
			}
		}
		// no point iterating further - rest of VF profiles have even less lmem_quota
		if !profileSuitable {
			klog.V(5).Infof("Profile %v is not suitable", vfProfileName)
			break
		}
		klog.V(5).Infof("Profile %v is suitable", vfProfileName)
		fairSplitOK = true
		profile = Profiles[vfProfileName]
	}

	if !fairSplitOK {
		klog.V(5).Infof("Could not split PF resources fairly")
		return 0, fmt.Errorf("Could not split PF resources fairly")
	}

	return int(profile["numvfs"]), nil
}

// Set custom VF settings from profile for manual provisioning mode, in case fair share is not suitable.
// pf/auto_provisioning will be automatically set to 0 in this case.
func PreConfigureVF(vfAttrsPath string, drmVfIndex int, vfProfile string) error {
	for _, attrName := range VfAttributeFiles {
		attrValue := Profiles[vfProfile][attrName]
		vfAttrFile := path.Join(vfAttrsPath, fmt.Sprintf("vf%v/gt/%v", drmVfIndex, attrName))
		klog.V(3).Infof("setting %v", attrName)
		fhandle, err := os.OpenFile(vfAttrFile, os.O_APPEND|os.O_WRONLY, os.ModeAppend)
		if err != nil {
			klog.Errorf("Failed to open %v file: %v", vfAttrFile, err)
			return fmt.Errorf("Failed to open %v file: %v", vfAttrFile, err)
		}

		_, err = fhandle.WriteString(fmt.Sprint(attrValue))
		if err != nil {
			klog.Errorf("Could not write to file %v: %v", vfAttrFile, err)
			return fmt.Errorf("Could not write to file %v: %v", vfAttrFile, err)
		}
		time.Sleep(250 * time.Millisecond)
	}

	return nil
}

// Set VF settings to auto mode, writing 0 to all configuration files.
// It is important to set pf/auto_provisioning to 1 after all VFs are unconfigured.
func UnConfigureVF(attrsDir string) error {
	for _, vfAttrName := range VfAttributeFiles {
		vfAttrFile := path.Join(attrsDir, vfAttrName)

		klog.V(3).Infof("Resetting VF file %v", vfAttrFile)
		fhandle, err := os.OpenFile(vfAttrFile, os.O_APPEND|os.O_WRONLY, os.ModeAppend)
		if err != nil {
			if errors.IsNotFound(err) {
				klog.V(5).Infof("Ignoring missing VF attribute file %v", vfAttrName)
				continue
			}
			klog.Errorf("Failed to open %v file: %v", vfAttrFile, err)
			return fmt.Errorf("Failed to open %v file: %v", vfAttrFile, err)
		}

		_, err = fhandle.WriteString("0")
		if err != nil {
			klog.Errorf("Could not write to file %v: %v", vfAttrFile, err)
			return fmt.Errorf("Could not write to file %v: %v", vfAttrFile, err)
		}

		if err = fhandle.Close(); err != nil {
			klog.Errorf("Could not close file %v: %v", vfAttrFile, err)
			// do not fail here, main job is done by now
		}
		time.Sleep(250 * time.Millisecond)
	}
	return nil
}
