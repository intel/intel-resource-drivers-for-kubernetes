#
# Copyright (C) 2024-2026 Intel Corporation
#
# SPDX-License-Identifier: Apache-2.0
#

GAUDI_VERSION ?= v0.7.2
GAUDI_IMAGE_NAME ?= intel-gaudi-resource-driver
GAUDI_IMAGE_VERSION ?= $(GAUDI_VERSION)
GAUDI_IMAGE_TAG ?= $(REGISTRY)/$(GAUDI_IMAGE_NAME):$(GAUDI_IMAGE_VERSION)

GAUDI_BINARIES = \
bin/kubelet-gaudi-plugin

GAUDI_COMMON_SRC = \
$(COMMON_SRC) \
pkg/gaudi/cdihelpers/*.go \
pkg/gaudi/device/*.go \
pkg/gaudi/discovery/*.go

# Gaudi DRA driver is not statically built, it depends on libhlml.so, therefore
# the -extldflags ${EXT_LDFLAGS} is not used.
GAUDI_LDFLAGS = ${LDFLAGS} -X ${PKG}/pkg/version.version=${GAUDI_VERSION}

.PHONY: gaudi
gaudi: $(GAUDI_BINARIES)

bin/kubelet-gaudi-plugin: cmd/kubelet-gaudi-plugin/*.go $(GAUDI_COMMON_SRC)
	cd $(CURDIR)/cmd/kubelet-gaudi-plugin && CGO_ENABLED=1 GOOS=linux GOARCH=${ARCH} \
	  go build -a -ldflags "${GAUDI_LDFLAGS}" -mod vendor -o $(CURDIR)/$@


.PHONY: gaudi-container-build
gaudi-container-build: cleanall vendor
	@echo "Building Gaudi resource driver container..."
	$(DOCKER) build --pull --platform="linux/$(ARCH)" -t $(GAUDI_IMAGE_TAG) \
	--build-arg LOCAL_LICENSES=$(LOCAL_LICENSES) \
	--build-arg http_proxy=$(http_proxy) \
	--build-arg https_proxy=$(https_proxy) \
	--build-arg no_proxy=$(no_proxy) \
	-f Dockerfile.gaudi .

.PHONY: gaudi-container-push
gaudi-container-push: gaudi-container-build
	$(DOCKER) push $(GAUDI_IMAGE_TAG)
