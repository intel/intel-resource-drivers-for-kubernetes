#
# Copyright (C) 2024-2026 Intel Corporation
#
# SPDX-License-Identifier: Apache-2.0
#

# Use a custom version for E2E tests if we are testing in CI
GPU_VERSION ?= v0.11.0
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

GPU_LDFLAGS = ${LDFLAGS} -extldflags $(EXT_LDFLAGS) -X ${PKG}/pkg/version.version=${GPU_VERSION}

.PHONY: gpu
gpu: $(GPU_BINARIES)

bin/kubelet-gpu-plugin: cmd/kubelet-gpu-plugin/*.go $(GPU_COMMON_SRC)
	CGO_ENABLED=0 GOOS=linux GOARCH=${ARCH} \
	  go build -a -ldflags "${GPU_LDFLAGS}" -mod vendor -o $@ ./cmd/kubelet-gpu-plugin

.PHONY: gpu-container-build
gpu-container-build: cleanall vendor
	@echo "Building GPU resource drivers container..."
	$(DOCKER) build --pull --platform="linux/$(ARCH)" \
	-t $(GPU_IMAGE_TAG) \
	--build-arg LOCAL_LICENSES=$(LOCAL_LICENSES) \
	--build-arg http_proxy=$(http_proxy) \
	--build-arg https_proxy=$(https_proxy) \
	--build-arg no_proxy=$(no_proxy) \
	-f Dockerfile.gpu .

.PHONY: gpu-container-push
gpu-container-push: gpu-container-build
	$(DOCKER) push $(GPU_IMAGE_TAG)

.PHONY: e2e-gpu
e2e-gpu:
	PLUGINS_REPO_DIR=$(CURDIR) GPU_E2E_USE_DEVICE_FAKER=$(GPU_E2E_USE_DEVICE_FAKER) \
	go test -v ./test/e2e/... --clean-start=true --kubeconfig="$${KUBECONFIG:-$$HOME/.kube/config}" -ginkgo.v -ginkgo.trace -ginkgo.show-node-events -ginkgo.focus='GPU DRA Driver'
