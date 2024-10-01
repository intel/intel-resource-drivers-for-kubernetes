# Intel resource drivers for Kubernetes

CAUTION: This is an beta / non-production software, do not use on production clusters.

## This repository containes following resource drivers:

- [GPU](doc/gpu/README.md)
- [Gaudi](doc/gaudi/README.md)
- [QAT](doc/qat/README.md)

## Glossary

- DRA https://github.com/kubernetes/enhancements/tree/master/keps/sig-node/3063-dynamic-resource-allocation
- CDI https://github.com/cncf-tags/container-device-interface/
- K8s https://github.com/kubernetes/kubernetes.git

## About resource drivers

Intel resource drivers for Kubernetes is an alternative for
[Intel device plugins](https://github.com/intel/intel-device-plugins-for-kubernetes/),
facilitating workload offloading by providing accelerator access on Kubernetes cluster worker nodes.

Resource drivers are not Linux kernel mode drivers (KMD), and do not help the operational system on
the worker nodes detect and operate the accelerators.

The resource drivers are based on Dynamic Resource Allocation (DRA) framework in Kubernetes

### About Dynamic Resource Allocation

Dynamic Resource Allocation (DRA) is a resource management framework in Kubernetes (1.26+), that
allows management of special resources in cluster (typically HW accelerators) by vendor-provided
resource drivers (typically a controller and a node-agent / kubelet-plugin) in a common way.

Resource drivers are meant to handle discovery, allocation, accounting of specific resources as well
as their preparation for Pod before Pod startup, and cleanup after the Pod has completed successfully
and the resource is no longer needed. More info is
[in the KEP](https://github.com/kubernetes/enhancements/tree/master/keps/sig-node/3063-dynamic-resource-allocation)


## Release process

Every resource driver in this repository has its own releases, release branches and version tags.

Typical release cadence is quarterly. During the release creation the project's documentation,
deployment files etc. will be changed to point to the newly created version.

Once the content is available in the main branch and validation PASSes, release branch will be
created (e.g. gpu-release-v0.2.0). The HEAD of release branch will also be tagged with the corresponding
tag (e.g. gpu-v0.2.0).

During the release creation, the project's documentation, deployment files etc. will be changed to
point to the newly created version.

Patch releases (e.g. gaudi-v0.1.1) are done on a need basis if there are security issues or minor fixes
for specific supported version. Fixes are always cherry-picked from the main branch to the release
branches.
