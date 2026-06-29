//
// Copyright (C) 2025 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0
//

package helpers

import "context"

type Driver interface {
	Shutdown(ctx context.Context) error
}
