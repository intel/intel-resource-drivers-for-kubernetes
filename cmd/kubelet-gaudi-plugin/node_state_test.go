//
// Copyright (C) 2022-2025 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0
//

package main

import (
	"reflect"
	"testing"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gaudi/device"
)

func TestDeviceInfoDeepCopy(t *testing.T) {
	di := device.DeviceInfo{
		UID:   "f",
		Model: "ff",
	}

	dc := di.DeepCopy()

	if !reflect.DeepEqual(&di, dc) {
		t.Fatalf("device infos %v and %v do not match", di, dc)
	}
}
