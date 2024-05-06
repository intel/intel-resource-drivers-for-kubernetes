# Copyright 2017 The Kubernetes Authors.
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

.PHONY: list-targets
list-targets:
	@echo -e "\nTargets:\n$$(grep '^[-a-zA-Z/]*:' Makefile | sort | sed -e 's/^/- /' -e 's/:$$//')\n"

ARCH=amd64
PKG = github.com/intel/intel-resource-drivers-for-kubernetes
GO111MODULE = on
GOPATH ?= $(shell go env GOPATH)
GOBIN ?= $(GOPATH)/bin
export GOPATH GOBIN GO111MODULE

GIT_COMMIT = $(shell git rev-parse HEAD)
BUILD_DATE = $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
GIT_BRANCH ?= $(shell git branch --show-current)

EXT_LDFLAGS = -static
LDFLAGS = \
 -s -w -extldflags $(EXT_LDFLAGS) \
 -X ${PKG}/pkg/version.gitCommit=${GIT_COMMIT} \
 -X ${PKG}/pkg/version.buildDate=${BUILD_DATE}


GOLICENSES_VERSION?=v1.6.0
ifneq ("$(wildcard licenses/)","")
LOCAL_LICENSES=TRUE
endif

MODULE = github.com/intel/intel-resource-drivers-for-kubernetes

REGISTRY ?= registry.local

# Use a custom version for E2E tests if we are testing in CI
GPU_VERSION ?= v0.4.0
GPU_IMAGE_NAME ?= intel-gpu-resource-driver
GPU_IMAGE_VERSION ?= $(GPU_VERSION)
GPU_IMAGE_TAG ?= $(REGISTRY)/$(GPU_IMAGE_NAME):$(GPU_IMAGE_VERSION)

GAUDI_VERSION ?= v0.1.0
GAUDI_IMAGE_NAME ?= intel-gaudi-resource-driver
GAUDI_IMAGE_VERSION ?= $(GAUDI_VERSION)
GAUDI_IMAGE_TAG ?= $(REGISTRY)/$(GAUDI_IMAGE_NAME):$(GAUDI_IMAGE_VERSION)

ifndef DOCKER
	PODMAN_VERSION := $(shell command podman version 2>/dev/null)
	DOCKER_VERSION := $(shell command docker version 2>/dev/null)
    ifdef DOCKER_VERSION
        DOCKER := docker
	else ifdef PODMAN_VERSION
		DOCKER := podman
	endif
endif

.EXPORT_ALL_VARIABLES:

GPU_BINARIES = \
bin/gpu-controller \
bin/kubelet-gpu-plugin \
bin/gas-status-updater \
bin/alert-webhook

GAUDI_BINARIES = \
bin/gaudi-controller \
bin/kubelet-gaudi-plugin

