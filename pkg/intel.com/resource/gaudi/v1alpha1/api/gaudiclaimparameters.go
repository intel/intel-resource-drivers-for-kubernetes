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

package api

import (
	icrd "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gaudi/v1alpha1"
)

// Spec field of GaudiClaimParameters.
type GaudiClaimParametersSpec = icrd.GaudiClaimParametersSpec

// Main GaudiClaimParameters object for storing resource request specification.
type GaudiClaimParameters = icrd.GaudiClaimParameters

// List of GaudiClaimParameters.
type GaudiClaimParametersList = icrd.GaudiClaimParametersList

// DefaultGaudiClaimParametersSpec returns new object with hardcoded default values.
func DefaultGaudiClaimParametersSpec() *GaudiClaimParametersSpec {
	return &GaudiClaimParametersSpec{
		Count: 1,
	}
}
