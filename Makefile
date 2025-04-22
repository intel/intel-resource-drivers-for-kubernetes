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

TEST_IMAGE ?= gaudi-dra-driver-test-image:latest

EXT_LDFLAGS = -static
LDFLAGS = \
 -s -w \
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
	  go build -a -ldflags "${LDFLAGS} -extldflags $(EXT_LDFLAGS)" \
	  -mod vendor -o $@ ./cmd/cdi-specs-generator

bin/device-faker: cmd/device-faker/*.go
	CGO_ENABLED=0 GOOS=linux GOARCH=${ARCH} \
	  go build -a -ldflags "${LDFLAGS} -extldflags ${EXT_LDFLAGS}" \
	  -mod vendor -o $@ ./cmd/device-faker


.PHONY: branch-build
# test that all commits in $GIT_BRANCH (default=current) build
branch-build:
	current=$$(git branch --show-current); echo "Current branch: $$current"; \
	for commit in $$(git log --reverse --pretty=oneline origin/master...$(GIT_BRANCH) | cut -d' ' -f1); do \
		echo "Building: '$$commit'..."; git checkout $$commit && make build; done; \
	git checkout $$current

.PHONY: containers-build
containers-build: gpu-container-build gaudi-container-build qat-container-build

.PHONY: container-local
container-local: container-build
	$(DOCKER) save -o /tmp/temp_image.tar $(GPU_IMAGE_TAG)
	sudo ctr -n k8s.io image import /tmp/temp_image.tar
	$(DOCKER) save -o /tmp/temp_image.tar $(GAUDI_IMAGE_TAG)
	sudo ctr -n k8s.io image import /tmp/temp_image.tar
	rm /tmp/temp_image.tar

.PHONY: containers-push
containers-push: containers-build gpu-container-push gaudi-container-push qat-container-push

.PHONY: clean cleanall
clean:
	rm -rf bin/*
cleanall: clean
	rm -rf vendor/* bin/*

.PHONY: rm-clientsets
rm-clientsets: rm-gpu-clientset rm-gaudi-clientset

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

lint: vendor format cilint vet klogformat shellcheck yamllint

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

.PHONE: test-image test-image-push
test-image: vendor
	@echo "Building container image with fake HLML for Gaudi tests with user $(shell id -u):$(shell id -g)"
	$(DOCKER) build \
	--build-arg UID=$(shell id -u) --build-arg GID=$(shell id -g) \
	--platform="linux/$(ARCH)" \
	-t "$(TEST_IMAGE)" 	-f Dockerfile.gaudi-test .

test-image-push: test-image
	$(DOCKER) push "$(TEST_IMAGE)"

.PHONY: update-dependencies package-helm-charts push-helm-charts
update-dependencies:
	@helm repo add nfd https://kubernetes-sigs.github.io/node-feature-discovery/charts || true
	@helm repo update
	@set -x; for chart in charts/*; do \
		if [ -d "$$chart" ]; then \
			echo "Updating dependencies for $$chart"; \
			helm dependency update $$chart; \
			helm dependency build $$chart; \
		fi \
	done

package-helm-charts:
	@set -x; for chart in charts/*; do \
		if [ -d "$$chart" ]; then \
			chart_name=$$(basename $$chart); \
			chart_version=$$(awk '/^version:/ {print $$2; exit}' $$chart/Chart.yaml); \
			release_version=$$(awk '/^appVersion:/ {print $$2; exit}' $$chart/Chart.yaml); \
			echo "Packaging $$chart_name with chart version $$chart_version and application version $$release_version"; \
			helm package $$chart --version $$chart_version --app-version $$release_version --destination .charts; \
		fi \
	done

push-helm-charts: package-helm-charts
	@for tgz in .charts/*.tgz; do \
		helm push $$tgz oci://${RELEASE_REGISTRY}; \
	done

.PHONY: test html-coverage test-containerized
COVERAGE_FILE := coverage.out
# Gaudi tests expect fake HLML library to be present at /usr/lib/habanalabs/libhlml.so
# Dependency comes from gohlml package hardcoded LD_LIBRARY_PATH pointing to it.
test:
ifeq ("$(container)","yes")
		@echo setting safe directory
		go test -buildvcs=false -v -coverprofile=$(COVERAGE_FILE) $(shell go list ./... | grep -v "test/e2e")
else
		@echo running tests
		go test -v -coverprofile=$(COVERAGE_FILE) $(shell go list ./... | grep -v "test/e2e")
endif

test-containerized:
	$(DOCKER) run \
	-it -e container=yes \
	--user 1000:1000 \
	-v "$(shell pwd)":/home/ubuntu/src:rw \
	"$(TEST_IMAGE)" \
	bash -c "cd src && make test"

html-coverage: $(COVERAGE_FILE)
	go tool cover -html=$(COVERAGE_FILE) -o coverage.html
	@echo coverage file: coverage.html

$(COVERAGE_FILE): $(shell find cmd pkg -name '*.go')
	go test -v -coverprofile=$(COVERAGE_FILE) $(shell go list ./... | grep -v "test/e2e")

.PHONY: gpu-coverage gaudi-coverage qat-coverage cdispecsgen-coverage excluded-coverage

gpu-coverage: COVERAGE_EXCLUDE="cdi-specs-generator|device-faker|kubelet-gaudi-plugin|kubelet-qat-plugin|qat-showdevice|pkg/qat|pkg/gaudi|pkg/fakesysfs|plugintesthelpers|fake_hlml|version"
gpu-coverage: excluded-coverage
# See: https://www.gnu.org/software/make/manual/html_node/Target_002dspecific.html

gaudi-coverage: COVERAGE_EXCLUDE="cdi-specs-generator|device-faker|kubelet-gpu-plugin|kubelet-qat-plugin|qat-showdevice|pkg/qat|pkg/gpu|pkg/fakesysfs|plugintesthelpers|version"
gaudi-coverage: excluded-coverage

qat-coverage: COVERAGE_EXCLUDE="cdi-specs-generator|device-faker|kubelet-gpu-plugin|kubelet-gaudi-plugin|pkg/gpu|pkg/gaudi|pkg/fakesysfs|plugintesthelpers|fake_hlml|version"
qat-coverage: excluded-coverage

cdispecsgen-coverage: COVERAGE_EXCLUDE="device-faker|kubelet-gpu-plugin|kubelet-gaudi-plugin|kubelet-qat-plugin|qat-showdevice|pkg/qat|pkg/gpu|pkg/gaudi|pkg/fakesysfs|plugintesthelpers|fake_hlml|version"
cdispecsgen-coverage: excluded-coverage

COVERAGE_EXCLUDE ?= "$^"
excluded-coverage: $(COVERAGE_FILE)
	@grep -v -E $(COVERAGE_EXCLUDE) $(COVERAGE_FILE) > $(COVERAGE_FILE).tmp && \
	go tool cover -func=$(COVERAGE_FILE).tmp && \
	rm $(COVERAGE_FILE).tmp
