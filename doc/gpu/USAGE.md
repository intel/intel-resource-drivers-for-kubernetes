## Requirements

- Kubernetes 1.31+, with `DynamicResourceAllocation` feature-flag enabled, and [other cluster parameters](../../hack/clusterconfig.yaml)
- Container runtime needs to support CDI:
  - CRI-O v1.23.0 or newer
  - Containerd v1.7 or newer

### Enable CDI in Containerd

Containerd has CDI enabled by default since version 2.0. For older versions (1.7 and above)
CDI has to be enabled in Containerd config by enabling `enable_cdi` and `cdi_specs_dir`.
Example `/etc/containerd/config.toml`:
```
version = 2
[plugins]
  [plugins."io.containerd.grpc.v1.cri"]
    enable_cdi = true
    cdi_specs_dir = ["/etc/cdi", "/var/run/cdi"]
```

## Limitations

- Currently max 640 GPUs can be requested for one resource claim (10 PCIe devices,
  each with 64 SR-IOV VFs = 640 VFs on the same node).
- v0.6.0 only supports K8s v1.31 which does not have partitionable devices support,
  therefore this release does not support dynamic GPU SR-IOV configuration.
- v0.6.0 does not support classic DRA and only relies on Structured Parameters DRA
- v0.6.0 drops Alertmanager web-hook used for (experimental) GPU health management support

## Deploy resource-driver

```bash
kubectl apply -f deployments/gpu/device-class.yaml
kubectl apply -f deployments/gpu/resource-driver-namespace.yaml
kubectl apply -f deployments/gpu/resource-driver.yaml
```

By default the kubelet-plugin will be deployed on _all_ nodes in the cluster, there is no nodeSelector.

One could be added, for example, based on [NFD provided device labels](https://kubernetes-sigs.github.io/node-feature-discovery/stable/usage/features.html) indicating PCI devices presence

When deploying custom resource driver image, change `image:` lines in
[resource-driver](../../deployments/gpu/resource-driver.yaml) to match its location.

## deployment/ directory contains all required YAMLs:

* `deployments/gpu/device-class.yaml` - pre-defined ResourceClasses that ResourceClaims can refer to.
* `deployments/gpu/resource-driver-namespace.yaml` - Kubernetes namespace for GPU Resource Driver.
* `deployments/gpu/resource-driver.yaml` - actual resource driver with service account and RBAC policy
  - kubelet-plugin DaemonSet - node-agent, it performs three functions:
    1) supported hardware discovery on Kubernetes cluster node and its announcement as a ResourceSlice
    2) preparation of the hardware allocated to the ResourceClaims for the Pod that is being started on the node.
    3) unpreparation of the hardware allocated to the ResourceClaims for the Pod that is being started on the node

## Deployment validation

After kubelet-plugin pods are ready, check ResourceSlice objects and their contents:
```bash
$ kubectl get resourceslices
NAME                          NODE    DRIVER            POOL    AGE
rpl-s-gpu.intel.com-mbr6p     rpl-s   gpu.intel.com     rpl-s   30s
```

Example contents of the ResourceSlice object:
```bash
$ kubectl get resourceslice/rpl-s-gpu.intel.com-mbr6p -o yaml
apiVersion: resource.k8s.io/v1alpha3
kind: ResourceSlice
metadata:
  creationTimestamp: "2024-09-27T09:11:24Z"
  generateName: rpl-s-gpu.intel.com-
  generation: 1
  name: rpl-s-gpu.intel.com-mbr6p
  ownerReferences:
  - apiVersion: v1
    controller: true
    kind: Node
    name: rpl-s
    uid: 0894e000-e7a3-49ad-8749-04b27be61c03
  resourceVersion: "2479360"
  uid: 305a8e03-fe9b-44ea-831e-01ce70edb1a7
spec:
  devices:
  - basic:
      attributes:
        family:
          string: Arc
        model:
          string: A770
      capacity:
        memory: 16288Mi
        millicores: 1k
    name: 0000-03-00-0-0x56a0
  driver: gpu.intel.com
  nodeName: rpl-s
  pool:
    generation: 0
    name: rpl-s
    resourceSliceCount: 1
```

## Deploying test pod to verify GPU resource-driver works

```bash
$ kubectl apply -f deployments/gpu/examples/pod-inline-gpu.yaml
resourceclaimtemplate.resource.k8s.io/claim1 created
pod/test-inline-claim created
```

When the Pod gets into Running state, check that GPU was assigned to it:
```bash
$ kubectl logs pod/test-inline-claim
Defaulted container "with-resource" out of: with-resource, without-resource
total 0
drwxr-xr-x    2 root     root            80 Sep 27 09:17 .
drwxr-xr-x    6 root     root           380 Sep 27 09:17 ..
crw-rw-rw-    1 root     root      226,   0 Sep 27 09:17 card0
crw-rw-rw-    1 root     root      226, 128 Sep 27 09:17 renderD128

```

