# Intel GPU resource driver for Kubernetes

CAUTION: This is an beta / non-production software, do not use on production clusters.

Supported GPU devices (with Linux kernel Intel `i915` GPU driver):
- Intel® Data Center GPU Max Series
- Intel® Data Center GPU Flex Series
- Intel® Arc A-Series
- Intel® Iris® Xe MAX
- Intel® Integrated graphics

## Resource driver components

Intel GPU resource driver consists of the controller Pod, kubelet plugin DaemonSet and CustomResourceDefinitions.
Controller makes allocation decisions and kubelet plugin ensures that the allocated GPUs and SR-IOV Virtual Functions
are prepared and available for Pods.

## Supported Kubernetes Versions

Supported Kubernetes versions are listed below:

| Branch            | Kubernetes branch/version       | Status      |
|:------------------|:--------------------------------|:------------|
| v0.1.0-beta       | Kubernetes 1.26 branch v1.26.x  | unsupported |
| v0.1.1-beta       | Kubernetes 1.27 branch v1.27.x  | unsupported |
| v0.2.0            | Kubernetes 1.28 branch v1.28.x  | unsupported |
| v0.3.0            | Kubernetes 1.28+                | unsupported |
| v0.4.0            | Kubernetes 1.28+                | supported   |
| v0.5.0            | Kubernetes 1.28+                | supported   |

[Kubernetes cluster]: https://kubernetes.io/docs/setup/independent/create-cluster-kubeadm/

## Documentation

- [How to setup a Kubernetes cluster with DRA enabled](../CLUSTER_SETUP.md)
- [How to deploy and use Intel GPU resource driver](USAGE.md)
- Optional: [How to build Intel GPU resource driver container image](BUILD.md)