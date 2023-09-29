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

	"k8s.io/klog/v2"
)

const (
	// Name of the profile in Profiles that resets VF to fair-share provisioning.
	FairShareProfile = "fairShare"
)

// Profiles is a set of all supported profiles and per-sysfs-file values.
var Profiles = map[string]map[string]uint64{
	// Flex 170
	"flex170_m1": {
		"contexts_quota":     1024,
		"doorbells_quota":    240,
		"exec_quantum_ms":    64,
		"ggtt_quota":         4026531840,
		"lmem_quota":         16911433728, // 16128 MiB
		"lmem_quota_ecc_on":  14334453350, // ~13670.4 MiB
		"preempt_timeout_us": 128000,
		"numvfs":             1,
	},
	"flex170_m2": {
		"contexts_quota":     1024,
		"doorbells_quota":    120,
		"exec_quantum_ms":    32,
		"ggtt_quota":         2013265920,
		"lmem_quota":         8455716864, // 8064 MiB
		"lmem_quota_ecc_on":  7167226675, // ~6835.2 MiB
		"preempt_timeout_us": 64000,
		"numvfs":             2,
	},
	"flex170_m4": {
		"contexts_quota":     1024,
		"doorbells_quota":    60,
		"exec_quantum_ms":    16,
		"ggtt_quota":         1006632960,
		"lmem_quota":         4227858432, // 4032 MiB
		"lmem_quota_ecc_on":  3583613337, // ~3417.6 MiB
		"preempt_timeout_us": 32000,
		"numvfs":             4,
	},
	"flex170_m5": {
		"contexts_quota":     1024,
		"doorbells_quota":    48,
		"exec_quantum_ms":    12,
		"ggtt_quota":         805306368,
		"lmem_quota":         3380609024, // 3224 MiB
		"lmem_quota_ecc_on":  2866890670, // ~2734.1 MiB
		"preempt_timeout_us": 24000,
		"numvfs":             5,
	},
	"flex170_m8": {
		"contexts_quota":     1024,
		"doorbells_quota":    30,
		"exec_quantum_ms":    8,
		"ggtt_quota":         503316480,
		"lmem_quota":         2113929216, // 2016 MiB
		"lmem_quota_ecc_on":  1791806668, // ~1708.8 MiB
		"preempt_timeout_us": 16000,
		"numvfs":             8,
	},
	"flex170_m16": {
		"contexts_quota":     1024,
		"doorbells_quota":    15,
		"exec_quantum_ms":    4,
		"ggtt_quota":         251658240,
		"lmem_quota":         1056964608, // 1008 MiB
		"lmem_quota_ecc_on":  895903334,  // ~854.4 MiB
		"preempt_timeout_us": 8000,
		"numvfs":             16,
	},
	// Flex 140
	"flex140_m1": {
		"contexts_quota":     1024,
		"doorbells_quota":    240,
		"exec_quantum_ms":    64,
		"ggtt_quota":         4026531840,
		"lmem_quota":         6174015488, // 5888 MiB
		"lmem_quota_ecc_on":  5014290432, // 4782 MiB
		"preempt_timeout_us": 128000,
		"numvfs":             1,
	},
	"flex140_m3": {
		"contexts_quota":     1024,
		"doorbells_quota":    80,
		"exec_quantum_ms":    22,
		"ggtt_quota":         1342177280,
		"lmem_quota":         2057306112, // 1962 MiB
		"lmem_quota_ecc_on":  1671430144, // 1594 MiB
		"preempt_timeout_us": 44000,
		"numvfs":             3,
	},
	"flex140_m6": {
		"contexts_quota":     1024,
		"doorbells_quota":    40,
		"exec_quantum_ms":    16,
		"ggtt_quota":         671088640,
		"lmem_quota":         1027604480, // 980 MiB
		"lmem_quota_ecc_on":  834666496,  // 796 MiB
		"preempt_timeout_us": 32000,
		"numvfs":             6,
	},
	"flex140_m12": {
		"contexts_quota":     1024,
		"doorbells_quota":    20,
		"exec_quantum_ms":    8,
		"ggtt_quota":         335544320,
		"lmem_quota":         513802240, // 490 MiB
		"lmem_quota_ecc_on":  417333248, // 398 MiB
		"preempt_timeout_us": 16000,
		"numvfs":             12,
	},
	// Max Series
	"max_128g_c1": {
		"contexts_quota":     1024,
		"doorbells_quota":    240,
		"exec_quantum_ms":    64,
		"ggtt_quota":         4026531840,
		"lmem_quota":         64424509440, // 61440 MiB
		"lmem_quota_ecc_on":  64424509440, // 61440 MiB
		"preempt_timeout_us": 128000,
		"numvfs":             1,
	},
	"max_128g_c2": {
		"contexts_quota":     1024,
		"doorbells_quota":    240,
		"exec_quantum_ms":    64,
		"ggtt_quota":         2013265920,
		"lmem_quota":         32212254720, // 30720 MiB
		"lmem_quota_ecc_on":  32212254720, // 30720 MiB
		"preempt_timeout_us": 128000,
		"numvfs":             2,
	},
	"max_128g_c4": {
		"contexts_quota":     1024,
		"doorbells_quota":    120,
		"exec_quantum_ms":    64,
		"ggtt_quota":         1006632960,
		"lmem_quota":         16106127360, // 15360 MiB
		"lmem_quota_ecc_on":  16106127360, // 15360 MiB
		"preempt_timeout_us": 128000,
		"numvfs":             4,
	},
	"max_128g_c8": {
		"contexts_quota":     1024,
		"doorbells_quota":    60,
		"exec_quantum_ms":    32,
		"ggtt_quota":         503316480,
		"lmem_quota":         8053063680, // 7680 MiB
		"lmem_quota_ecc_on":  8053063680, // 7680 MiB
		"preempt_timeout_us": 64000,
		"numvfs":             8,
	},
	"max_128g_c16": {
		"contexts_quota":     1024,
		"doorbells_quota":    30,
		"exec_quantum_ms":    16,
		"ggtt_quota":         251658240,
		"lmem_quota":         4026531840, // 3840 MiB
		"lmem_quota_ecc_on":  4026531840, // 3840 MiB
		"preempt_timeout_us": 32000,
		"numvfs":             16,
	},
	"max_128g_c32": {
		"contexts_quota":     1024,
		"doorbells_quota":    15,
		"exec_quantum_ms":    8,
		"ggtt_quota":         125829120,
		"lmem_quota":         2013265920, // 1920 MiB
		"lmem_quota_ecc_on":  2013265920, // 1920 MiB
		"preempt_timeout_us": 16000,
		"numvfs":             32,
	},
	"max_128g_c62": {
		"contexts_quota":     1024,
		"doorbells_quota":    7,
		"exec_quantum_ms":    4,
		"ggtt_quota":         64880640,
		"lmem_quota":         1038090240, // 990 MiB
		"lmem_quota_ecc_on":  1038090240, // 990 MiB
		"preempt_timeout_us": 8000,
		"numvfs":             62,
	},
	"max_48g_c1": {
		"contexts_quota":     1024,
		"doorbells_quota":    240,
		"exec_quantum_ms":    64,
		"ggtt_quota":         4026531840,
		"lmem_quota":         47244640256, // 45056 MiB
		"lmem_quota_ecc_on":  47244640256, // 45056 MiB
		"preempt_timeout_us": 128000,
		"numvfs":             1,
	},
	"max_48g_c2": {
		"contexts_quota":     1024,
		"doorbells_quota":    120,
		"exec_quantum_ms":    64,
		"ggtt_quota":         2013265920,
		"lmem_quota":         23622320128, // 22528 MiB
		"lmem_quota_ecc_on":  23622320128, // 22528 MiB
		"preempt_timeout_us": 128000,
		"numvfs":             2,
	},
	"max_48g_c4": {
		"contexts_quota":     1024,
		"doorbells_quota":    60,
		"exec_quantum_ms":    64,
		"ggtt_quota":         1006632960,
		"lmem_quota":         11811160064, // 11264 MiB
		"lmem_quota_ecc_on":  11811160064, // 11264 MiB
		"preempt_timeout_us": 128000,
		"numvfs":             4,
	},
	"max_48g_c8": {
		"contexts_quota":     1024,
		"doorbells_quota":    30,
		"exec_quantum_ms":    32,
		"ggtt_quota":         503316480,
		"lmem_quota":         5905580032, // 5632 MiB
		"lmem_quota_ecc_on":  5905580032, // 5632 MiB
		"preempt_timeout_us": 64000,
		"numvfs":             8,
	},
	"max_48g_c16": {
		"contexts_quota":     1024,
		"doorbells_quota":    15,
		"exec_quantum_ms":    16,
		"ggtt_quota":         251658240,
		"lmem_quota":         2952790016, // 2816 MiB
		"lmem_quota_ecc_on":  2952790016, // 2816 MiB
		"preempt_timeout_us": 32000,
		"numvfs":             16,
	},
	"max_48g_c32": {
		"contexts_quota":     1024,
		"doorbells_quota":    7,
		"exec_quantum_ms":    8,
		"ggtt_quota":         125829120,
		"lmem_quota":         1476395008, // 1408 MiB
		"lmem_quota_ecc_on":  1476395008, // 1408 MiB
		"preempt_timeout_us": 16000,
		"numvfs":             32,
	},
	"max_48g_c63": {
		"contexts_quota":     1024,
		"doorbells_quota":    3,
		"exec_quantum_ms":    4,
		"ggtt_quota":         63897600,
		"lmem_quota":         738197504, // 704 MiB
		"lmem_quota_ecc_on":  738197504, // 704 MiB
		"preempt_timeout_us": 8000,
		"numvfs":             63,
	},
	FairShareProfile: {
		"contexts_quota":     0,
		"doorbells_quota":    0,
		"exec_quantum_ms":    0,
		"ggtt_quota":         0,
		"lmem_quota":         0,
		"lmem_quota_ecc_on":  0,
		"preempt_timeout_us": 0,
		"numvfs":             0,
	},
	// Max per tile TODO: TBD.
}

