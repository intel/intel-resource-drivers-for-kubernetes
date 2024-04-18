# How to build Intel GPU Resource Driver container image

## Platforms supported

- Linux

## Prerequisites

- Docker or Podman.

## Building

`Makefile` automates this, only required tool is Docker or Podman.
To build the container image locally, from the root of this Git repository:
```bash
make gpu-container-build
```

It is possible to specify custom registry, container image name, and version (tag) as separate
variables to override any part of release container image URL in the build command, e.g.:
```bash
REGISTRY=myregistry GPU_IMAGE_NAME=myimage GPU_IMAGE_VERSION=myversion make gpu-container-build
```

or whole resulting image URL (this will ignore REGISTRY, GPU_IMAGE_NAME, GPU_IMAGE_VERSION even if specified):
```bash
GPU_IMAGE_TAG=myregistry/myimagename:myversion make gpu-container-build
```

To build the container image and push image to the destination registry straight away:
```bash
REGISTRY=registry.local make gpu-container-push
```
or
```bash
GPU_IMAGE_TAG=registry.local/intel-gpu-resource-driver:latest make gpu-container-push
```
