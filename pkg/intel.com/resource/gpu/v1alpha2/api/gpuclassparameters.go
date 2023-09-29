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

package api

import (
	icrd "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gpu/v1alpha2"
)

// DeviceSelector allows one to match on a specific type of Device as part of the class.
type DeviceSelector = icrd.DeviceSelector

// Spec field of GpuClassParameters.
type GpuClassParametersSpec = icrd.GpuClassParametersSpec

// Main GpuClassParameters object for storing resource request specification.
type GpuClassParameters = icrd.GpuClassParameters

// List of GpuClassParameters.
type GpuClassParametersList = icrd.GpuClassParametersList

// DefaultGpuClassParametersSpec returns new object with hardcoded default values.
func DefaultGpuClassParametersSpec() *GpuClassParametersSpec {
	return &GpuClassParametersSpec{
		Shared: false,
		DeviceSelector: []DeviceSelector{
			{
				Type: GpuDeviceType,
				Name: "*",
			},
		},
	}
}