// VfAttributeFiles is a list of filenames that needs to be configured for a VF
// profile to be applied.
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

	// Ponte Vecchio XL (2 Tile) [Data Center GPU Max 1450]
	"0x0b69": {"max_128g_c1", "max_128g_c2", "max_128g_c4", "max_128g_c8", "max_128g_c16", "max_128g_c32", "max_128g_c62"},
	// Ponte Vecchio XL (2 Tile)
	"0x0bd0": {"max_128g_c1", "max_128g_c2", "max_128g_c4", "max_128g_c8", "max_128g_c16", "max_128g_c32", "max_128g_c62"},
	// Ponte Vecchio XT (2 Tile) [Data Center GPU Max 1550]
	"0x0bd5": {"max_128g_c1", "max_128g_c2", "max_128g_c4", "max_128g_c8", "max_128g_c16", "max_128g_c32", "max_128g_c62"},
	// Ponte Vecchio XT (2 Tile) [Data Center GPU Max 1550]
	"0x0bd6": {"max_128g_c1", "max_128g_c2", "max_128g_c4", "max_128g_c8", "max_128g_c16", "max_128g_c32", "max_128g_c62"},

	// Ponte Vecchio XT (1 Tile) [Data Center GPU Max 1100] 48G
	"0x0bd9": {"max_48g_c1", "max_48g_c2", "max_48g_c4", "max_48g_c8", "max_48g_c16", "max_48g_c32", "max_48g_c63"},
	// Ponte Vecchio XT (1 Tile) [Data Center GPU Max 1100] 48G
	"0x0bda": {"max_48g_c1", "max_48g_c2", "max_48g_c4", "max_48g_c8", "max_48g_c16", "max_48g_c32", "max_48g_c63"},
	// Ponte Vecchio XT (1 Tile) [Data Center GPU Max 1100] 48G
	"0x0bdb": {"max_48g_c1", "max_48g_c2", "max_48g_c4", "max_48g_c8", "max_48g_c16", "max_48g_c32", "max_48g_c63"},
}

