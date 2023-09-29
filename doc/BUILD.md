# How to build Intel GPU Resource Driver container image

## Platforms supported

- Linux

## Prerequisites

- Docker or Podman.

## Building

`Makefile` automates this, only required tool is Docker or Podman.
To build the container image locally, from the root of this Git repository:
```bash
make container-build
```

It is possible to specify custom registry, container image name, and version (tag) as separate
variables to override any part of release container image URL in the build command, e.g.:
```bash
REGISTRY=myregistry IMAGENAME=myimage IMAGE_VERSION=myversion make container-build
```

or whole resulting image URL (this will ignore REGISTRY, IMAGENAME, IMAGE_VERSION even if specified):
```bash
IMAGE_TAG=myregistry/myimagename:myversion make container-build
```

To build the container image and push image to the destination registry straight away:
```bash
REGISTRY=registry.local make container-push
```
or
```bash
IMAGE_TAG=registry.local/intel-gpu-resource-driver:latest make container-push
```
