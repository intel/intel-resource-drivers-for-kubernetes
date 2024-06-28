# How to build Intel Gaudi Resource Driver container image

## Platforms supported

- Linux

## Prerequisites

- Docker or Podman.

## Building

`Makefile` automates this, only required tool is Docker or Podman.
To build the container image locally, from the root of this Git repository:
```bash
make gaudi-container-build
```

It is possible to specify custom registry, container image name, and version (tag) as separate
variables to override any part of release container image URL in the build command, e.g.:
```bash
REGISTRY=myregistry GAUDI_IMAGE_NAME=myimage GAUDI_IMAGE_VERSION=myversion make gaudi-container-build
```

or whole resulting image URL (this will ignore REGISTRY, GAUDI_IMAGE_NAME, GAUDI_IMAGE_VERSION even if specified):
```bash
GAUDI_IMAGE_TAG=myregistry/myimagename:myversion make gaudi-container-build
```

To build the container image and push image to the destination registry straight away:
```bash
REGISTRY=registry.local make gaudi-container-push
```
or
```bash
GAUDI_IMAGE_TAG=registry.local/intel-gaudi-resource-driver:latest make gaudi-container-push
```
