# Intel GPU resource driver

CAUTION: This is an alpha / non-production software, do not use on production systems.

## Glossary
- DRA https://github.com/kubernetes/enhancements/tree/master/keps/sig-node/3063-dynamic-resource-allocation
- CDI https://github.com/container-orchestrated-devices/container-device-interface/
- K8s https://github.com/kubernetes/kubernetes.git

## About driver
Dynamic Resource Allocation (DRA) is a concept in k8s that allows management of special resources
in cluster (typically HW accelerators) by vendor-provided resource-drivers (typically a controller
and a node-agent / kubelet-plugin) in a common way. Resource drivers are meant to handle discovery,
allocation, accounting of resources as well as preparation of such for upcoming Pod and cleanup
after the resource is no longer needed. More info is
[in the KEP](https://github.com/kubernetes/enhancements/tree/master/keps/sig-node/3063-dynamic-resource-allocation)

GPU resource driver consists of a driver controller and a resource kubelet plugin.

# Current limitations

- Max 8 GPUs can be requested for one resource claim
- Max 128GiB can be requested per GPU
- No HW features discovery

# Requirements

Kubernetes 1.26+, `DynamicResourceAllocation` feature-flag has to be enabled, and [other parameters](hack/clusterconfig.yaml)
Container runtime needs to support CDI:
- CRI-O at least v1.23.0
- Containerd at least v1.7

# [How to deploy and use](USAGE.md)

# Building resource-driver

## Build a container
```bash
make container-build
```

NB: It is recommended to have a local registry to deploy from, so all cluster nodes can access same image and no need to build same image on evey node.
