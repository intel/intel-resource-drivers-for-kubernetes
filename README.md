# Intel GPU resource driver

CAUTION: This is an beta / non-production software, do not use on production clusters.

## Glossary

- DRA https://github.com/kubernetes/enhancements/tree/master/keps/sig-node/3063-dynamic-resource-allocation
- CDI https://github.com/container-orchestrated-devices/container-device-interface/
- K8s https://github.com/kubernetes/kubernetes.git

## About resource driver

Intel GPU resource driver is a better alternative for
[Intel GPU device plugin](https://github.com/intel/intel-device-plugins-for-kubernetes/tree/main/cmd/gpu_plugin),
facilitating workload offloading by providing GPU access on Kubernetes cluster worker nodes.

Supported GPU devices (with Linux kernel Intel `i915` GPU driver):
- Intel® Data Center GPU Max Series
- Intel® Data Center GPU Flex Series
- Intel® Arc A-Series
- Intel® Iris® Xe MAX
- Intel® Integrated graphics

### About Dynamic Resource Allocation

Dynamic Resource Allocation (DRA) is a resource management framework in Kubernetes (1.26+), that
allows management of special resources in cluster (typically HW accelerators) by vendor-provided
resource drivers (typically a controller and a node-agent / kubelet-plugin) in a common way.

Resource drivers are meant to handle discovery, allocation, accounting of specific resources as well
as their preparation for Pod before Pod startup, and cleanup after the Pod has completed successfully
and the resource is no longer needed. More info is
[in the KEP](https://github.com/kubernetes/enhancements/tree/master/keps/sig-node/3063-dynamic-resource-allocation)

Intel GPU resource driver consists of the controller and kubelet plugin. Controller makes allocation
decisions and kubelet plugin ensures that the allocated GPUs and SR-IOV Virtual Functions are prepared
and available for Pods.

## Requirements

- Kubernetes 1.28+, with `DynamicResourceAllocation` feature-flag enabled, and [other cluster parameters](hack/clusterconfig.yaml)
- Container runtime needs to support CDI:
  - CRI-O at least v1.23.0
  - Containerd at least v1.7 (any release candidate will do)

## Supported Kubernetes Versions

Supported Kubernetes versions are listed below:

| Branch            | Kubernetes branch/version       | Status      |
|:------------------|:--------------------------------|:------------|
| v0.1.0-beta       | Kubernetes 1.26 branch v1.26.x  | unsupported |
| v0.1.1-beta       | Kubernetes 1.27 branch v1.27.x  | unsupported |
| v0.2.0            | Kubernetes 1.28 branch v1.28.x  | unsupported |
| v0.3.0            | Kubernetes 1.28+                | supported   |

[Kubernetes cluster]: https://kubernetes.io/docs/setup/independent/create-cluster-kubeadm/

## Documentation

- [How to setup a Kubernetes cluster with DRA enabled](doc/CLUSTER_SETUP.md)
- [How to deploy and use Intel GPU resource driver](doc/gpu/USAGE.md)
- Optional: [How to build Intel GPU resource driver container image](doc/gpu/BUILD.md)

## Release process

Project's release cadence is quarterly. During the release process the issue is created in Github
to track progress based on [release task template](release_task_template.md).

Once the content is available in the main branch and validation PASSes, release branch will be
created (e.g. release-v0.2.0). The HEAD of release branch will also be tagged with the corresponding
tag (e.g. v0.2.0).

During the release creation, the project's documentation, deployment files etc. will be changed to
point to the newly created version.

Patch releases (e.g. 0.2.1) are done on a need basis if there are security issues or minor fixes
for specific supported version. Fixes are always cherry-picked from the main branch to the release
branches.
