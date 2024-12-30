## Requirements

- Kubernetes 1.31+, with `DynamicResourceAllocation` feature-flag enabled, and [other cluster parameters](../../hack/clusterconfig.yaml)
- Container runtime needs to support CDI:
  - CRI-O v1.23.0 or newer
  - Containerd v1.7 or newer
- [habanalabs container runtime](https://docs.habana.ai/en/latest/Installation_Guide/Bare_Metal_Fresh_OS.html#set-up-container-runtime) with CDI support

## Deploy resource-driver

Deploy DeviceClass, Namespace and ResourceDriver
```bash
kubectl apply -f deployments/gaudi/device-class.yaml
kubectl apply -f deployments/gaudi/resource-driver-namespace.yaml
kubectl apply -f deployments/gaudi/resource-driver.yaml
```

By default the kubelet-plugin will be deployed on _all_ nodes in the cluster, there is no nodeSelector.

When deploying custom-built resource driver image, change `image:` lines in
[resource-driver](../../deployments/gaudi/resource-driver.yaml) to match its location.

## `deployment/` directory contains all required YAMLs:

* `deployments/gaudi/device-class.yaml` - pre-defined ResourceClasses that ResourceClaims can refer to.
* `deployments/gaudi/resource-driver-namespace.yaml` - Kubernetes namespace for Gaudi resource driver.
* `deployments/gaudi/resource-driver.yaml` - actual resource driver with service account and RBAC policy
  - kubelet-plugin DaemonSet - node-agent, it performs three functions:
    1) supported hardware discovery on Kubernetes cluster node and it's announcement as a ResourceSlice.
    2) preparation of the hardware allocated to the ResourceClaims for the Pod that is being started on the node.
    3) unpreparation of the hardware allocated to the ResourceClaims for the Pod that is being started on the node

## Deployment validation

After kubelet-plugin pods are ready, check ResourceSlice objects and their contents:
```bash
$ kubectl get resourceSlices
NAME                          NODE    DRIVER            POOL    AGE
rpl-s-gaudi.intel.com-x8m4h   rpl-s   gaudi.intel.com   rpl-s   4d1h
```

Example contents of the ResourceSlice object:
```bash
$ kubectl get resourceSlices/rpl-s-gaudi.intel.com-x8m4h -o yaml
apiVersion: resource.k8s.io/v1beta1
kind: ResourceSlice
metadata:
  creationTimestamp: "2024-09-23T13:03:21Z"
  generateName: rpl-s-gaudi.intel.com-
  generation: 1
  name: rpl-s-gaudi.intel.com-x8m4h
  ownerReferences:
  - apiVersion: v1
    controller: true
    kind: Node
    name: rpl-s
    uid: 0894e000-e7a3-49ad-8749-04b27be61c03
  resourceVersion: "2047239"
  uid: 92fb64c7-219e-4cef-9be9-5233b589d7bd
spec:
  devices:
  - basic:
      attributes:
        model:
          string: Gaudi2
    name: 0000-a0-00-0-0x1020
  - basic:
      attributes:
        model:
          string: Gaudi2
    name: 0000-b0-00-0-0x1020
  - basic:
      attributes:
        model:
          string: Gaudi2
    name: 0000-c0-00-0-0x1020
  driver: gaudi.intel.com
  nodeName: rpl-s
  pool:
    generation: 0
    name: rpl-s
    resourceSliceCount: 1
```

## Deploying test pod to verify Gaudi resource-driver works

```bash
$ kubectl apply -f deployments/examples/pod-inline.yaml
resourceclaim.resource.k8s.io/claim1 created
pod/test-inline-claim created
```

When the Pod gets into Running state, check that Gaudi was assigned to it:
```bash
$ kubectl logs po/test-inline-claim
Defaulted container "with-resource" out of: with-resource, without-resource
total 0
drwxr-xr-x    2 root     root            80 Sep 27 14:30 .
drwxr-xr-x    6 root     root           380 Sep 27 14:30 ..
crw-rw-rw-    1 root     root        1,   3 Sep 27 14:30 accel0
crw-rw-rw-    1 root     root        1,   3 Sep 27 14:30 accel_controlD0

```

## Requesting resources

With Dynamic Resource Allocation the resources are requested in a similar way to how the persistent
storage is requested. The ResourceClaim is an analog of Persistent Volume Claim,
and it is used for scheduling Pods to nodes based on the devices availability. It provides access
to devices in Pod's containers.

