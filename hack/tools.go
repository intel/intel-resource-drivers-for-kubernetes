//
// Copyright (C) 2022 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0
//

//go:build tools
// +build tools

// This package imports things required by build scripts, to force `go mod` to see them as dependencies
package tools

import _ "k8s.io/code-generator"
