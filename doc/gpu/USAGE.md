## Requirements

- Kubernetes v1.34+, and  optionally [some cluster parameters](../../hack/clusterconfig.yaml) for advanced features, see [Cluster Setup](../CLUSTER_SETUP.md)
- Container runtime needs to support CDI:
  - CRI-O v1.23.0 or newer
  - Containerd v1.7 or newer with CDI enabled
- Optional (recommended): [XPUM Daemon](https://github.com/intel/xpumanager/tree/v2.x/xpumd) (`xpumd`) is required for health monitoring and non-privileged discovery mode. It  can be installed ([Helm chart](https://github.com/intel/xpumanager/blob/v2.x/xpumd/charts/xpumd/README.md)) after Intel GPU DRA driver.

## Deploy resource-driver

### Discovery modes

There are three modes of device details discovery:
- **non-privileged, with xpumd (Recommended)**: the DRA driver publishes the `ResourceSlice` with zero `memory` attribute initially, later when the xpumd is deployed and reports device details (e.g. GPU memory size) - the `ResourceSlice` is updated with populated device attributes.
- **privileged, without xpumd**: the DRA driver queries the device details (e.g. memory size) directly from the GPU kernel driver, and announces them in `ResourceSlice` with all the available attributes populated. The xpumd is not necessary in this case.
- **non-privileged, without xpumd**: the DRA driver has no source of device details (e.g. memory), the devices are announced in `ResourceSlice` as-is, without details - it is still possible to use the devices, but selectors in `ResourceClaim` should take into account missing attributes values.

### Helm Chart

The [Intel GPU Resource Driver Helm Chart](../../charts/intel-gpu-resource-driver) is published
as a package to GitHub OCI registry, and can be installed directly with Helm.

> [!NOTE]
> Starting from v0.10.0, [XPUM Daemon](https://github.com/intel/xpumanager/tree/v2.x/xpumd) is used for health monitoring and devices' details discovery,
> and is enabled by default. It is not currently part of this chart, and needs to be installed separaterly.
> XPUM Daemon can be installed either before or after the Intel GPU Resource Driver.

```console
helm install \
    --namespace "intel-gpu-resource-driver" \
    --create-namespace \
    intel-gpu-resource-driver oci://ghcr.io/intel/intel-resource-drivers-for-kubernetes/intel-gpu-resource-driver-chart
```

See [details](../../charts/intel-gpu-resource-driver/README.md) in the chart directory.

### From sources

```bash
kubectl apply -k 'https://github.com/intel/intel-resource-drivers-for-kubernetes/deployments/gpu?ref=<RELEASE_VERSION>'
```
Example RELEASE_VERSION: `gpu-v0.10.1`.

By default, the kubelet-plugin is deployed on _all_ nodes in the cluster, as no nodeSelector is defined.
To restrict the deployment to GPU-enabled nodes, follow these steps:

1. Install Node Feature Discovery (NFD):

Follow [Node Feature Discovery](https://github.com/kubernetes-sigs/node-feature-discovery) documentation to install and configure NFD in your cluster.

```bash
kubectl apply -k "https://github.com/kubernetes-sigs/node-feature-discovery/deployment/overlays/default?ref=v0.18.3"
```

2. Deploy the DRA driver with new NFD Rules for Intel GPUs:

```bash
kubectl apply -k 'https://github.com/intel/intel-resource-drivers-for-kubernetes/deployments/gpu/overlays/nfd_labeled_nodes?ref=<RELEASE_VERSION>'
```

After NFD is installed and running, the nodes with GPUs will be labeled with:
```bash
intel.feature.node.kubernetes.io/gpu: "true"
```

The GPU DRA driver will be deployed to nodes that have such labels.

When deploying custom resource driver image, change `image:` lines in
[resource-driver](../../deployments/gpu/base/resource-driver.yaml) to match its location.

### deployment/ directory contains all required YAMLs:

* `deployments/gpu/base/device-class.yaml` - pre-defined ResourceClasses that ResourceClaims can refer to.
* `deployments/gpu/base/namespace.yaml` - Kubernetes namespace for GPU Resource Driver.
* `deployments/gpu/base/resource-driver.yaml` - actual resource driver with service account and RBAC policy
  - kubelet-plugin DaemonSet - node-agent, it performs three functions:
    1) supported hardware discovery on Kubernetes cluster node and it's announcement as a ResourceSlice
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
<details>

```bash
$ kubectl get resourceslice/rpl-s-gpu.intel.com-mbr6p -o yaml
apiVersion: resource.k8s.io/v1
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
  - attributes:
      driver:
        string: i915
      family:
        string: Unknown
      health:
        string: Healthy
      model:
        string: Unknown
      pciAddress:
        string: "0000:00:02.0"
      pciId:
        string: "0x7d67"
      pciRoot:
        string: "00"
      resource.kubernetes.io/pciBusID:
        string: "0000:00:02.0"
      resource.kubernetes.io/pcieRoot:
        string: pci0000:00
      sriov:
        bool: true
    capacity:
      memory:
        value: "0"
      millicores:
        value: 1k
    name: 0000-00-02-0-0x7d67
  - attributes:
      driver:
        string: xe
      family:
        string: Unknown
      health:
        string: Healthy
      model:
        string: Unknown
      pciAddress:
        string: "0000:04:00.0"
      pciId:
        string: "0xe211"
      pciRoot:
        string: "00"
      resource.kubernetes.io/pciBusID:
        string: "0000:04:00.0"
      resource.kubernetes.io/pcieRoot:
        string: pci0000:00
      sriov:
        bool: true
    capacity:
      memory:
        value: 24480Mi
      millicores:
        value: 1k
    name: 0000-04-00-0-0xe211
  driver: gpu.intel.com
  nodeName: rpl-s
  pool:
    generation: 0
    name: rpl-s
    resourceSliceCount: 1
```

</details>

## Deploying test pod to verify GPU resource-driver works

```bash
$ kubectl apply -f 'https://raw.githubusercontent.com/intel/intel-resource-drivers-for-kubernetes/refs/heads/main/deployments/gpu/examples/pod-inline-gpu.yaml'
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

# Notable changes

K8s v1.34 resource.k8s.io/v1 API changed ResourceClaim syntax compared to resource.k8s.io/v1beta1.
In v1, device request must be specified either as `exactly`, or as a priority-ordered list using `firstAvailable`
request type.  Using latter requires `DRAPrioritizedList` [feature gate](../CLUSTER_SETUP.md#useful-and-required-featuregates) to be enabled.

`exactly`-specified request is allocated by the kube-scheduler as-is. The`firstAvailable` list of requests
is processed by the scheduler sequentially until the currently processed request is possible to allocate.

## v0.10.0

- `pciRoot` attribute of DRA device is deprecated and will eventually be removed (current target is v1.0)
- health monitoring change: in-container `xpu-smi` is replaced by GRPC-based communications with xpumd: to get operational health monitoring, deploy xpumd into the cluster: https://github.com/intel/xpumanager/blob/v2.x/xpumd/charts/xpumd/README.md
- hardware discovery change: privileged mode is no longer required, xpumd is now the default source of detailed HW information (e.g. GPU local memory amount). Alternatively, if health monitoring is not required, privileged mode can be used to allow GPU DRA driver query HW details directly from the devices. If neither health-monitoring is enabled, nor privileged mode - the discovered devices are announced to the cluster without the HW details (e.g. memory)

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
apiVersion: resource.k8s.io/v1
kind: ResourceClaim
metadata:
  name: claim1
spec:
  devices:
    requests:
    - name: gpu
      exactly:
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

### Requesting DRA-managed accelerators through extended resources

Starting K8s v1.34 it is possible to request resources managed by DRA driver without creating a
ResourceClaim, through `resources` section of workload definition. This requires
`DRAExtendedResources` [feature gate](../CLUSTER_SETUP.md#useful-and-required-featuregates) to be
 enabled.

If this feature is enabled in the cluster, ensure that `enableDRAExtendedResources` is set in the Helm
chart values during the installation, or that
[respective line](../../deployments/gpu/base/device-class.yaml#L11) is uncommented in yaml when
installing from the from sources.

To check if this feature is enabled and successfully activated, check if DeviceClass has
`Extended Resource Name`:
```shell
kubectl describe deviceclass/gpu.intel.com
```

See [workload example](../../deployments/gpu/examples/deployment-extended-resources.yaml).

Starting K8s v1.35, it is also possible to request DRA driver-managed resource implicitly,
based on the DRA driver name, even if the latter lacks `extendedResourceName` setting.
See [example](../../deployments/gpu/examples/deployment-extended-resources-implicit.yaml)

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
apiVersion: resource.k8s.io/v1
kind: ResourceClaimTemplate
metadata:
  name: claim1
spec:
  spec:
    devices:
      requests:
      - name: gpu
        exactly:
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
'selectors' is a [CEL](https://github.com/google/cel-spec) filter to narrow down allocation to desired GPUs. For instance, amount of
memory should be at least 16Gi. The attributes and capacity properties of the GPU can be used in CEL.

Example of Resource Claim requesting 2 GPUs with at least 16 Gi of local memory each:
```yaml
apiVersion: resource.k8s.io/v1
kind: ResourceClaimTemplate
metadata:
  name: claim1
spec:
  spec:
    devices:
      requests:
      - name: gpu
        exactly:
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

## Health monitoring support

Starting from v0.10.0 GPU DRA driver supports health monitoring with `-m` command-line parameter
(disabled in default deployments/ configuration, enabled by default in the [Helm chart](../../charts/intel-gpu-resource-driver/))
through [XPUM Daemon](https://github.com/intel/xpumanager/tree/v2.x/xpumd). When it deems GPU accelerator as unhealthy,
`health` field for corresponding device in `ResourceSlice` is set as `false`. Additionally, if `DRADeviceTaints`
feature gate is enabled in the cluster, health category [DeviceTaint](https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/#device-taints-and-tolerations) will be added to the unhealthy device's entry in `ResourceSlice`, preventing
workload Pods from using such GPU unless they have toleration specified in the `ResourceClaim`.

This feature was first introduced in K8s v1.33, it allows scheduler to handle ResourceSlice devices
similarly to how K8s Node Taints and Tolerations allow. Cluster admins can also create standalone
DeviceTaintRule to prevent workloads being scheduled and / or executed on a particular GPU.

## Known issues

- In K8s v1.34.0 - v1.34.1 the kubelet might lose GRPC connection to a DRA driver after 30 minutes
  of inactivity. To prevent this situation, enable `ResourceHealthStatus` feature-gate in Kubelet
  and api-server.
- In K8s v1.34-0 - v1.34.1 the Device Taint Eviction Controller can evict a Pod with a
  DeviceTaintToleration immediately after successful scheduling. Solution is to upgrade the cluster
  to a newer K8s version.