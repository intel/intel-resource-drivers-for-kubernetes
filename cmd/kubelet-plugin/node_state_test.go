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

package main

import (
	"reflect"
	"testing"
)

func TestDeviceInfoDeepCopy(t *testing.T) {
	di := DeviceInfo{
		uid:        "f",
		model:      "ff",
		cardidx:    2,
		renderdidx: 3,
		memoryMiB:  4,
		millicores: 5,
		deviceType: "fff",
		maxvfs:     6,
		parentuid:  "ffff",
		vfprofile:  "fffff",
		vfindex:    7,
		eccOn:      true,
	}

	dc := di.DeepCopy()

	if !reflect.DeepEqual(&di, dc) {
		t.Fatalf("device infos %v and %v do not match", di, dc)
	}
}
