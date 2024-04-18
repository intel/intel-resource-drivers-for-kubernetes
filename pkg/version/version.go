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

package version

import (
	"runtime"

	"k8s.io/klog/v2"
)

// These are set during build time via -ldflags.
var (
	driverVersion = "N/A"
	gitCommit     = "N/A"
	buildDate     = "N/A"
)

// GetVersion returns the version information of the driver.
func PrintDriverVersion(apiGroupName string, apiGroupVersion string) {
	klog.Infof(`
Driver Name:        %v,
Driver Version:     %v,
Driver API Version: %v,
Git Commit:         %v,
Build Date:         %v,
Go Version:         %v,
Compiler:           %v,
Platform:           %v/%v`,
		apiGroupName,
		driverVersion,
		apiGroupVersion,
		gitCommit,
		buildDate,
		runtime.Version(),
		runtime.Compiler,
		runtime.GOOS,
		runtime.GOARCH,
	)
}
