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

.PHONY: list-targets
list-targets:
	@echo "\nTargets:\n$$(grep -h '^[-a-zA-Z/]*:' Makefile *.mk | sort | sed -e 's/^/- /' -e 's/:.*//')\n"

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


ifndef DOCKER
	PODMAN_VERSION := $(shell command podman version 2>/dev/null)
	DOCKER_VERSION := $(shell command docker version 2>/dev/null)
    ifdef DOCKER_VERSION
        DOCKER := docker
	else ifdef PODMAN_VERSION
		DOCKER := podman
	endif
endif


COMMON_SRC = \
pkg/version/*.go

include $(CURDIR)/gpu.mk
include $(CURDIR)/gaudi.mk
include $(CURDIR)/qat.mk

.EXPORT_ALL_VARIABLES:


.PHONY: build
build: gpu gaudi qat bin/intel-cdi-specs-generator bin/device-faker


bin/intel-cdi-specs-generator: cmd/cdi-specs-generator/*.go $(GPU_COMMON_SRC)
	CGO_ENABLED=0 GOOS=linux GOARCH=${ARCH} \
	  go build -a -ldflags "${LDFLAGS}" -mod vendor -o $@ ./cmd/cdi-specs-generator

bin/device-faker: cmd/device-faker/*.go
	CGO_ENABLED=0 GOOS=linux GOARCH=${ARCH} \
	  go build -a -ldflags "${LDFLAGS}" -mod vendor -o $@ ./cmd/device-faker


.PHONY: branch-build
# test that all commits in $GIT_BRANCH (default=current) build
branch-build:
	current=$$(git branch --show-current); echo "Current branch: $$current"; \
	for commit in $$(git log --reverse --pretty=oneline origin/master...$(GIT_BRANCH) | cut -d' ' -f1); do \
		echo "Building: '$$commit'..."; git checkout $$commit && make build; done; \
	git checkout $$current

.PHONY: containers-build
containers-build: gpu-container-build gaudi-container-build

.PHONY: container-local
container-local: container-build
	$(DOCKER) save -o /tmp/temp_image.tar $(GPU_IMAGE_TAG)
	sudo ctr -n k8s.io image import /tmp/temp_image.tar
	$(DOCKER) save -o /tmp/temp_image.tar $(GAUDI_IMAGE_TAG)
	sudo ctr -n k8s.io image import /tmp/temp_image.tar
	rm /tmp/temp_image.tar

.PHONY: containers-push
containers-push: containers-build gpu-container-push gaudi-container-push

.PHONY: clean cleanall
clean:
	rm -rf bin/*
cleanall: clean
	rm -rf vendor/* bin/*

.PHONY: rm-clientsets
rm-clientsets: rm-gpu-clientset rm-gaudi-clientset

.PHONY: generate
generate: generate-gpu-crd generate-gaudi-crd

.PHONY: generate-deepcopy
generate-deepcopy: generate-gpu-deepcopy generate-gaudi-deepcopy

.PHONY: generate-clientsets
generate-clientsets: generate-gpu-clientset generate-gaudi-clientset

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
	save \
	"./cmd/kubelet-gaudi-plugin" \
	"./cmd/kubelet-gpu-plugin" \
	"./cmd/kubelet-qat-plugin" \
	"./cmd/cdi-specs-generator" \
	"./cmd/device-faker" \
	"./cmd/qat-showdevice" \
	"./pkg/gaudi/cdihelpers" \
	"./pkg/gaudi/device" \
	"./pkg/gaudi/discovery" \
	"./pkg/gpu/cdihelpers" \
	"./pkg/gpu/device" \
	"./pkg/gpu/discovery" \
	"./pkg/qat/cdi" \
	"./pkg/qat/device" \
	"./pkg/helpers" \
	"./pkg/fakesysfs" \
	"./pkg/plugintesthelpers" \
	"./pkg/version" \
	 --save_path licenses


# linting targets for Go and other code
.PHONY: lint format cilint vet shellcheck yamllint

lint: format cilint vet klogformat shellcheck yamllint

format:
	gofmt -w -s -l ./

cilint:
	golangci-lint --max-same-issues 0 --max-issues-per-linter 0 run --timeout 2m0s ./...

vet:
	go vet $(PKG)/...

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
	go test -v -coverprofile=$(COVERAGE_FILE) $(shell go list ./... | grep -v "test/e2e")

coverage: test
	go tool cover -html=$(COVERAGE_FILE) -o coverage.html
	@echo coverage file: coverage.html
	@echo "average coverage (except main.go files)"
	grep '<option value=' coverage.html | grep -v 'main.go' | grep -o '(.*)' | tr -d '()%' | awk 'BEGIN{s=0;}{s+=$$1;}END{print s/NR;}'

.PHONY: e2e-qat
e2e-qat:
	go test -v ./test/e2e/... --clean-start=true -ginkgo.v -ginkgo.trace -ginkgo.show-node-events
