# Intel GPU resource driver for Kubernetes

CAUTION: This is a beta / non-production software, do not use on production clusters.

## About resource driver

With structured parameters (K8s v1.31+), the DRA driver publishes ResourceSlice, scheduler allocates
the resources and DRA driver kubelet-plugin ensures that the allocated devices are prepared
and available for Pods.

DRA API graduated to GA with v1 API in K8s v1.34, backwards compatibility may vary
depending on features enabled.

## Supported GPU devices

Intel GPU DRA driver relies on the host Linux kernel [Intel GPU driver(s)](https://dgpu-docs.intel.com/driver/kernel-driver-types.html) to detect the devices.
See the [supported hardware](https://dgpu-docs.intel.com/devices/hardware-table.html)
section in the Intel GPU driver support documentation.

(To _use_ the devices, workload containers need to include a suitable Intel GPU user space driver.  See that documentation site on how to install them.)

## Supported Kubernetes Versions

Supported Kubernetes versions are listed below:

| Branch            | Kubernetes branch/version        | Status      | DRA                            |
|:------------------|:---------------------------------|:------------|:-------------------------------|
| v0.1.0-beta       | Kubernetes v1.26 branch v1.26.x  | unsupported | Classic                        |
| v0.1.1-beta       | Kubernetes v1.27 branch v1.27.x  | unsupported | Classic                        |
| v0.2.0            | Kubernetes v1.28 branch v1.28.x  | unsupported | Classic                        |
| v0.3.0            | Kubernetes v1.28+                | unsupported | Classic                        |
| v0.4.0            | Kubernetes v1.28+                | unsupported | Classic                        |
| v0.5.0            | Kubernetes v1.27 - v1.30         | unsupported | Classic, Structured Parameters |
| v0.6.0            | Kubernetes v1.31                 | unsupported | Structured Parameters          |
| v0.7.0            | Kubernetes v1.32+                | unsupported | Structured Parameters          |
| v0.8.0            | Kubernetes v1.33-v1.34           | unsupported | Structured Parameters          |
| v0.9.0            | Kubernetes v1.32+                | unsupported | Structured Parameters          |
| v0.10.0           | Kubernetes v1.34+                | supported   | Structured Parameters          |

## Documentation

- [How to setup a Kubernetes cluster with DRA enabled](../CLUSTER_SETUP.md)
- [How to deploy and use Intel GPU resource driver](USAGE.md)
- Optional: [How to build Intel GPU resource driver container image](BUILD.md)
