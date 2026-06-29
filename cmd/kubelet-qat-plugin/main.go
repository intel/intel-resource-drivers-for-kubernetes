//
// Copyright (C) 2024-2025 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0
//

package main

import (
	"fmt"
	"os"

	"github.com/urfave/cli/v2"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/helpers"
	qat "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/qat/device"
)

func main() {
	if err := helpers.NewApp(qat.DriverName, newDriver, []cli.Flag{}, nil).Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
