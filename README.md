# Intel GPU resource driver

CAUTION: This is an beta / non-production software, do not use on production clusters.

## Glossary

- DRA https://github.com/kubernetes/enhancements/tree/master/keps/sig-node/3063-dynamic-resource-allocation
- CDI https://github.com/container-orchestrated-devices/container-device-interface/
- K8s https://github.com/kubernetes/kubernetes.git

## About resource driver

Intel GPU resource driver is a better alternative for
[Intel GPU device plugin](https://github.com/intel/intel-device-plugins-for-kubernetes/tree/main/cmd/gpu_plugin),
facilitating workload offloading by providing access to GPU devices available on worker nodes of Kubernetes cluster.

Supported GPU devices:

- Intel® Data Center GPU Flex Series
- Intel® Data Center GPU Max Series
- any integrated Intel GPU supported by the host kernel

### About Dynamic Resource Allocation

Dynamic Resource Allocation (DRA) is a resource management framework in Kubernetes (1.26+), that
allows management of special resources in cluster (typically HW accelerators) by vendor-provided
resource drivers (typically a controller and a node-agent / kubelet-plugin) in a common way.

Resource drivers are meant to handle discovery, allocation, accounting of specific resources as well
as their preparation for Pod before Pod startup and cleanup after the Pod has completed successfully
and the resource is no longer needed. More info is
[in the KEP](https://github.com/kubernetes/enhancements/tree/master/keps/sig-node/3063-dynamic-resource-allocation)

Intel GPU resource driver consists of the controller and kubelet plugin. Controller makes allocation
decisions and kubelet plugin ensures that the allocated GPUs and SR-IOV Virtual Functions are prepared
and available for Pods 

## Requirements

- Kubernetes 1.26, with `DynamicResourceAllocation` feature-flag enabled, and [other cluster parameters](hack/clusterconfig.yaml)
Container runtime needs to support CDI:
- CRI-O at least v1.23.0
- Containerd at least v1.7 (any release candidate will do)

## Supported Kubernetes Versions

Supported Kubernetes versions are listed below:

| Branch            | Kubernetes branch/version      | Status      |
|:------------------|:-------------------------------|:------------|
| v0.1.0-beta       | Kubernetes 1.26 branch v1.26.x | supported   |

[Go environment]: https://golang.org/doc/install
[Kubernetes cluster]: https://kubernetes.io/docs/setup/independent/create-cluster-kubeadm/

## [How to deploy and use](doc/USAGE.md)

## Building resource-driver

### Build a container
```bash
make container-build
```

NB: It is recommended to have a local registry to deploy from, so all cluster nodes can access same 
image and no need to build same image on evey node.
