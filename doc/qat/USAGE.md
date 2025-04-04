## Requirements

- Kubernetes 1.32+, with `DynamicResourceAllocation` feature-flag enabled, and
[other cluster parameters](../../hack/clusterconfig.yaml)
- Container runtime needs to support CDI:
  - CRI-O v1.23.0 or newer
  - Containerd v1.7 or newer

## Deploy resource-driver

Deploy DeviceClass, Namespace and ResourceDriver
```bash
kubectl apply -k deployments/qat/
```

By default, the kubelet-plugin is deployed on _all_ nodes in the cluster, as no nodeSelector is defined.
To restrict the deployment to QAT-enabled nodes, follow these steps:

1. Install Node Feature Discovery (NFD):

Follow [Node Feature Discovery](https://github.com/kubernetes-sigs/node-feature-discovery) documentation to install and configure NFD in your cluster.

```bash
kubectl apply -k "https://github.com/kubernetes-sigs/node-feature-discovery/deployment/overlays/default?ref=v0.17.1"
```

2. Apply NFD Rules:

```bash
kubectl apply -k deployments/qat/overlays/nfd_labeled_nodes/
```
After NFD is installed and running, make sure the target node is labeled with:
```bash
intel.feature.node.kubernetes.io/qat: "true"
```

When deploying custom-built resource driver image, change `image:` lines in
[resource-driver](../../deployments/qat/base/resource-driver.yaml) to match its location.


## `deployment/` directory contains all required YAMLs:

* `deployments/qat/base/device-class.yaml` - pre-defined DeviceClass that ResourceClaims can refer to.
* `deployments/qat/base/namespace.yaml` - Kubernetes namespace for QAT resource driver.
* `deployments/qat/base/resource-driver.yaml` - actual resource driver with service account and RBAC policy
  - kubelet-plugin DaemonSet - node-agent which performs three functions:
    1) discovery of supported hardware on the Kubernetes cluster node and its announcement as a ResourceSlice.
    2) preparation of the hardware allocated to the ResourceClaims for the Pod that is being started on the node.
    3) unpreparation of the hardware allocated to the ResourceClaims for the Pod that has stopped and reached final state on the node.

### Example use case: Pod with QAT accelerator

The simplest way to use the Intel® QAT resource driver is to create a ResourceClaim
and add it to the Pod spec. The Intel® QAT resource driver will take care of allocating
a suitable device to the Resource Claim when Kubernetes schedules the Pod on the node.

Example:
```
apiVersion: resource.k8s.io/v1beta1
kind: ResourceClaimTemplate
metadata:
  name: qat-template-sym
spec:
  spec:
    devices:
      requests:
      - name: qat-request-sym
        deviceClassName: qat.intel.com
        selectors:
        - cel:
           expression: |-
              device.attributes["qat.intel.com"].services == "sym" ||
              device.attributes["qat.intel.com"].services == "sym;asym" ||
              device.attributes["qat.intel.com"].services == "sym;dc" ||
              device.attributes["qat.intel.com"].services == "asym;sym" ||
              device.attributes["qat.intel.com"].services == "dc;sym" ||

---
apiVersion: v1
kind: Deployment
metadata:
  name: qat-sample-sym
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
          - name: resource-sym
      resourceClaims:
      - name: resource-sym
        resourceClaimTemplateName: qat-template-sym
```
QAT services are matched by CEL expression; in the example above, `sym` and `asym`
services are considered in the regular expression. Examples of other common service
matches include `sym;asym`, `[^a]?sym` and `dc`, see [README](README.md#qat-service-configuration).

`IPC_LOCK` capability is required sinces VFIO based device access expects IPC_LOCK with the QAT sw stack.
