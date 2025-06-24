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


QAT_VERSION ?= v0.3.0
QAT_IMAGE_NAME ?= intel-qat-resource-driver
QAT_IMAGE_VERSION ?= $(QAT_VERSION)
QAT_IMAGE_TAG ?= $(REGISTRY)/$(QAT_IMAGE_NAME):$(QAT_IMAGE_VERSION)

QAT_BINARIES = \
bin/qat-showdevice \
bin/kubelet-qat-plugin

QAT_COMMON_SRC = \
$(COMMON_SRC) \
pkg/qat/device/*.go \
pkg/qat/cdi/*.go

QAT_LDFLAGS = ${LDFLAGS} -extldflags $(EXT_LDFLAGS) -X ${PKG}/pkg/version.driverVersion=${QAT_VERSION}

.PHONY: qat
qat: $(QAT_BINARIES)

bin/qat-showdevice: cmd/qat-showdevice/*.go $(QAT_COMMON_SRC)
	CGO_ENABLED=0 GOOS=linux GOARCH=${ARCH} \
	  go build -a -ldflags "${QAT_LDFLAGS}" -mod vendor -o $@ ./cmd/qat-showdevice

bin/kubelet-qat-plugin: cmd/kubelet-qat-plugin/*.go $(QAT_COMMON_SRC)
	CGO_ENABLED=0 GOOS=linux GOARCH=${ARCH} \
	  go build -a -ldflags "${QAT_LDFLAGS}" -mod vendor -o $@ ./cmd/kubelet-qat-plugin

.PHONY: qat-container-build
qat-container-build: cleanall vendor
	@echo "Building QAT resource driver container..."
	$(DOCKER) build --pull --platform="linux/$(ARCH)" -t $(QAT_IMAGE_TAG) \
	--build-arg LOCAL_LICENSES=$(LOCAL_LICENSES) -f Dockerfile.qat .

.PHONY: qat-container-push
qat-container-push: qat-container-build
	$(DOCKER) push $(QAT_IMAGE_TAG)

.PHONY: e2e-qat
e2e-qat:
	sed -i 's|\(intel/intel-qat-resource-driver:\)[^ ]*|\1devel|' deployments/qat/base/resource-driver.yaml
	go test -v ./test/e2e/... --clean-start=true -ginkgo.v -ginkgo.trace -ginkgo.show-node-events
