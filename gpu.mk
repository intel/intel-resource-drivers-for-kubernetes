# Copyright (c) 2024, Intel Corporation.  All Rights Reserved.
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

# Use a custom version for E2E tests if we are testing in CI
GPU_VERSION ?= v0.8.0-rc1
GPU_IMAGE_NAME ?= intel-gpu-resource-driver
GPU_IMAGE_VERSION ?= $(GPU_VERSION)
GPU_IMAGE_TAG ?= $(REGISTRY)/$(GPU_IMAGE_NAME):$(GPU_IMAGE_VERSION)

GPU_BINARIES = \
bin/kubelet-gpu-plugin

GPU_COMMON_SRC = \
$(COMMON_SRC) \
pkg/gpu/cdihelpers/*.go \
pkg/gpu/device/*.go \
pkg/gpu/discovery/*.go

GPU_LDFLAGS = ${LDFLAGS} -extldflags $(EXT_LDFLAGS) -X ${PKG}/pkg/version.driverVersion=${GPU_VERSION}

.PHONY: gpu
gpu: $(GPU_BINARIES)

bin/kubelet-gpu-plugin: cmd/kubelet-gpu-plugin/*.go $(GPU_COMMON_SRC)
	CGO_ENABLED=0 GOOS=linux GOARCH=${ARCH} \
	  go build -a -ldflags "${GPU_LDFLAGS}" -mod vendor -o $@ ./cmd/kubelet-gpu-plugin

bin/alert-webhook: cmd/alert-webhook/*.go $(GPU_COMMON_SRC)
	CGO_ENABLED=0 GOOS=linux GOARCH=${ARCH} \
	  go build -a -ldflags "${GPU_LDFLAGS}" -mod vendor -o $@ ./cmd/alert-webhook

.PHONY: gpu-container-build
gpu-container-build: cleanall vendor
	@echo "Building GPU resource drivers container..."
	$(DOCKER) build --pull --platform="linux/$(ARCH)" -t $(GPU_IMAGE_TAG) \
	--build-arg LOCAL_LICENSES=$(LOCAL_LICENSES) -f Dockerfile.gpu .

.PHONY: gpu-container-push
gpu-container-push: gpu-container-build
	$(DOCKER) push $(GPU_IMAGE_TAG)
