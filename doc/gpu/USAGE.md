## Requirements

- Kubernetes 1.28+
- Runtime with CDI support: Containerd 1.7+ or CRI-O 1.23+

## Limitations

- Currently max 640 GPUs can be requested for one resource claim (10 PCIe devices,
  each with 64 SR-IOV VFs = 640 VFs on the same node).

## Deploy resource-driver

When deploying custom resource driver image, change `image:` lines in
[resource-driver](../deployments/gpu/resource-driver.yaml) to match its location.

Deploy Custom Resource Definitions, resource-class and finally resource-driver
```bash
kubectl apply -f deployments/gpu/static/crds/
kubectl apply -f deployments/gpu/resource-class.yaml
kubectl apply -f deployments/gpu/resource-driver-namespace.yaml
kubectl apply -f deployments/gpu/resource-defaults.yaml
kubectl apply -f deployments/gpu/resource-driver.yaml
```

## Migrating to latest version

Ensure that the latest Custom Resource Definitions are deployed when deploying the latest version of resource driver:
```bash
kubectl delete -f deployments/gpu/resource-driver.yaml
kubectl apply -f deployments/gpu/static/crds/
kubectl apply -f deployments/gpu/resource-driver.yaml
```

## deployment/ directory contains all required YAMLs:

* `deployments/gpu/static/crds/` - Custom Resource Definitions the GPU resource driver uses.
  - `GpuAllocationState` - main object of communication between controller and kubelet-plugins
  - `GpuClaimParameters` - used in ResourceClaims to specify details about requested HW, e.g.
    quantity, type, minimum requested memory, millicores.
  - `GpuClassParameters` - used in ResourceClass to customize the allocation logic, e.g. `shared` or
     exclusive GPU allocation, or allocation of all devices at once for `monitor`ing purposes.

* `deployments/gpu/resource-class.yaml` - pre-defined ResourceClasses that ResourceClaims can refer to.
* `deployments/gpu/resource-driver-namespace.yaml` - Kubernetes namespace for GPU Resource Driver.
* `deployments/gpu/resource-defaults.yaml` - ConfigMap allowing customizing otherwise hardcoded default values.
* `deployments/gpu/resource-driver.yaml` - actual resource driver with service account and RBAC policy
  - controller Deployment - controller of the GPU resource driver make decisions on what GPU or
    SR-IOV VF should be allocated to a particular ResourceClaim based on the GpuClaimParameters and
    `allocatableDevices` of particular Kubernetes node
  - kubelet-plugin DaemonSet - node-agent, it performs three functions:
    1) supported hardware discovery on Kubernetes cluster node and it's announcement to the
      `GpuAllocationState` that is specific to the node.
    2) preparation of the hardware allocated to the ResourceClaims for the Pod that is being started on the node.
    3) unpreparation of the hardware allocated to the ResourceClaims for the Pod that is being started on the node


## Deployment validation

After the controller and kubelet-plugin pods are ready, check one of GpuAllocationState Custom
Resource object's contents. There should be one object for each cluster node with relevant GPUs,
describing the available GPUs:
```bash
$ kubectl get -n intel-gpu-resource-driver gpuallocationstates.gpu.resource.intel.com
```

Or shorter version
```bash
$ kubectl get -n intel-gpu-resource-driver gas

NAME       AGE
icx-cp-1   2d19h
icx-cp-3   2d19h
spr-es-1   2d19h
```

Example contents of the GpuAllocationState CR object:
```bash
$ kubectl get -n intel-gpu-resource-driver gas/icx-cp-3 -o yaml
apiVersion: gpu.resource.intel.com/v1alpha2
kind: GpuAllocationState
metadata:
  creationTimestamp: "2023-03-28T12:36:21Z"
  generation: 30
  name: icx-cp-3
  namespace: default
  ownerReferences:
  - apiVersion: v1
    kind: Node
    name: icx-cp-3
    uid: 3ae742c7-6654-4c7a-9d3d-5584b64bb014
  resourceVersion: "144509816"
  uid: 8487756c-e2fe-4a53-8cfd-6f2a3c583586
spec:
  allocatable:
    0000:b3:00.0-0x56c0:
      ecc: true
      maxvfs: 16
      memory: 14248
      millicores: 1000
      model: "0x56c0"
      parentuid: ""
      type: gpu
      uid: 0000:93:00.0-0x56c0
      vfindex: 0
status: Ready
```

