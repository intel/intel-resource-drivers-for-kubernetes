# Intel Gaudi resource driver for Kubernetes

CAUTION: This is an beta / non-production software, do not use on production clusters.

## Resource driver components

Intel Gaudi resource driver consists of the controller Pod, kubelet plugin DaemonSet and CustomResourceDefinitions.
Controller makes allocation decisions and kubelet plugin ensures that the allocated devices are prepared and available
for Pods.

## Supported Kubernetes Versions

Supported Kubernetes versions are listed below:

| Branch            | Kubernetes branch/version       | Status      |
|:------------------|:--------------------------------|:------------|
| v0.1.0            | Kubernetes 1.28+                | supported   |

[Kubernetes cluster]: https://kubernetes.io/docs/setup/independent/create-cluster-kubeadm/

## Documentation

- [How to setup a Kubernetes cluster with DRA enabled](../CLUSTER_SETUP.md)
- [How to deploy and use Intel Gaudi resource driver](USAGE.md)
- Optional: [How to build Intel Gaudi resource driver container image](BUILD.md)