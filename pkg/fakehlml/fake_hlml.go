/*
 * Copyright (c) 2024, Intel Corporation. All Rights Reserved.
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

package fakehlml

/*
#cgo LDFLAGS: "/usr/lib/habanalabs/libhlml.so" -ldl -Wl,--unresolved-symbols=ignore-all
#include "fake_hlml.h"
#include <stdlib.h>
*/
import "C"

import (
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gaudi/device"
)

// KEEP THIS IDENTICAL TO fake_hlml.h call_identity_t
const (
	FakeInit uint32 = iota
	FakeInitWithFlags
	FakeShutdown
	FakeDeviceGetCount
	FakeDeviceGetHandleByPCIBusID
	FakeDeviceGetHandleByIndex
	FakeDeviceGetHandleByUUID
	FakeDeviceGetName
	FakeDeviceGetPCIInfo
	FakeDeviceGetSerial
	FakeDeviceRegisterEvents
	FakeEventSetCreate
	FakeEventSetFree
	FakeEventSetWait
)

// KEEP THIS IDENTICAL TO hlml.h hlml_return_t
const (
	HLMLSuccess                 = 0
	HLMLErrorUninitialized      = 1
	HLMLErrorInvalidArgument    = 2
	HLMLErrorNotSupported       = 3
	HLMLErrorAlreadyInitialized = 5
	HLMLErrorNotFound           = 6
	HLMLErrorInsufficientSize   = 7
	HLMLErrorDriverNotLoaded    = 9
	HLMLErrorTimeout            = 10
	HLMLErrorAipIsLost          = 15
	HLMLErrorMemory             = 20
	HLMLErrorNoData             = 21
	HLMLErrorUnknown            = 49
)

func AddDevices(devicesInfo device.DevicesInfo) {
	for _, deviceInfo := range devicesInfo {
		C.add_device(
			C.CString(deviceInfo.PCIAddress),
			C.CString(deviceInfo.Model),
			C.CString("0x0"), // vendor
			C.CString(deviceInfo.Serial),
			C.uint(deviceInfo.DeviceIdx),
		)
	}
}

func Reset() {
	C.reset()
}

func SetReturnCode(callId uint32, returnCode uint32) {
	C.set_error(C.call_identity_t(callId), C.hlml_return_t(returnCode))
}

func AddCriticalEvent(serial string) {
	C.add_critical_event(C.CString(serial))
}

func ResetRvents() {
	C.reset_events()
}