## Deploying test pod to verify GPU resource-driver works

```bash
$ kubectl apply -f deployments/gpu/delayed/inline/pod-inline.yaml
gpuclaimparameters.gpu.resource.intel.com/inline-claim-parameters created
resourceclaimtemplate.resource.k8s.io/test-inline-claim-template created
pod/test-inline-claim created
```

When the Pod gets into Running state, check that GPU was assigned to it:
```bash
$ kubectl logs test-inline-claim -c with-resource
drwxr-xr-x    2 root     root            80 Nov 28 13:35 .
drwxr-xr-x    6 root     root           380 Nov 28 13:35 ..
crw-rw-rw-    1 root     root      226,   0 Nov 28 13:35 card0
crw-rw-rw-    1 root     root      226, 128 Nov 28 13:35 renderD128
```

## Requesting resources

With Dynamic Resource Allocation the resources are requested in a similar way to how the persistent
storage is requested. The [Resource Claim](#resourceclaim) is an analog of Persistent Volume Claim,
and it is used for scheduling Pods to nodes based on the GPU resource availability. It provide access
to GPU devices in Pod's containers.

### Basic use case: Pod needs a GPU

The simplest way to start using Intel GPU Resource Driver is to create a ResourceClaim, and add it
to Pod spec to be used in container. The Intel GPU Resource Driver will take care of allocating
suitable GPU resource to the Resource Claim when Kubernetes Scheduler is scheduling the Pod.

```yaml
apiVersion: resource.k8s.io/v1alpha2
kind: ResourceClaim
metadata:
  name: gpu-claim-1
spec:
  resourceClassName: intel-gpu
---
apiVersion: v1
kind: Pod
metadata:
  name: test-pod-1
spec:
  restartPolicy: Never
  containers:
  - name: with-resource
    image: registry.k8s.io/e2e-test-images/busybox:1.29-2
    command: ["sh", "-c", "ls -la /dev/dri/ && sleep 30"]
    resources:
      claims:
      - name: resource
  resourceClaims:
  - name: resource
    source:
      resourceClaimName: gpu-claim-1
```

Two important sections in above Pod spec are:
- `resourceClaims` - all ResourceClaims that the Pod will use, need to be here
- `claims` - is the new section in container's `resources` section. If the container
  needs to use a ResourceClaim - the Claim needs to be listed in this section for
  that container.

In this example:
- the ResourceClaim `gpu-claim-1` is created;
- the Pod `test-pod-1` declares that:
  - it will use Resource Claim `gpu-claim-1`;
  - the container named `with-resource` will be using the resources allocated to the Resource Claim
    `gpu-claim-1`.

### Resource Classes

Intel GPU Resource Driver provides three resource classes:
- `intel-gpu`: intended for exclusive GPU requests.
- `intel-gpu-shared`: intended for workloads that do not need exclusive GPU access and can share the
  GPU with other workloads.
- [intel-gpu-monitor](#gpu-monitor-deployment): intended for deployments of GPU monitoring software.


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
apiVersion: resource.k8s.io/v1alpha2
kind: ResourceClaimTemplate
metadata:
  name: claim-template-1
  namespace: default
spec:
  spec:
    resourceClassName: intel-gpu
---
apiVersion: v1
kind: Pod
metadata:
  name: test-pod-1
spec:
  restartPolicy: Never
  containers:
  - name: with-resource
    image: registry.k8s.io/e2e-test-images/busybox:1.29-2
    command: ["sh", "-c", "ls -la /dev/dri/ && sleep 5"]
    resources:
      claims:
      - name: resource1
  resourceClaims:
  - name: resource1
    source:
      resourceClaimTemplateName: claim-template-1
```

#### Customizing resources request

Resource Claim references Resource Class in `resourceClassName` field, and based on it the
Intel GPU Resource Driver decides what GPUs should be allocated to the Resource Claim. For example,
shared GPU allocation or exclusive.

ResourceClaim can also reference a GpuClaimParameters in `parametersRef` to customize the GPU
request. For instance quantity and / or qualities of the GPU.

Example of Resource Claim requesting 2 GPUs with at least 4096 MiB of local memory each:
```yaml
apiVersion: gpu.resource.intel.com/v1alpha2
kind: GpuClaimParameters
metadata:
  name: claim-params-1
  namespace: default
spec:
  count: 2
  memory: 4096
  type: "gpu"
---
apiVersion: resource.k8s.io/v1alpha2
kind: ResourceClaim
metadata:
  name: claim-1
spec:
  resourceClassName: intel-gpu
  parametersRef:
    apiGroup: gpu.resource.intel.com/v1alpha2
    kind: GpuClaimParameters
    name: claim-params-1
```

See details in [GpuClaimParameters](#gpuclaimparameters) object reference

### GPU sharing

GPUs in Kubernetes cluster can be shared between Pods in two main ways with GPU resource driver:
- sharing the same Resource Claim with allocated GPU resources between pods
- sharing the same GPU between Resource Claims (fractionalization)

Quick reference:

|                                         | ResourceClassParameters.Shared=true | ResourceClassParameters.Shared=false |
|                :---:                    |                :---:                |                :---:                 |
| ResourceClaimParameters.Shareable=true  |   Shared between pods and claims    |         Shared between pods          |
| ResourceClaimParameters.Shareable=false |        Shared between claims        |               Exclusive              |


#### Within the same Pod

One ResourceClaim can be used by any number of containers within one Pod, regardless of ResourceClass
or ResourceClaim parameters. Just like Persistent Volume Claim, this allows using the same GPU resource
allocated to Claim in multiple containers.

Note: use templated ResourceClaim when each Pod replica should have its own GPU allocation.

#### Between Pods using same ResourceClaim

By default, ResourceClaim can be used only in one Pod at a time. If the ResourceClaimParameters
has `shareable` field set to `true`, then up to 32 Pods can use it at the same time. Keep in
mind, this may degrade workload service or user experience.

#### Between ResourceClaims

Fractionalization of GPU can be done in two ways:
- non-guaranteed, when `type` field in `GpuClaimParameters` is set to `gpu`
- guaranteed, when using SR-IOV, by setting `type` field in `GpuClaimParameters` value to `vf`

In both cases, if Resource Claim requested fraction of GPU, and GPU Resource Driver allocated GPU A to it,
the same GPU A can be allocated to another Resource Claim if leftover GPU resources satisfy another
ResourceClaim request.

##### `gpu` device type fractionalization

To limit number of ResourceClaims that can share the GPU, `memory` and `millicores` fields of
`GpuClaimParameters` can be used. While neither minimum, nor maximum limits are guaranteed to be
available or restricted for a workload, they provide a simple Kubernetes level accounting that will stop
allocating same GPU to the new `ResourceClaims` once either of these resources is exhausted.

For this to work, all ResourceClaims must specify these fields. If a ResourceClaim does not specify
`millicores` - the minimum amount of 1 is enforced.

Sharing should be avoided for latency critical workloads, and/or if other workloads
have not been validated to keep their resource usage within their requested amount (causing other
workloads to potentially slow down, or even fail).

##### SR-IOV Virtual Functions

Virtual Functions (VFs) can be used to split GPU to parts that have a fixed portion of its resources.
This can be used both to protect workload(s) running in given VF, from workloads running in other VFs
of the same GPU (guaranteeing resources for trusted workloads), and vice versa (limiting resources
available to untrusted workloads).

Some operations are available only through PF (Physical Function i.e. whole GPU) device, but those are
needed only for monitoring and administrating the GPU, not for running normal GPU workloads.

To request a VF for Pod usage, ResourceClaimParameters field `type` has to be set to `vf` value.

When GPU is split into Virtual Functions, GPU Resource Driver does not allocate that GPU to any workload
until the VFs are unprovisioned. Instead, new devices representing SR-IOV VFs are being allocated
as long as any workload needs them.

When a Resource Claim requests a VF, the GPU resource driver will try to find existing unallocated
SR-IOV VFs suitable for request. If one or more suitable VF devices are found, the VF with least GPU
resources is allocated to the Resource Claim.

If there are no existing VFs available in the cluster, the Resource Driver will attempt to find a GPU
that can have VFs but does not have any at the moment, and also is not allocated to any ResourceClaim.
If found, this GPU is then used to provision VFs suitable to (some or all of) the requested VFs.
After any number of VFs are provisioned on a GPU, it's no longer possible to add more VFs to it until
all VFs are removed from this GPU.

If that allocation succeeds with sufficient amount of planned VFs, Pod scheduling will then proceed
to node, where kubelet will request resource preparation and GPU Resource driver will provision
SR-IOV VFs.

After the last Pod using a ResourceClaim with a VF device type comes to completion, resource
driver checks if any VFs on the same device are being used by any other ResourceClaims, and if
- no VFs are in use, then all of the VFs are removed from that GPU and it is treated
  again as a plain GPU device
- any VF is in use from the same GPU, then all the VFs on that GPU stay until
  all of them are unused.


### Exclusive GPU usage

Setting ResourceClassParameters `shared` field to false, allows exclusive usage
of GPU in the ResourceClaim. If the ResourceClaimParameters has `shareable` field set to `false` or
not set at all (absent, not specified) - the GPU is then used exclusively by the only Pod that is
allowed to use ResourceClaim at a time.

## GPU monitor deployment

GPU monitor deployment ResourceClaim must specify a ResourceClass with GpuClassParams having `monitor` field set to `true` (see [Resource Class specs](../deployments/gpu/resource-class.yaml).

Unlike with normal GPU ResourceClaims:
* Monitor deployment gets access to all GPU devices on a node
* Resource Claim parameters are ignored
* Only (default) delayed claims are supported, not immediate ones
* No support for SR-IOV VF provisioning
  - Monitor pods need to be manually restarted when GPU devices change

With [Intel GPU device plugin](https://github.com/intel/intel-device-plugins-for-kubernetes#gpu-device-plugin)
`i915_monitor` resource, there could be only single monitor per node.
Setting GpuClassParameters `shared` field to `false` does the same.

## Preferred allocation policy

GPU allocation policy within a node, and within the GPUs that have enough resources to fulfill the GpuClaimParameters,
can be either none (the default), packed or balanced. Packed allocation policy will select the GPU which has the least
(yet still sufficient) amount of resources. Balanced allocation policy will select the GPU which has the most amount
of resources. Default (none) policy selects any GPU with sufficient amount of resources.

The allocation policy is set via the controller command line flag `--preferred-allocation-policy`. Its
valid values are one of `none`, `packed` and `balanced`.
The name of the resource used for enforcing the allocation policy is set via the controller command line flag
`--allocation-policy-resource`. Its valid values are one of `memory` and `millicores`. The default is `memory`.

## SR-IOV VF profiles

On the GPUs where [SR-IOV profiles](../pkg/sriov/sriov_profiles.go#L32) are supported, the GPU resources
can be split evenly between all VFs (with `fairShare` VF profile), or unevenly, optimizing the usage of GPU.
Based on requested memory and millicores, most suitable of recommended SR-IOV VF profiles will be selected for VF.

The profiles are named after model and relative slice of GPU resources that the VF has. For instance
VF with profile `GPU1_m4` would have 1/4th of the local memory and computational capacity of the
`GPU1` model GPU.

### Leftovers utilization

When requested SR-IOV VFs would need to be provisioned on a GPU, and would not utilize all the local
memory the GPU has, the leftover resources are provisioned as additional, unallocated VF(s).

If all VFs to be provisioned on a GPU have the same profile - additional VFs will be of the same profile
until the maximum number of VFs with such profile is reached. For instance, for `XXX_m5` profile -
five such VFs can be provisioned on a GPU, and if ResourceClaim has requested 2 VFs that were assigned
`XXX_m5` profile, that means 2/5th of GPU memory and computational capacity would be idling unused,
so 3 additional VFs with the same profile are added as unallocated, and available for allocation.

If the VFs to be provisioned on a GPU are of different profile - the leftover resources are divided
into VFs with biggest-to-smallest approach. First, the VFs with the biggest fitting profile is added,
and if there are still leftover resources - next smaller profile is considered, and so forth until even
VF with smallest profile wouldn't have enough resources.

#### Default VF profiles

Default memory amount that should be allocated to VFs when leftover resources are utilized can be
customized per GPU model in [ConfigMap with defaults](../deployments/gpu/resource-defaults.yaml).
See `vf-memory.config` section. The amount per model is GPU local memory in MiB, based on which
profile will be selected.

## Objects reference

### GpuClaimParameters

Intel GPU Resource Driver `GpuClaimParameters` format is following:
```yaml
apiVersion: gpu.resource.intel.com/v1alpha2
kind: GpuClaimParameters
metadata:
  name: gpu-claim-params-1
  namespace: default
spec:
  count: 1
  memory: 512
  millicores: 500
  type: "gpu"
  shareable: true
```

`count`: Required. Quantity of resource requested [1-640].

`type`: Required. Type of resource. Possible values are `gpu`, `vf`, where `gpu` is a plain GPU and `vf` is a SR-IOV Virtual Function. For `monitor`-enabled ResourceClass, only `gpu` value is supported.

`memory`: Optional, default: 0. Amount of local memory per requested `gpu` or `vf`, in MiB. For `gpu` type of
resource the amount of memory is not guaranteed. For `vf` type of resource `memory` is used for
choosing [SR-IOV VF profile](#sr-iov-vf-profiles).

`millicores`: Optional, default: 1. Relative amount of computational capacity of GPU (1000 = whole GPU).
For `gpu` device type, this field can be used to limit the number of ResourceClaims that the GPU can be allocated to,
see [GPU sharing](#gpu-sharing). For `vf` device type, `millicores` resource is used for choosing a
[SR-IOV VF profile](#sr-iov-vf-profiles) with (at least) the requested portion of the GPU time.

`shareable`: Optional, default: false. If `true`, indicates that the ResourceClaim can be used by
multiple (up to 32) Pods at the same time. If `false`, only one Pod is allowed to use the ResourceClaim at a
time, and all the other Pods that need to use the ResourceClaim will be in `Pending` state until the ResourceClaim
is not used by any Pod. Pods are not guaranteed to get access to the ResourceClaim in request order.

See [deployments/](../deployments/gpu/) directory for use cases examples.

### GpuClassParameters

Intel GPU Resource Driver `GpuClasParameters` format is following:

```YAML
apiVersion: gpu.resource.intel.com/v1alpha2
kind: GpuClassParameters
metadata:
  name: gpu-class-params-1
spec:
  monitor: false
  shared: false
```

`monitor`: Optional, default: false. Indicates whether the ResourceClass is intended for monitoring
workloads.

`shared`: Required. If `true`, indicates that GPUs are not allocated exclusively to given ResourceClaim,
but can be allocated to several ResourceClaims at once, when ResourceClaimParameters request
is matching the remaining GPU resource. If `false`, indicates that GPUs are allocated to the
ResourceClaim exclusively, and cannot be allocated to any other ResourceClaim. If GPU is already
allocated to some ResourceClaim, it is not anymore eligible for exclusive allocation.
