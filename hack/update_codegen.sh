#!/usr/bin/env bash

# Copyright (c) 2022, Intel Corporation.  All Rights Reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -o errexit
set -o nounset
set -o pipefail


# ensure operating from the root of this git tree 
INTEL_DRA_GPU_DRIVER_HACK=$(dirname "$(readlink -f "$0")")
INTEL_DRA_GPU_DRIVER=$(dirname "$INTEL_DRA_GPU_DRIVER_HACK")
cd "$INTEL_DRA_GPU_DRIVER"

API_VERSION=v1alpha
bash vendor/k8s.io/code-generator/generate-groups.sh \
  "all" \
  github.com/intel/intel-resource-drivers-for-kubernetes/pkg/crd/intel \
  github.com/intel/intel-resource-drivers-for-kubernetes/pkg/crd \
  intel:"$API_VERSION" \
  --go-header-file hack/boilerplate.go.txt \
  --output-base "./pkg/crd/"

# wipe old generated code and copy new instead
for modname in clientset informers listers; do
    rm -rf pkg/crd/intel/$modname
    mv pkg/crd/github.com/intel/intel-resource-drivers-for-kubernetes/pkg/crd/intel/$modname pkg/crd/intel/
done
rm -f pkg/crd/intel/"$API_VERSION"/zz_generated.deepcopy.go
mv pkg/crd/github.com/intel/intel-resource-drivers-for-kubernetes/pkg/crd/intel/"$API_VERSION"/zz_generated.deepcopy.go pkg/crd/intel/"$API_VERSION"/

pushd pkg/
for filename in $(grep -ri Parameterses | awk '{print $1}' | sed 's/:.*//'| sort -u); do
    echo "Fixing parameterses in $filename"
    sed -i 's/Parameterses/Parameters/g' $filename;
    sed -i 's/parameterses/parameters/g' $filename;
done;
popd


# cleanup empty dir after moving sole subdir
rm -r pkg/crd/github.com
