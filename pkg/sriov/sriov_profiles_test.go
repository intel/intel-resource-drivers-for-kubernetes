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
	"testing"
)

func TestAllProfilesArePresent(t *testing.T) {
	t.Run("All profiles are present with no typos", func(t *testing.T) {
		for deviceId, profileIds := range PerDeviceIdProfiles {
			for _, profileId := range profileIds {
				if _, exists := Profiles[profileId]; !exists {
					t.Errorf("profile %s for device %s does not exist", profileId, deviceId)
				}
			}
		}
	})
}

func TestNoUnusedProfilesArePresent(t *testing.T) {
	t.Run("All present profiles except fairShare are used by some device", func(t *testing.T) {
		for profileId := range Profiles {
			if profileId == "fairShare" {
				continue
			}
			unused := true
			for _, deviceProfiles := range PerDeviceIdProfiles {
				for _, deviceProfile := range deviceProfiles {
					if profileId == deviceProfile {
						unused = false
					}
				}
			}
			if unused {
				t.Errorf("profile %s is unused", profileId)
			}
		}
	})
}

func TestAllDefaultProfilesArePresent(t *testing.T) {
	t.Run("All profiles are present with no typos", func(t *testing.T) {
		for deviceId, profileName := range PerDeviceIdDefaultProfiles {
			if _, exists := Profiles[profileName]; !exists {
				t.Errorf("default profile %s for device %s does not exist", profileName, deviceId)
			}
		}
	})
}
