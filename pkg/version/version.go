// Copyright (c) 2022, Intel Corporation.  All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package version

import (
	"runtime"

	intelcrd "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/crd/intel/v1alpha/api"
	"k8s.io/klog/v2"
)

// These are set during build time via -ldflags
var (
	driverVersion = "N/A"
	gitCommit     = "N/A"
	buildDate     = "N/A"
)

// GetVersion returns the version information of the driver
func PrintDriverVersion() {
	klog.Infof(`
DriverName:    %v,
DriverVersion: %v,
GitCommit:     %v,
BuildDate:     %v,
GoVersion:     %v,
Compiler:      %v,
Platform:      %v/%v`,
		intelcrd.ApiGroupName,
		driverVersion,
		gitCommit,
		buildDate,
		runtime.Version(),
		runtime.Compiler,
		runtime.GOOS,
		runtime.GOARCH,
	)
}