// PerDeviceIdDefaultProfiles specifies name of default profile that should be
// used for particular PCI deviceId.
var PerDeviceIdDefaultProfiles = map[string]string{
	// Flex170
	"0x56c0": "flex170_m8",
	// Flex140
	"0x56c1": "flex140_m6",

	// Ponte Vecchio XL (2 Tile) [Data Center GPU Max 1450]
	"0x0b69": "max_128g_c16",
	// Ponte Vecchio XL (2 Tile)
	"0x0bd0": "max_128g_c16",
	// Ponte Vecchio XT (2 Tile) [Data Center GPU Max 1550]
	"0x0bd5": "max_128g_c16",
	// Ponte Vecchio XT (2 Tile) [Data Center GPU Max 1550]
	"0x0bd6": "max_128g_c16",

	// Ponte Vecchio XT (1 Tile) [Data Center GPU Max 1100] 48G
	"0x0bd9": "max_48g_c8",
	// Ponte Vecchio XT (1 Tile) [Data Center GPU Max 1100] 48G
	"0x0bda": "max_48g_c8",
	// Ponte Vecchio XT (1 Tile) [Data Center GPU Max 1100] 48G
	"0x0bdb": "max_48g_c8",
}

// getProfileLmemQuotaMiBNoErr is safe to use internally, the profile tables are unit-tested.
func getProfileLmemQuotaMiBNoErr(profileName string, eccOn bool) uint64 {
	lmemQuotaMiB, _ := GetProfileLmemQuotaMiB(profileName, eccOn)
	return lmemQuotaMiB
}

