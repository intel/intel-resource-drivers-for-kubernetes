#!/usr/bin/env bash
# Copyright (C) 2025-2026 Intel Corporation
#
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

RESULT=$(make "$1" | awk '/total:/ {print ($3+0)}')

if (( $(echo "$RESULT >= $2" | bc -l) )); then
    echo "$1 $RESULT% is above or equal to the threshold $2%"
    exit 0
else
    echo "$1 $RESULT% is below threshold $2%. Add more tests!"
    exit 1
fi
