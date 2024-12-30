## Requirements

- Kubernetes 1.32+, with `DynamicResourceAllocation` feature-flag enabled, and
[other cluster parameters](../../hack/clusterconfig.yaml)
- Container runtime needs to support CDI:
  - CRI-O v1.23.0 or newer
  - Containerd v1.7 or newer

## Deploy resource-driver

Deploy DeviceClass, Namespace and ResourceDriver
```bash
kubectl apply -f deployments/qat/device-class.yaml
kubectl apply -f deployments/qat/resource-driver-namespace.yaml
kubectl apply -f deployments/qat/resource-driver.yaml
```

By default the kubelet-plugin will be deployed on _all_ nodes in the cluster, there is no nodeSelector.

When deploying custom-built resource driver image, change `image:` lines in
[resource-driver](../../deployments/qat/resource-driver.yaml) to match its location.

## `deployment/` directory contains all required YAMLs:

* `deployments/qat/device-class.yaml` - pre-defined DeviceClass that ResourceClaims can refer to.
* `deployments/qat/resource-driver-namespace.yaml` - Kubernetes namespace for QAT resource driver.
* `deployments/qat/resource-driver.yaml` - actual resource driver with service account and RBAC policy
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

`IPC_LOCK` capability is required sinces VFIO based device access expects IPC_LOCK with the QAT sw stack.