// GetProfileLmemQuotaMiB returns amount of memory in MiB given profile provides
// if it exists, or error.
func GetProfileLmemQuotaMiB(profileName string, eccOn bool) (uint64, error) {
	profile, found := Profiles[profileName]
	if !found {
		return 0, fmt.Errorf("profile %v not found", profileName)
	}

	if eccOn {
		return profile["lmem_quota_ecc_on"] / (1024 * 1024), nil
	}

	return profile["lmem_quota"] / (1024 * 1024), nil
}

// GetVFDefaults returns default VF memory amount in MiB and profile name for
// a given deviceId.
func GetVFDefaults(deviceId string, eccOn bool) (uint64, string, error) {
	defaultProfileName, found := PerDeviceIdDefaultProfiles[deviceId]
	if !found {
		return 0, "", fmt.Errorf("unsupported device %v", deviceId)
	}

	vfMem := getProfileLmemQuotaMiBNoErr(defaultProfileName, eccOn)

	return vfMem, defaultProfileName, nil
}

// GetMimimumVFMemorySizeMiB returns amount of memory in MiB that the smallest
// profile for deviceId has.
func GetMimimumVFMemorySizeMiB(deviceId string, eccOn bool) (uint64, error) {
	deviceProfiles, found := PerDeviceIdProfiles[deviceId]
	if !found || len(deviceProfiles) == 0 {
		return 0, fmt.Errorf("unsupported device %v", deviceId)
	}

	minimumProfileName := deviceProfiles[len(deviceProfiles)-1]
	return GetProfileLmemQuotaMiB(minimumProfileName, eccOn)
}

// GetMaximumVFMemorySizeMiB returns amount of memory in MiB that the largest
// profile for deviceId has.
func GetMaximumVFMemorySizeMiB(deviceId string, eccOn bool) (uint64, error) {
	deviceProfiles, found := PerDeviceIdProfiles[deviceId]
	if !found || len(deviceProfiles) == 0 {
		return 0, fmt.Errorf("unsupported device %v", deviceId)
	}

	maximumProfileName := deviceProfiles[0]
	return GetProfileLmemQuotaMiB(maximumProfileName, eccOn)
}

