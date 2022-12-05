# Copyright (c) 2022, Intel Corporation.  All Rights Reserved.
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

ARG GOLANG_VERSION=1.19

FROM golang:${GOLANG_VERSION} as build

WORKDIR /build
COPY . .

RUN make all && \
make licenses && \
mkdir -p /install_root/etc && \
adduser --disabled-password --quiet --gecos "" -u 10001 gas && \
tail -1 /etc/passwd > /install_root/etc/passwd && \
cp -r licenses/ /install_root/ && \
cp bin/* /install_root/

# check debian base version from
# https://github.com/kubernetes/kubernetes/blob/master/build/dependencies.yaml
FROM registry.k8s.io/build-image/debian-base:bullseye-v1.3.0

LABEL description="Intel GPU resource driver for Kubernetes"

COPY --from=build /install_root /
