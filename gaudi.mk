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


GAUDI_VERSION ?= v0.1.0
GAUDI_IMAGE_NAME ?= intel-gaudi-resource-driver
GAUDI_IMAGE_VERSION ?= $(GAUDI_VERSION)
GAUDI_IMAGE_TAG ?= $(REGISTRY)/$(GAUDI_IMAGE_NAME):$(GAUDI_IMAGE_VERSION)

GAUDI_BINARIES = \
bin/gaudi-controller \
bin/kubelet-gaudi-plugin

GAUDI_COMMON_SRC = \
$(COMMON_SRC) \
pkg/intel.com/resource/gaudi/clientset/versioned/*.go \
pkg/intel.com/resource/gaudi/v1alpha1/api/*.go \
pkg/intel.com/resource/gaudi/v1alpha1/*.go \
pkg/gaudi/cdihelpers/*.go \
pkg/gaudi/device/*.go \
pkg/gaudi/discovery/*.go

GAUDI_LDFLAGS = ${LDFLAGS} -X ${PKG}/pkg/version.driverVersion=${GPU_VERSION}

.PHONY: gaudi
gaudi: $(GAUDI_BINARIES)

bin/gaudi-controller: cmd/gaudi-controller/*.go $(GAUDI_COMMON_SRC)
	CGO_ENABLED=0 GOOS=linux GOARCH=${ARCH} \
	  go build -a -ldflags "${GAUDI_LDFLAGS}" -mod vendor -o $@ ./cmd/gaudi-controller

bin/kubelet-gaudi-plugin: cmd/kubelet-gaudi-plugin/*.go $(GAUDI_COMMON_SRC)
	CGO_ENABLED=0 GOOS=linux GOARCH=${ARCH} \
	  go build -a -ldflags "${GAUDI_LDFLAGS}" -mod vendor -o $@ ./cmd/kubelet-gpu-plugin

.PHONY: gaudi-container-build
gaudi-container-build: cleanall vendor
	@echo "Building Gaudi resource driver container..."
	$(DOCKER) build --pull --platform="linux/$(ARCH)" -t $(GAUDI_IMAGE_TAG) \
	--build-arg LOCAL_LICENSES=$(LOCAL_LICENSES) -f Dockerfile.gaudi .

.PHONY: gaudi-container-push
gaudi-container-push: gaudi-container-build
	$(DOCKER) push $(GAUDI_IMAGE_TAG)

.PHONY: rm-gaudi-clientset
rm-gaudi-clientset:
	rm -rf  "$(CURDIR)/pkg/intel.com/resource/gaudi/clientset/"

.PHONY: generate-gaudi-clientset
generate-gaudi-clientset: rm-gaudi-clientset
	client-gen \
		--go-header-file=$(CURDIR)/hack/boilerplate.go.txt \
		--clientset-name "versioned" \
		--output-pkg "$(MODULE)/pkg/intel.com/resource/gaudi/clientset" \
		--input-base "$(MODULE)/pkg/intel.com/resource" \
		--output-dir "$(CURDIR)/pkg/tmp_clientset" \
		--input "gaudi/v1alpha1" \
		--plural-exceptions "GaudiClassParameters:GaudiClassParameters,GaudiClaimParameters:GaudiClaimParameters"
	mkdir -p $(CURDIR)/pkg/intel.com/resource/gaudi/clientset
	mv $(CURDIR)/pkg/tmp_clientset/versioned $(CURDIR)/pkg/intel.com/resource/gaudi/clientset/
	rm -rf $(CURDIR)/pkg/tmp_clients

.PHONY: generate-gaudi-crd
generate-gaudi-crd: generate-gaudi-deepcopy
	controller-gen \
		crd:crdVersions=v1 \
		paths=$(CURDIR)/pkg/intel.com/resource/gaudi/v1alpha1/ \
		output:crd:dir=$(CURDIR)/deployments/gaudi/static/crds


.PHONY: generate-gaudi-deepcopy
generate-gaudi-deepcopy: generate-gaudi-clientset
	controller-gen \
		object:headerFile=$(CURDIR)/hack/boilerplate.go.txt,year=$(shell date +"%Y") \
		paths=$(CURDIR)/pkg/intel.com/resource/gaudi/v1alpha1/ \
		output:object:dir=$(CURDIR)/pkg/intel.com/resource/gaudi/v1alpha1