COMMON_SRC = \
pkg/version/*.go \
pkg/controllerhelpers/*.go

GPU_COMMON_SRC = \
$(COMMON_SRC) \
pkg/intel.com/resource/gpu/clientset/versioned/*.go \
pkg/intel.com/resource/gpu/v1alpha2/api/*.go \
pkg/intel.com/resource/gpu/v1alpha2/*.go \
pkg/sriov/*.go


GAUDI_COMMON_SRC = \
$(COMMON_SRC) \
pkg/intel.com/resource/gaudi/clientset/versioned/*.go \
pkg/intel.com/resource/gaudi/v1alpha1/api/*.go \
pkg/intel.com/resource/gaudi/v1alpha1/*.go \
pkg/gaudi/cdihelpers/*.go \
pkg/gaudi/device/*.go \
pkg/gaudi/discovery/*.go

.PHONY: build gpu gaudi
build: gpu gaudi

gpu: $(GPU_BINARIES)

gaudi: $(GAUDI_BINARIES)

bin/kubelet-gpu-plugin: cmd/kubelet-gpu-plugin/*.go $(GPU_COMMON_SRC)
	CGO_ENABLED=0 GOOS=linux GOARCH=${ARCH} \
	  go build -a -ldflags "${LDFLAGS}" -mod vendor -o $@ ./cmd/kubelet-gpu-plugin

bin/gpu-controller: cmd/gpu-controller/*.go $(GPU_COMMON_SRC)
	CGO_ENABLED=0 GOOS=linux GOARCH=${ARCH} \
	  go build -a -ldflags "${LDFLAGS}" -mod vendor -o $@ ./cmd/gpu-controller

bin/gas-status-updater: cmd/gas-status-updater/*.go $(GPU_COMMON_SRC)
	CGO_ENABLED=0 GOOS=linux GOARCH=${ARCH} \
	  go build -a -ldflags "${LDFLAGS}" -mod vendor -o $@ ./cmd/gas-status-updater

bin/alert-webhook: cmd/alert-webhook/*.go $(GPU_COMMON_SRC)
	CGO_ENABLED=0 GOOS=linux GOARCH=${ARCH} \
	  go build -a -ldflags "${LDFLAGS}" -mod vendor -o $@ ./cmd/alert-webhook

bin/gaudi-controller: cmd/gaudi-controller/*.go $(GAUDI_COMMON_SRC)
	CGO_ENABLED=0 GOOS=linux GOARCH=${ARCH} \
	  go build -a -ldflags "${LDFLAGS}" -mod vendor -o $@ ./cmd/gaudi-controller

bin/kubelet-gaudi-plugin: cmd/kubelet-gaudi-plugin/*.go $(GAUDI_COMMON_SRC)
	CGO_ENABLED=0 GOOS=linux GOARCH=${ARCH} \
	  go build -a -ldflags "${LDFLAGS}" -mod vendor -o $@ ./cmd/kubelet-gpu-plugin

.PHONY: branch-build
# test that all commits in $GIT_BRANCH (default=current) build
branch-build:
	current=$$(git branch --show-current); echo "Current branch: $$current"; \
	for commit in $$(git log --reverse --pretty=oneline origin/master...$(GIT_BRANCH) | cut -d' ' -f1); do \
		echo "Building: '$$commit'..."; git checkout $$commit && make build; done; \
	git checkout $$current

.PHONY: containers-build gpu-container-build gaudi-container-build
containers-build: gpu-container-build gaudi-container-build

gpu-container-build: vendor
	@echo "Building GPU resource drivers container..."
	$(DOCKER) build --pull --platform="linux/$(ARCH)" -t $(GPU_IMAGE_TAG) \
	--build-arg LOCAL_LICENSES=$(LOCAL_LICENSES) -f Dockerfile.gpu .

gaudi-container-build: vendor
	@echo "Building Gaudi resource driver container..."
	$(DOCKER) build --pull --platform="linux/$(ARCH)" -t $(GAUDI_IMAGE_TAG) \
	--build-arg LOCAL_LICENSES=$(LOCAL_LICENSES) -f Dockerfile.gaudi .

.PHONY: container-local
container-local: container-build
	$(DOCKER) save -o /tmp/temp_image.tar $(GPU_IMAGE_TAG)
	sudo ctr -n k8s.io image import /tmp/temp_image.tar
	$(DOCKER) save -o /tmp/temp_image.tar $(GAUDI_IMAGE_TAG)
	sudo ctr -n k8s.io image import /tmp/temp_image.tar
	rm /tmp/temp_image.tar

.PHONY: containers-push gpu-container-push gaudi-container-push
containers-push: containers-build gpu-container-push gaudi-container-push

gpu-container-push: gpu-container-build
	$(DOCKER) push $(GPU_IMAGE_TAG)

gaudi-container-push: gaudi-container-build
	$(DOCKER) push $(GAUDI_IMAGE_TAG)

.PHONY: clean cleanall
clean:
	rm -rf $(GPU_BINARIES) $(GAUD_BINARIES)
cleanall: clean
	rm -rf vendor/* bin/*

.PHONY: rm-clientsets rm-gpu-clientset rm-gaudi-clientset
rm-clientsets: rm-gpu-clientset rm-gaudi-clientset

rm-gpu-clientset:
	rm -rf "$(CURDIR)/pkg/intel.com/resource/gpu/clientset/"

rm-gaudi-clientset:
	rm -rf  "$(CURDIR)/pkg/intel.com/resource/gaudi/clientset/"

.PHONY: generate
generate: generate-crds

.PHONY: generate-crds
generate-crds: generate-deepcopy
	controller-gen \
		crd:crdVersions=v1 \
		paths=$(CURDIR)/pkg/intel.com/resource/gpu/v1alpha2/ \
		output:crd:dir=$(CURDIR)/deployments/gpu/static/crds
	controller-gen \
		crd:crdVersions=v1 \
		paths=$(CURDIR)/pkg/intel.com/resource/gaudi/v1alpha1/ \
		output:crd:dir=$(CURDIR)/deployments/gaudi/static/crds

.PHONY: generate-deepcopy
generate-deepcopy: generate-clientsets
	controller-gen \
		object:headerFile=$(CURDIR)/hack/boilerplate.go.txt,year=$(shell date +"%Y") \
		paths=$(CURDIR)/pkg/intel.com/resource/gpu/v1alpha2/ \
		output:object:dir=$(CURDIR)/pkg/intel.com/resource/gpu/v1alpha2
	controller-gen \
		object:headerFile=$(CURDIR)/hack/boilerplate.go.txt,year=$(shell date +"%Y") \
		paths=$(CURDIR)/pkg/intel.com/resource/gaudi/v1alpha1/ \
		output:object:dir=$(CURDIR)/pkg/intel.com/resource/gaudi/v1alpha1

.PHONY: generate-clientsets generate-gpu-clientset generate-gaudi-clientset
generate-clientsets: generate-gpu-clientset generate-gaudi-clientset

generate-gpu-clientset: rm-gpu-clientset
	client-gen \
		--go-header-file=$(CURDIR)/hack/boilerplate.go.txt \
		--clientset-name "versioned" \
		--build-tag "ignore_autogenerated" \
		--output-package "$(MODULE)/pkg/intel.com/resource/gpu/clientset" \
		--input-base "$(MODULE)/pkg/intel.com/resource" \
		--output-base "$(CURDIR)/pkg/tmp_clientset" \
		--input "gpu/v1alpha2" \
		--plural-exceptions "GpuClassParameters:GpuClassParameters,GpuClaimParameters:GpuClaimParameters"
	mkdir -p $(CURDIR)/pkg/intel.com/resource/
	mv $(CURDIR)/pkg/tmp_clientset/$(MODULE)/pkg/intel.com/resource/gpu/clientset \
		$(CURDIR)/pkg/intel.com/resource/gpu/
	rm -rf $(CURDIR)/pkg/tmp_clientset

generate-gaudi-clientset: rm-gaudi-clientset
	client-gen \
		--go-header-file=$(CURDIR)/hack/boilerplate.go.txt \
		--clientset-name "versioned" \
		--build-tag "ignore_autogenerated" \
		--output-package "$(MODULE)/pkg/intel.com/resource/gaudi/clientset" \
		--input-base "$(MODULE)/pkg/intel.com/resource" \
		--output-base "$(CURDIR)/pkg/tmp_clientset" \
		--input "gaudi/v1alpha1" \
		--plural-exceptions "GaudiClassParameters:GaudiClassParameters,GaudiClaimParameters:GaudiClaimParameters"
	mkdir -p $(CURDIR)/pkg/intel.com/resource/
	mv $(CURDIR)/pkg/tmp_clientset/$(MODULE)/pkg/intel.com/resource/gaudi/clientset \
		$(CURDIR)/pkg/intel.com/resource/gaudi/
	rm -rf $(CURDIR)/pkg/tmp_clients

.PHONY: vendor
vendor:
	go mod vendor

.PHONY: update-vendor
update-vendor:
	go mod tidy
	go mod vendor

.PHONY: clean-licenses
clean-licenses:
	rm -rf licenses

.PHONY: licenses
licenses: clean-licenses
	GO111MODULE=on go run github.com/google/go-licenses@$(GOLICENSES_VERSION) \
	save "./cmd/gpu-controller" "./cmd/kubelet-gpu-plugin" "./cmd/gaudi-controller" "./cmd/kubelet-gaudi-plugin" \
	"./pkg/controllerhelpers" \
	"./pkg/version" \
	"./pkg/gpu/cdihelpers" \
	"./pkg/gpu/device" \
	"./pkg/gpu/discovery" \
	"./pkg/gpu/sriov" \
	"./pkg/gaudi/cdihelpers" \
	"./pkg/gaudi/device" \
	"./pkg/gaudi/discovery" \
	"./pkg/intel.com/resource/gpu/v1alpha2" \
	"./pkg/intel.com/resource/gpu/v1alpha2/api" \
	"./pkg/intel.com/resource/gpu/clientset/versioned/" --save_path licenses \
	"./pkg/intel.com/resource/gaudi/v1alpha1" \
	"./pkg/intel.com/resource/gaudi/v1alpha1/api" \
	"./pkg/intel.com/resource/gaudi/clientset/versioned/" --save_path licenses


# linting targets for Go and other code
.PHONY: lint format cilint vet shellcheck yamllint

lint: format cilint vet klogformat shellcheck yamllint

format:
	gofmt -w -s -l ./

cilint:
	golangci-lint --max-same-issues 0 --max-issues-per-linter 0 run ./...

vet:
	go vet  $(PKG)/...

# cilint does not check klog formats. Until it does, make at least sure
# that when format parameters are given, some format function is used
# (assumes that there are no non-format percent char usage) and vice verse.
klogformat:
	@echo -e "\ntesting/klog: format calls without format args, or vice verse:"
	! git grep -n -e '\bt\..*f("[^%]*")' -e 'klog\..*f("[^%]*")' -e 'klog\..*[^f]("[^)]*%'

# exclude env.sh + SC1091, shellcheck external file handling is broken
shellcheck:
	@echo -e "\nshellcheck: validate our own shell code:"
	find . -name '*.sh' | grep -v -e vendor/ -e /env.sh | xargs shellcheck -e SC1091

# Exclude Helm template files which contain Helm templating syntax
yamllint:
	@echo -e "\nyamllint: lint non-templated YAML files:"
	git ls-files '*.yaml' | xargs grep -L '^ *{{-' | xargs yamllint -d relaxed --no-warnings


.PHONY: test coverage
COVERAGE_FILE := coverage.out
test:
	go test -v -coverprofile=$(COVERAGE_FILE) $(MODULE)/...

coverage: test
	go tool cover -html=$(COVERAGE_FILE) -o coverage.html
