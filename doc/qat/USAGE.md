## Requirements

- Kubernetes 1.31+, with `DynamicResourceAllocation` feature-flag enabled, and
[other cluster parameters](../../hack/clusterconfig.yaml)
- Container runtime needs to support CDI:
  - CRI-O v1.23.0 or newer
  - Containerd v1.7 or newer

## Deploy resource-driver

Deploy DeviceClass, Namespace and ResourceDriver
```bash
kubectl apply -f deployments/qat/resource-class.yaml
kubectl apply -f deployments/qat/resource-driver-namespace.yaml
kubectl apply -f deployments/qat/resource-driver.yaml
```

By default the kubelet-plugin will be deployed on _all_ nodes in the cluster, there is no nodeSelector.

When deploying custom-built resource driver image, change `image:` lines in
[resource-driver](../../deployments/qat/resource-driver.yaml) to match its location.

## `deployment/` directory contains all required YAMLs:

* `deployments/qat/resource-class.yaml` - pre-defined DeviceClass that ResourceClaims can refer to.
* `deployments/qat/resource-driver-namespace.yaml` - Kubernetes namespace for QAT resource driver.
* `deployments/qat/resource-driver.yaml` - actual resource driver with service account and RBAC policy
  - kubelet-plugin DaemonSet - node-agent which performs three functions:
    1) discovery of supported hardware on the Kubernetes cluster node and its announcement as a ResourceSlice.
    2) preparation of the hardware allocated to the ResourceClaims for the Pod that is being started on the node.
    3) unpreparation of the hardware allocated to the ResourceClaims for the Pod that has stopped and reached final state on the node.

* `deployments/qat/examples/` - test cases for running a pod with a QAT accelerator, including the examples of the required configuration and resource claim templates.

  - The simplest way to use the Intel® QAT resource driver is to create a ResourceClaim
  and add it to the Pod spec. The Intel® QAT resource driver will take care of allocating
  a suitable device to the Resource Claim when Kubernetes schedules the Pod on the node.

    Example:
    ```
    apiVersion: resource.k8s.io/v1alpha3
    kind: ResourceClaimTemplate
    metadata:
      name: qat-template-sym-asym
    spec:
      spec:
        devices:
          requests:
          - name: qat-request-sym-asym
            deviceClassName: qat.intel.com
            selectors:
            - cel:
              expression: |-
                  device.driver == "qat.intel.com" &&
                  device.attributes["qat.intel.com"].services.matches("sym;asym")

    ---
    apiVersion: v1
    kind: Deployment
    metadata:
      name: qat-sample-sym-asym
      labels:
        app: inline-qat-deployment
    spec:
      replicas: 1
      selector:
        matchLabels:
          app: inline-qat-deployment
      template:
        metadata:
          labels:
            app: inline-qat-deployment
        spec:
          containers:
          - name: with-resource
            image: registry.k8s.io/e2e-test-images/busybox:1.29-2
            command: ["sh", "-c", "ls -la /dev/vfio/ && sleep 300"]
            securityContext:
              capabilities:
                add:
                  ["IPC_LOCK"]
            resources:
              claims:
              - name: resource-sym-asym
          resourceClaims:
          - name: resource-sym-asym
            resourceClaimTemplateName: qat-template-sym-asym
    ```
    QAT services are matched by CEL expression; in the example above, `sym` and `asym`
    services are considered in the regular expression. Examples of other common service
    matches include `sym;asym`, `[^a]?sym` and `dc`, see [README](README.md#qat-service-configuration).

> **Note**: `IPC_LOCK`capability is [strongly recommended](README.md#qat-service-configuration).

### Basic use case: Pod with QAT accelerator
```
kubectl apply -f deployments/qat/examples/resource-template.yaml
kubectl apply -f deployments/qat/examples/deployment-inline.yaml
```

### Other use cases: Test cases in Intel® QAT Device Plugin

There are test cases made for [Intel® QAT Device Plugin](https://github.com/intel/intel-device-plugins-for-kubernetes/blob/main/cmd/qat_plugin/README.md). It is possible to run those images using this resource driver. Those images are available in the following links.

- [openssl-qat-engine](https://github.com/intel/intel-device-plugins-for-kubernetes/tree/main/demo/openssl-qat-engine)
- [qat-dpdk-test](https://github.com/intel/intel-device-plugins-for-kubernetes/tree/main/demo/crypto-perf)

After building those images, create a resourceClaimTemaplate and run the pod in `deployments/qat/<test case>`.
For example:
```
kubectl apply -f deployments/qat/examples/resource-claim-template.yaml
kubectl apply -k deployments/qat/examples/openssl-qat-engine
kubectl apply -k deployments/qat/examples/qat-dpdk-test
```

To run `qat-dpdk-test`, the cluster should have `CPU Manager Policy` as `static`
in its kubelet configuration. In addition, `hugepages-2Mi` resource should be
available.
Uncomment the related parts in `hack/clusterconfig.yaml` to have enable them and
create a cluster with the configuration file.
