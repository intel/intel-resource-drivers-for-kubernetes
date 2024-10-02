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


GAUDI_VERSION ?= v0.2.0
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

GAUDI_LDFLAGS = ${LDFLAGS} -X ${PKG}/pkg/version.driverVersion=${GAUDI_VERSION}

.PHONY: gaudi
gaudi: $(GAUDI_BINARIES)

bin/kubelet-gaudi-plugin: cmd/kubelet-gaudi-plugin/*.go $(GAUDI_COMMON_SRC)
	CGO_ENABLED=0 GOOS=linux GOARCH=${ARCH} \
	  go build -a -ldflags "${GAUDI_LDFLAGS}" -mod vendor -o $@ ./cmd/kubelet-gaudi-plugin

.PHONY: gaudi-container-build
gaudi-container-build: cleanall vendor
	@echo "Building Gaudi resource driver container..."
	$(DOCKER) build --pull --platform="linux/$(ARCH)" -t $(GAUDI_IMAGE_TAG) \
	--build-arg LOCAL_LICENSES=$(LOCAL_LICENSES) -f Dockerfile.gaudi .

.PHONY: gaudi-container-push
gaudi-container-push: gaudi-container-build
	$(DOCKER) push $(GAUDI_IMAGE_TAG)
