## Requirements

- Kubernetes v1.34+, and  optionally [some cluster parameters](../../hack/clusterconfig.yaml) for advanced features, see [Cluster Setup](../CLUSTER_SETUP.md)
- Container runtime needs to support CDI:
  - CRI-O v1.23.0 or newer
  - Containerd v1.7 or newer with CDI enabled
- [habanalabs container runtime](https://docs.habana.ai/en/latest/Installation_Guide/Bare_Metal_Fresh_OS.html#set-up-container-runtime) with CDI support

## Deploy resource-driver

### Helm Chart

The [Intel Gaudi Resource Driver Helm Chart](../../charts/intel-gaudi-resource-driver) is published
as a package to GitHub OCI registry, and can be installed directly with Helm.

```console
helm install \
    --namespace "intel-gaudi-resource-driver" \
    --create-namespace \
    intel-gaudi-resource-driver oci://ghcr.io/intel/intel-resource-drivers-for-kubernetes/intel-gaudi-resource-driver-chart
```

See [details](../../charts/intel-gaudi-resource-driver/README.md) in the chart directory.

### From sources

```bash
kubectl apply -k deployments/gaudi/
```

**Note:** Health monitoring functionality (with the -m flag) is enabled by default.
Since this is the only reason for using a `privileged` container, it is recommended to set `privileged: false`
when the functionality is disabled, for security reasons. This will be improved in the future.

By default, the kubelet-plugin is deployed on _all_ nodes in the cluster, as no nodeSelector is defined.
To restrict the deployment to Gaudi-enabled nodes, follow these steps:

1. Install Node Feature Discovery (NFD):

Follow [Node Feature Discovery](https://github.com/kubernetes-sigs/node-feature-discovery) documentation to install and configure NFD in your cluster.

```bash
kubectl apply -k "https://github.com/kubernetes-sigs/node-feature-discovery/deployment/overlays/default?ref=v0.18.3"
```

2. Deploy the Gaudi DRA driver and NFD Rules together:

```bash
kubectl apply -k deployments/gaudi/overlays/nfd_labeled_nodes/
```

After NFD is installed and running, the nodes with Gaudi accelerators will be labeled with:
```bash
intel.feature.node.kubernetes.io/gaudi: "true"
```

The Gaudi DRA driver will be deployed to nodes that have such labels.

When deploying custom-built resource driver image, change `image:` lines in
[resource-driver](../../deployments/gaudi/base/resource-driver.yaml) to match its location.

## `deployment/` directory contains all required YAMLs:

* `deployments/gaudi/base/device-class.yaml` - pre-defined ResourceClasses that ResourceClaims can refer to.
* `deployments/gaudi/base/namespace.yaml` - Kubernetes namespace for Gaudi resource driver.
* `deployments/gaudi/base/resource-driver.yaml` - actual resource driver with service account and RBAC policy
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
<details>

```bash
$ kubectl get resourceslices.resource.k8s.io rpl-s-gaudi.intel.com-x8m4h -o yaml
apiVersion: resource.k8s.io/v1
kind: ResourceSlice
metadata:
  creationTimestamp: "2026-04-15T07:17:43Z"
  generateName: rpl-s-gaudi.intel.com-
  generation: 1
  name: rpl-s-gaudi.intel.com-x8m4h
  ownerReferences:
  - apiVersion: v1
    controller: true
    kind: Node
    name: rpl-s
    uid: 3a243a6b-e6db-4613-94f2-169f938c87ae
  resourceVersion: "6266269"
  uid: d093f601-9ad5-4234-91d8-410733e32784
spec:
  devices:
  - attributes:
      healthy:
        bool: true
      model:
        string: Gaudi2
      pciRoot:
        string: "01"
      resource.kubernetes.io/pcieRoot:
        string: pci0000:01
      serial:
        string: ""
    name: 0000-a0-00-0-0x1020
  - attributes:
      healthy:
        bool: true
      model:
        string: Gaudi2
      pciRoot:
        string: "02"
      resource.kubernetes.io/pcieRoot:
        string: pci0000:02
      serial:
        string: ""
    name: 0000-b0-00-0-0x1020
  driver: gaudi.intel.com
  nodeName: rpl-s
  pool:
    generation: 1
    name: rpl-s
    resourceSliceCount: 1

```

</details>

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
apiVersion: resource.k8s.io/v1
kind: ResourceClaim
metadata:
  name: claim1
spec:
  devices:
    requests:
    - name: gaudi
      exactly:
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

### Requesting DRA-managed accelerators through extended resources

Starting K8s v1.34 it is possible to request resources managed by DRA driver without creating a
ResourceClaim, through `resources` section of workload definition. This requires
`DRAExtendedResources` [feature gate](../CLUSTER_SETUP.md#useful-and-required-featuregates) to be
 enabled.

If this feature is enabled in the cluster, ensure that `enableDRAExtendedResources` is set in the Helm
chart values during the installation, or that
[respective line](../../deployments/gaudi/base/device-class.yaml#L11) is uncommented in yaml when
installing from the from sources.

To check if this feature is enabled and successfully activated, check if DeviceClass has
`Extended Resource Name`:
```shell
kubectl describe deviceclass/gaudi.intel.com
```

See [workload example](../../deployments/gaudi/examples/deployment-extended-resources.yaml).

Starting K8s v1.35, it is also possible to request DRA driver-managed resource implicitly,
based on the DRA driver name, even if the latter lacks `extendedResourceName` setting.
See [example](../../deployments/gaudi/examples/deployment-extended-resources-implicit.yaml)


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
apiVersion: resource.k8s.io/v1
kind: ResourceClaimTemplate
metadata:
  name: claim1
spec:
  spec:
    devices:
      requests:
      - name: gaudi
        exactly:
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
apiVersion: resource.k8s.io/v1
kind: ResourceClaim
metadata:
  name: claim1
spec:
  devices:
    requests:
    - name: gaudi
      deviceClassName: gaudi.intel.com
      exactly:
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

## Health monitoring support

Starting from v0.7.0 Gaudi DRA driver supports health monitoring with `-m` command-line parameter (enabled
in default deployment configuration) through HLML library. When Gaudi accelerator becomes unhealthy, the
DeviceTaintRule is created (to evict current workloads and prevent this device from being allocated), and
respective device's `healthy` field in ResourceSlice is changed to false.

### Backwards compatibility

In K8s v1.32 DeviceClass can be changed to only allow allocation of healthy devices to workloads:
```shell
kubectl edit deviceclass/gaudi.intel.com
```
add one more selector:
```YAML
  - cel:
      expression: device.attributes["gaudi.intel.com"].healthy == true
```
This approach, however, does not allow influencing the Pods that are / were using Gaudi accelerator when its health status changes.

### K8s v1.33+

In K8s v1.33 [DeviceTaintRule](https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/#device-taints-and-tolerations) concept was introduced that allows scheduler to handle ResourceSlice devices similarly to how K8s Node Taints and Tolerations allow.

Starting from v0.7.0 Gaudi DRA driver leverages DeviceTaintRules if Gaudi accelerator health degrades.
DeviceTaintRule created as a result of degraded health has "NoSchedule" effect, which implies "NoExecute"
and results in Pod eviction for devices with degraded health. Workloads that need to access tainted devices
need to have [taint toleration in ResourceClaim](https://kubernetes.io/docs/concepts/scheduling-eviction/dynamic-resource-allocation/#device-taints-and-tolerations).

## Known issues

- In K8s v1.34.0 - v1.34.1 the kubelet might lose GRPC connection to a DRA driver after 30 minutes of inactivity. To prevent this situation, enable `ResourceHealthStatus` feature-gate in Kubelet and api-server.
- In K8s v1.34-0 - v1.34.1 the Device Taint Eviction Controller can evict a Pod with a DeviceTaintToleration immediately after successful scheduling. Solution is to upgrade the cluster to a newer K8s version.