## Requesting resources

With Dynamic Resource Allocation the resources are requested in a similar way to how the persistent
storage is requested. The ResourceClaim is an analog of Persistent Volume Claim, and it is used for
scheduling Pods to nodes based on the GPU resource availability. It provide access to GPU devices
in Pod's containers.

### Basic use case: Pod needs a GPU

The simplest way to start using Intel GPU resource driver is to create a ResourceClaim, and add it
to Pod spec to be used in container. The scheduler will allocate suitable GPU resource from respective
ResourceSlice that was published by the Intel GPU resource driver.

```yaml
apiVersion: resource.k8s.io/v1alpha3
kind: ResourceClaim
metadata:
  name: claim1
spec:
  devices:
    requests:
    - name: gpu
      deviceClassName: gpu.intel.com
---
apiVersion: v1
kind: Pod
metadata:
  name: test-claim
spec:
  restartPolicy: Never
  containers:
  - name: with-resource
    image: registry.k8s.io/e2e-test-images/busybox:1.29-2
    command: ["sh", "-c", "ls -la /dev/dri/ && sleep 60"]
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
- the Pod `test-claim` declares that:
  - it will use Resource Claim `claim1`;
  - the container named `with-resource` will be using the resources allocated to the Resource Claim
    `claim1`.

### Device Class

Intel GPU resource driver provides following device class:
- `gpu.intel.com`

### Advanced use cases

#### Creation of Resource Claim

There are two ways to create a Resource Claim:
- creating it explicitly as a `ResourceClaim` object
- letting K8s generate Resource Claim from existing `ResourceClaimTemplate` when the Pod is created

When referencing ResourceClaim in Pod spec - the claim has to exist.

When Pod spec references a ResourceClaimTemplate, a new ResourceClaim will be generated for every
entry in Pod spec `resourceClaims` section. In this case every generated claim will have separate GPU
resources allocated the same way that different existing ResourceClaims would.

The only difference between a standalone ResourceClaim, and one generated from a template, is that generated
Resource Claims are deleted when the Pod is deleted, while the standalone Resource Claims stay
and needs explicit deletion.

Example of Pod with generated Resource Claim:
```YAML
apiVersion: resource.k8s.io/v1alpha3
kind: ResourceClaimTemplate
metadata:
  name: claim1
spec:
  spec:
    devices:
      requests:
      - name: gpu
        deviceClassName: gpu.intel.com
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
    command: ["sh", "-c", "ls -la /dev/dri/ && sleep 60"]
    resources:
      claims:
      - name: resource
  resourceClaims:
  - name: resource
    resourceClaimTemplateName: claim1
```

#### Customizing resources request

ResourceClaim device request can be customized. `count` field specifies how many devices are needed.
'selectors' is a [CEL](https://github.com/google/cel-spec) filter to narrow down allocation to desired GPUs.
The attributes and capacity properties of the GPU can be used in CEL.

Example of Resource Claim requesting 2 GPUs with at least 16 Gi of local memory each:
```yaml
apiVersion: resource.k8s.io/v1alpha3
kind: ResourceClaimTemplate
metadata:
  name: claim1
spec:
  spec:
    devices:
      requests:
      - name: gpu
        deviceClassName: gpu.intel.com
      count: 2
      selectors:
      - cel:
          expression: device.capacity["gpu.intel.com"].memory.compareTo(quantity("16Gi")) >= 0
```

## GPU monitor deployment

GPU monitor deployment ResourceClaim must specify `allocationMode: All` and `adminAccess: true` in `requests` (see [Monitor pod example](../../deployments/gpu/examples/monitor-pod-inline.yaml).

Unlike with normal GPU ResourceClaims:
* Monitor deployment gets access to all GPU devices on a node
* `adminAccess` ResourceClaim allocations are not counted by scheduler as consumed resource, and can be allocated to workloads

### Helm Charts

[Intel GPU Resource Driver Helm Chart](https://github.com/intel/helm-charts/tree/main/charts/intel-gpu-resource-driver) is located in Intel Helm Charts repository.

To add repo:
```
helm repo add intel https://intel.github.io/helm-charts
```

To install Helm Chart:
```
helm install intel-gpu-resource-driver intel/intel-gpu-resource-driver \
--create-namespace --namespace intel-gpu-resource-driver
```
CRDs of the GPU driver are installed as part of the chart first.

If you change the image tag to be used in Helm chart deployment, ensure that the version of the container image is consistent with CRDs and deployment YAMLs - they might change between releases.

Note that Helm does not support _upgrading_ (or deleting) CRDs, only installing them.  Rationale: https://github.com/helm/community/blob/main/hips/hip-0011.md


I.e. making sure that CRDs are upgraded correctly is user responsibility when using Helm.