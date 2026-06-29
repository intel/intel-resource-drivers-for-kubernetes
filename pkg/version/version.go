//
// Copyright (C) 2022-2025 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0
//

package version

import (
	"runtime"

	"k8s.io/klog/v2"
)

// These are set during build time via -ldflags.
var (
	version   = "N/A"
	gitCommit = "N/A"
	buildDate = "N/A"
)

// GetVersion returns the version information of the driver.
func PrintDriverVersion(apiGroupName string) {
	klog.Infof(`
Driver Name:        %v,
Driver Version:     %v,
Git Commit:         %v,
Build Date:         %v,
Go Version:         %v,
Compiler:           %v,
Platform:           %v/%v`,
		apiGroupName,
		version,
		gitCommit,
		buildDate,
		runtime.Version(),
		runtime.Compiler,
		runtime.GOOS,
		runtime.GOARCH,
	)
}

func GetVersion() string {
	return version
}

func GetGitCommit() string {
	return gitCommit
}

func GetBuildDate() string {
	return buildDate
}
