# How to build Intel® QAT Resource Driver container image

## Platforms supported

- Linux

## Prerequisites

- Docker or Podman.

## Building

`Makefile` automates this, only required tool is Docker or Podman.
To build the container image locally, from the root of this Git repository:
```bash
make qat-container-build
```

It is possible to specify custom registry, container image name, and version (tag) as separate
variables to override any part of release container image URL in the build command, e.g.:
```bash
REGISTRY=myregistry QAT_IMAGE_NAME=myimage QAT_IMAGE_VERSION=myversion make qat-container-build
```

or whole resulting image URL (this will ignore REGISTRY, QAT_IMAGE_NAME, QAT_IMAGE_VERSION even if specified):
```bash
QAT_IMAGE_TAG=myregistry/myimagename:myversion make qat-container-build
```

To build the container image and push image to the destination registry straight away:
```bash
REGISTRY=registry.local make qat-container-push
```
or
```bash
QAT_IMAGE_TAG=registry.local/intel-qat-resource-driver:latest make qat-container-push
```
