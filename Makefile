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

ARCH=amd64
PKG = github.com/intel/intel-resource-drivers-for-kubernetes
GO111MODULE = on
GOPATH ?= $(shell go env GOPATH)
GOBIN ?= $(GOPATH)/bin
export GOPATH GOBIN GO111MODULE

GIT_COMMIT = $(shell git rev-parse HEAD)
BUILD_DATE = $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
VERSION ?= v0.1.0-beta

LDFLAGS = -X ${PKG}/pkg/version.driverVersion=${IMAGE_VERSION} -X ${PKG}/pkg/version.gitCommit=${GIT_COMMIT} -X ${PKG}/pkg/version.buildDate=${BUILD_DATE}
EXT_LDFLAGS = -s -w -extldflags "-static"

GOLICENSES_VERSION?=v1.5.0
ifneq ("$(wildcard licenses/)","")
LOCAL_LICENSES=TRUE
endif

# Use a custom version for E2E tests if we are testing in CI
REGISTRY ?= registry.local
IMAGENAME ?= intel-gpu-resource-driver
IMAGE_VERSION ?= $(VERSION)
IMAGE_TAG ?= $(REGISTRY)/$(IMAGENAME):$(IMAGE_VERSION)

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

.PHONY: build all kubelet-plugin controller
kubelet-plugin:
	CGO_ENABLED=0 GOOS=linux GOARCH=${ARCH} \
		go build -a -ldflags "${LDFLAGS} ${EXT_LDFLAGS}" \
		-mod vendor -o bin/kubelet-plugin ./cmd/kubelet-plugin

controller:
	CGO_ENABLED=0 GOOS=linux GOARCH=${ARCH} \
		go build -a -ldflags "${LDFLAGS} ${EXT_LDFLAGS}" \
		-mod vendor -o bin/controller ./cmd/controller

all: controller kubelet-plugin
build: all

.PHONY: container-build
container-build: update-vendor
	$(DOCKER) build --pull --platform="linux/$(ARCH)" -t $(IMAGE_TAG) --build-arg ARCH=$(ARCH) \
	--build-arg LOCAL_LICENSES=$(LOCAL_LICENSES) ./

.PHONY: container-local
container-local: container-build
	$(DOCKER) save -o /tmp/temp_image.tar $(IMAGE_TAG)
	sudo ctr -n k8s.io image import /tmp/temp_image.tar
	rm /tmp/temp_image.tar

.PHONY: container-push
container-push: container-build
	$(DOCKER) push $(IMAGE_TAG)

.PHONY: clean
clean:
	rm -rf vendor/* bin/*

.PHONY: generate
generate:
	go generate $(PKG)/...

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
	save "./cmd/controller" "./cmd/kubelet-plugin" "./pkg/version/" "./pkg/crd/intel/v1alpha" \
	"./pkg/crd/intel/v1alpha/api" "./pkg/crd/intel/clientset/versioned/" --save_path licenses

.PHONY: format
format:
	gofmt -w -s -l ./

.PHONY: lint
lint: format build
	golangci-lint run ./...

.PHONY: vet
vet:
	go vet  $(PKG)/...