### Basic use case: Pod needs a Gaudi accelerator

The simplest way to start using Intel Gaudi resource driver is to create a ResourceClaim, and add it
to Pod spec to be used in container. The Intel Gaudi resource driver will take care of allocating
suitable device to the Resource Claim when Kubernetes is scheduling the Pod.

```yaml
apiVersion: resource.k8s.io/v1beta1
kind: ResourceClaim
metadata:
  name: claim1
spec:
  devices:
    requests:
    - name: gaudi
      deviceClassName: gaudi.intel.com
---
apiVersion: v1
kind: Pod
metadata:
  name: test-inline-claim
spec:
  restartPolicy: Never
  containers:
  - name: with-resource
    image: registry.k8s.io/e2e-test-images/busybox:1.29-2
    command: ["sh", "-c", "ls -la /dev/accel/ && sleep 60"]
    resources:
      claims:
      - name: resource
  resourceClaims:
  - name: resource
    resourceClaimName: claim1

```

Two important sections in above Pod spec are:
- `resourceClaims` - all ResourceClaims that the Pod will use, need to be here
- `claims` - is the new section in container's `resources` section. If the container
  needs to use a ResourceClaim - the Claim needs to be listed in this section for
  that container.

In this example:
- the ResourceClaim `claim1` is created;
- the Pod `test-inline-claim` declares that:
  - it will use Resource Claim `claim1`;
  - the container named `with-resource` will be using the resources allocated to the Resource Claim
    `claim1`.

### Device Class

Intel Gaudi resource driver provides following device class:
- `gaudi.intel.com`

### Advanced use cases

#### Creation of Resource Claim

There are two ways to create a Resource Claim:
- creating it explicitly as a `ResourceClaim` object
- letting K8s generate Resource Claim from existing `ResourceClaimTemplate` when the Pod is created

When referencing a ResourceClaim in Pod spec - the claim has to exist.

When Pod spec references a ResourceClaimTemplate, a new ResourceClaim will be generated for every
entry in Pod spec `resourceClaims` section. In this case every generated claim will have separate Gaudi
accelerators allocated the same way that different existing ResourceClaims would.

The only difference between a standalone ResourceClaim, and one generated from a template, is that generated
Resource Claims are deleted when the Pod is deleted, while the standalone Resource Claims stay
and needs explicit deletion.

Example of Pod with generated Resource Claim:
```YAML
apiVersion: resource.k8s.io/v1beta1
kind: ResourceClaimTemplate
metadata:
  name: claim1
spec:
  spec:
    devices:
      requests:
      - name: gaudi
        deviceClassName: gaudi.intel.com
---
apiVersion: v1
kind: Pod
metadata:
  name: test-inline-claim
spec:
  restartPolicy: Never
  containers:
  - name: with-resource
    image: registry.k8s.io/e2e-test-images/busybox:1.29-2
    command: ["sh", "-c", "ls -la /dev/accel/ && sleep 60"]
    resources:
      claims:
      - name: resource
  resourceClaims:
  - name: resource
    resourceClaimTemplateName: claim1
```

#### Customizing resources request

ResourceClaim device request can be customized. `count` field specifies how many devices are needed.
'selectors' is a [CEL](https://github.com/google/cel-spec) filter to narrow down allocation to
desired devices. For instance, device model should be Gaudi2. The attributes of the device can be
used in CEL.

Example of Resource Claim requesting 2 Gaudi2 accelerators:
```yaml
apiVersion: resource.k8s.io/v1beta1
kind: ResourceClaim
metadata:
  name: claim1
spec:
  devices:
    requests:
    - name: gaudi
      deviceClassName: gaudi.intel.com
      count: 2
      selectors:
      - cel:
          expression: device.attributes["gaudi.intel.com"].model == 'Gaudi2'
```

## Gaudi monitor deployment

Gaudi monitor deployment ResourceClaim must specify `allocationMode: All` and `adminAccess: true` in `requests` (see [Monitor Pod example](../../deployments/gaudi/examples/monitor-pod-inline.yaml).

Unlike with normal Gaudi ResourceClaims:
* Monitor deployment gets access to all Gaudi devices on a node
* `adminAccess` ResourceClaim allocations are not counted by scheduler as consumed resource, and can be allocated to workloads

