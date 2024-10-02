# Intel Gaudi resource driver for Kubernetes

CAUTION: This is an beta / non-production software, do not use on production clusters.

## About resource driver

With structured parameters (K8s v1.31+), the DRA driver publishes ResourceSlice, scheduler allocates
the resources and resource driver's kubelet-plugin ensures that the allocated devices are prepared
and available for Pods.

## Supported Kubernetes Versions

Supported Kubernetes versions are listed below:

| Branch            | Kubernetes branch/version       | Status      | DRA                            |
|:------------------|:--------------------------------|:------------|:-------------------------------|
| v0.1.0            | Kubernetes v1.27 ~ v1.30        | supported   | Classic, Structured Parameters |
| v0.2.0            | Kubernetes v1.31+               | supported   | Structured Parameters          |

## Documentation

- [How to setup a Kubernetes cluster with DRA enabled](../../CLUSTER_SETUP.md)
- [How to deploy and use Intel Gaudi resource driver](USAGE.md)
- Optional: [How to build Intel Gaudi resource driver container image](BUILD.md)