// SanitizeLmemQuotaMiB returns true is requested amount of lmemQuota in MiB is
// supported by at least one profile for deviceId, otherwise false.
func SanitizeLmemQuotaMiB(deviceId string, eccOn bool, lmemQuotaMiB uint64) bool {
	deviceProfiles, found := PerDeviceIdProfiles[deviceId]
	if !found || len(deviceProfiles) == 0 {
		klog.V(5).Infof("unsupported device %v", deviceId)
		return false
	}

	minVFMemMiB, _ := GetMimimumVFMemorySizeMiB(deviceId, eccOn)
	maxVFMemMiB, _ := GetMaximumVFMemorySizeMiB(deviceId, eccOn)

	if lmemQuotaMiB > maxVFMemMiB || lmemQuotaMiB < minVFMemMiB {
		klog.V(5).Infof("VF memory value is out of bounds")
		return false
	}

	return true
}

// PickVFProfile selects suitable VF profile based on memory request.
// Returns VF memory in MiB, profile name and error.
// Can be used in case fair share is not suitable.
func PickVFProfile(deviceId string, vfMemoryRequestMiB uint64, eccOn bool) (uint64, string, error) {
	availableProfileNames, found := PerDeviceIdProfiles[deviceId]

	if !found {
		klog.Infof("No VF profiles for device %v, using %v", deviceId, FairShareProfile)
		return 0, FairShareProfile, nil
	}

	lmemRequestBytes := vfMemoryRequestMiB * (1024 * 1024)
	profileName := ""

	// iterate over list of profiles backwards - find the smallest VF that fits
	// request to provision as many VFs as possible
	for profileIdx := len(availableProfileNames) - 1; profileIdx >= 0; profileIdx-- {
		if (eccOn && Profiles[availableProfileNames[profileIdx]]["lmem_quota_ecc_on"] >= lmemRequestBytes) ||
			(!eccOn && Profiles[availableProfileNames[profileIdx]]["lmem_quota"] >= lmemRequestBytes) {
			profileName = availableProfileNames[profileIdx]
			break
		}
	}

	if profileName == "" {
		return 0, "", fmt.Errorf("could not select suitable VF Profile")
	}

	klog.V(5).Infof("Picking profile %v", profileName)
	vfMemMiB := getProfileLmemQuotaMiBNoErr(profileName, eccOn)

	return vfMemMiB, profileName, nil
}

// MaxFairVFs returns the maximum number of VFs that PF resources can be split
// fairly into, for the requested VF combination.
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
		return 0, fmt.Errorf("no VF profiles for device %v", deviceId)
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
		return 0, fmt.Errorf("could not split PF resources fairly")
	}

	return int(profile["numvfs"]), nil
}

// GetMaximumVFDoorbells returns amount of doorbells the biggest profile for
// deviceId has.
func GetMaximumVFDoorbells(deviceId string) uint64 {
	deviceProfiles, found := PerDeviceIdProfiles[deviceId]
	if !found || len(deviceProfiles) == 0 {
		return 0
	}

	maximumProfileName := deviceProfiles[0]
	return Profiles[maximumProfileName]["doorbells_quota"]
}

// GetMinimumVFDoorbells returns amount of doorbells the smallest profile for
// deviceId has.
func GetMimimumVFDoorbells(deviceId string) uint64 {
	deviceProfiles, found := PerDeviceIdProfiles[deviceId]
	if !found || len(deviceProfiles) == 0 {
		return 0
	}

	minimumProfileName := deviceProfiles[len(deviceProfiles)-1]
	return Profiles[minimumProfileName]["doorbells_quota"]
}

// GetProfileDoorbells returns amount of doorbells the profile has.
func GetProfileDoorbells(profileName string) uint64 {
	profile, found := Profiles[profileName]
	if !found {
		return 0
	}

	return profile["doorbells_quota"]
}

// DeviceProfileExists returns true if given profileName is found for deviceId
// in PerDeviceIdProfiles, or false.
func DeviceProfileExists(deviceId string, profileName string) bool {
	if profileName == FairShareProfile {
		return true
	}

	deviceProfiles, found := PerDeviceIdProfiles[deviceId]
	if !found {
		return false
	}

	for _, deviceProfile := range deviceProfiles {
		if profileName == deviceProfile {
			return true
		}
	}

	return false
}
