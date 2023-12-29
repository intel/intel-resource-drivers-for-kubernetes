# Copyright (c) 2023, Intel Corporation.  All Rights Reserved.
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

ARG GOLANG_VERSION=1.21

FROM golang:${GOLANG_VERSION} as build
ARG LOCAL_LICENSES
WORKDIR /build
COPY . .

RUN make build && \
mkdir -p /install_root && \
if [ -z "$LOCAL_LICENSES" ]; then \
    make licenses; \
fi && \
cp -r licenses /install_root/ && \
cp bin/* /install_root/

# check debian base version from
# https://github.com/kubernetes/kubernetes/blob/master/build/dependencies.yaml
FROM scratch
WORKDIR /
LABEL description="Intel GPU resource driver for Kubernetes"

COPY --from=build /install_root /
