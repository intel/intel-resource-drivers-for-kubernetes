# Intel® QAT resource driver for Kubernetes

CAUTION: This is an beta / non-production software, do not use on production clusters.

## About resource driver

With structured parameters (K8s v1.31+), the DRA driver publishes ResourceSlice, scheduler allocates
the resources and resource driver's kubelet-plugin ensures that the allocated devices are prepared
and available for Pods.

## Host OS requirements

In order to guarantee proper operation, ensure Linux kernel module `vfio_pci` has been loaded.

The QAT Kubernetes resource driver is intended to be used on upstream Linux kernels,
see [the in-tree kernel documentation](https://intel.github.io/quickassist/RN/In-Tree/in_tree_firmware_RN.html)
for details. Note though, that the QAT resource driver itself does not depend on
any QAT user space libraries mentioned in that document.

## Supported QAT devices

All 4th Gen Intel® Xeon® Scalable Processor QAT devices handled by the Linux kernel
driver module `qat_4xxx` are supported.

## Supported Kubernetes Versions

Supported Kubernetes versions are listed below:

| Branch            | Kubernetes branch/version       | Status      | DRA                            |
|:------------------|:--------------------------------|:------------|:-------------------------------|
| v0.1.0            | Kubernetes v1.31                | supported   | Structured Parameters          |

## QAT service configuration

In version 0.1.0 static configuration of QAT services is done using a ConfigMap,
please have a look at
[the example ConfigMap yaml](../../deployments/qat/examples/intel-qat-resource-driver-configuration.yaml).

The ConfigMap and Resource Claims use the same string notation as the QAT kernel
driver when specifying what services are to be configured for the device and Resource
Claim. When two services are requested, the service strings are to be separated by
semicolon (';'). Supported services are:
* Symmetric cryptography: `sym`
* Asymmetric cryptograpy: `asym`
* Compression: `dc`

For symmetric and asymmetric cryptography the `IPC_LOCK` capability is strongly recommended.

## Documentation

- [How to setup a Kubernetes cluster with DRA enabled](../CLUSTER_SETUP.md)
- [How to deploy and use Intel® QAT resource driver](USAGE.md)
- Optional: [How to build Intel® QAT resource driver container image](BUILD.md)