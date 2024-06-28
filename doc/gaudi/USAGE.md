## Requirements

- Kubernetes 1.28+, with `DynamicResourceAllocation` feature-flag enabled, and [other cluster parameters](hack/clusterconfig.yaml)
- [habanalabs container runtime](https://docs.habana.ai/en/latest/Installation_Guide/Bare_Metal_Fresh_OS.html#set-up-container-runtime) with CDI support


## Deploy resource-driver

Deploy CustomResourceDefinitions, ResourceClass and finally ResourceDriver
```bash
kubectl apply -f deployments/gaudi/static/crds/
kubectl apply -f deployments/gaudi/resource-class.yaml
kubectl apply -f deployments/gaudi/resource-driver-namespace.yaml
kubectl apply -f deployments/gaudi/resource-defaults.yaml
kubectl apply -f deployments/gaudi/resource-driver.yaml
```

By default the kubelet-plugin will be deployed on _all_ nodes in the cluster, there is no nodeSelector.

When deploying custom-built resource driver image, change `image:` lines in
[resource-driver](../deployments/gaudi/resource-driver.yaml) to match its location.

## Migrating to latest version

Ensure that the latest Custom Resource Definitions are deployed when deploying the latest version of resource driver:
```bash
kubectl delete -f deployments/gaudi/resource-driver.yaml
kubectl apply -f deployments/gaudi/static/crds/
kubectl apply -f deployments/gaudi/resource-driver.yaml
```

## `deployment/` directory contains all required YAMLs:

* `deployments/gaudi/static/crds/` - Custom Resource Definitions needed for resource driver.
  - `GaudiAllocationState` - main object of communication between controller and kubelet-plugins
  - `GaudiClaimParameters` - used in ResourceClaims to specify details about requested HW, e.g.
    quantity, type, minimum requested memory, millicores.
  - `GaudiClassParameters` - used in ResourceClass to customize the allocation logic, e.g. number of devices, or
    allocation of all devices at once for `monitor`ing purposes.

* `deployments/gaudi/resource-class.yaml` - pre-defined ResourceClasses that ResourceClaims can refer to.
* `deployments/gaudi/resource-driver-namespace.yaml` - Kubernetes namespace for Gaudi resource driver.
* `deployments/gaudi/resource-defaults.yaml` - ConfigMap allowing customizing otherwise hardcoded default values.
* `deployments/gaudi/resource-driver.yaml` - actual resource driver with service account and RBAC policy
  - controller Deployment - controller of the Gaudi resource driver makes decisions on which Gaudi accelerator should
    be allocated to a particular ResourceClaim based on the GaudiClaimParameters and `allocatableDevices` of particular
    Kubernetes node
  - kubelet-plugin DaemonSet - node-agent, it performs three functions:
    1) supported hardware discovery on Kubernetes cluster node and it's announcement to the
      `GaudiAllocationState` that is specific to the node.
    2) preparation of the hardware allocated to the ResourceClaims for the Pod that is being started on the node.
    3) unpreparation of the hardware allocated to the ResourceClaims for the Pod that is being started on the node


## Deployment validation

After the controller and kubelet-plugin pods are ready, check one of GaudiAllocationState Custom
Resource object's contents. There should be one object for each cluster node with relevant devices,
describing the available Gaudi accelerators:
```bash
$ kubectl get -n intel-gaudi-resource-driver gaudiallocationstates.gaudi.resource.intel.com
```

Or shorter version
```bash
$ kubectl get -n intel-gaudi-resource-driver gas

NAME       AGE
node-1   2d19h
node-2   2d19h
node-3   2d19h
```

Example contents of the GaudiAllocationState CR object:
```bash
$ kubectl get -n intel-gaudi-resource-driver gas/node-2 -o yaml
apiVersion: gaudi.resource.intel.com/v1alpha1
kind: GaudiAllocationState
metadata:
  creationTimestamp: "2023-03-28T12:36:21Z"
  generation: 30
  name: node-2
  namespace: default
  ownerReferences:
  - apiVersion: v1
    kind: Node
    name: node-2
    uid: 3ae742c7-6654-4c7a-9d3d-5584b64bb014
  resourceVersion: "144509816"
  uid: 8487756c-e2fe-4a53-8cfd-6f2a3c583586
spec:
  allocatable:
    0000:b3:00.0-0x56c0:
      model: "Gaudi2"
      uid: 0000:93:00.0-0x56c0
status: Ready
```

## Deploying test pod to verify Gaudi resource-driver works

```bash
$ kubectl apply -f deployments/gaudi/delayed/inline/pod-inline.yaml
gaudiclaimparameters.gaudi.resource.intel.com/inline-claim-parameters created
resourceclaimtemplate.resource.k8s.io/test-inline-claim-template created
pod/test-inline-claim created
```

When the Pod gets into Running state, check that Gaudi was assigned to it:
```bash
$ kubectl logs test-inline-claim -c with-resource
drwxr-xr-x    2 root     root            80 Nov 28 13:35 .
drwxr-xr-x    6 root     root           380 Nov 28 13:35 ..
crw-rw-rw-    1 root     root      226,   0 Nov 28 13:35 accel0
crw-rw-rw-    1 root     root      226, 128 Nov 28 13:35 accel_controlD0
```

## Requesting resources

With Dynamic Resource Allocation the resources are requested in a similar way to how the persistent
storage is requested. The [Resource Claim](#resourceclaim) is an analog of Persistent Volume Claim,
and it is used for scheduling Pods to nodes based on the devices availability. It provides access
to devices in Pod's containers.

### Basic use case: Pod needs a Gaudi accelerator

The simplest way to start using Intel Gaudi resource driver is to create a ResourceClaim, and add it
to Pod spec to be used in container. The Intel Gaudi resource driver will take care of allocating
suitable device to the Resource Claim when Kubernetes is scheduling the Pod.

```yaml
apiVersion: resource.k8s.io/v1alpha2
kind: ResourceClaim
metadata:
  name: gaudi-claim-1
spec:
  resourceClassName: intel-gaudi
---
apiVersion: v1
kind: Pod
metadata:
  name: test-pod-1
spec:
  restartPolicy: Never
  containers:
  - name: with-resource
    image: busybox:latest
    command: ["sh", "-c", "ls -la /dev/accel/ && sleep 30"]
    resources:
      claims:
      - name: resource
  resourceClaims:
  - name: resource
    source:
      resourceClaimName: gaudi-claim-1
```

Two important sections in above Pod spec are:
- `resourceClaims` - all ResourceClaims that the Pod will use, need to be here
- `claims` - is the new section in container's `resources` section. If the container
  needs to use a ResourceClaim - the Claim needs to be listed in this section for
  that container.

In this example:
- the ResourceClaim `gaudi-claim-1` is created;
- the Pod `test-pod-1` declares that:
  - it will use Resource Claim `gaudi-claim-1`;
  - the container named `with-resource` will be using the resources allocated to the Resource Claim
    `gaudi-claim-1`.

### Resource Classes

Intel Gaudi resource driver provides two resource classes:
- `intel-gaudi`: intended for typical Gaudi requests for workloads.
- [intel-gaudi-monitor](#gaudi-monitor-deployment): intended for deployments of Gaudi monitoring software.


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
apiVersion: resource.k8s.io/v1alpha2
kind: ResourceClaimTemplate
metadata:
  name: claim-template-1
  namespace: default
spec:
  spec:
    resourceClassName: intel-gaudi
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
    command: ["sh", "-c", "ls -la /dev/accel/ && sleep 5"]
    resources:
      claims:
      - name: resource1
  resourceClaims:
  - name: resource1
    source:
      resourceClaimTemplateName: claim-template-1
```

#### Customizing resources request

Resource Claim references a Resource Class in `resourceClassName` field, and based on it the
Intel Gaudi resource driver decides what acceleratorss should be allocated to the Resource Claim.

ResourceClaim can also reference a GaudiClaimParameters in `parametersRef` to customize the request.
For instance, quantity and / or qualities of the accelerators.

Example of Resource Claim requesting 2 Gaudi accelerators:
```yaml
apiVersion: gaudi.resource.intel.com/v1alpha1
kind: GaudiClaimParameters
metadata:
  name: claim-params-1
  namespace: default
spec:
  count: 2
---
apiVersion: resource.k8s.io/v1alpha2
kind: ResourceClaim
metadata:
  name: claim-1
spec:
  resourceClassName: intel-gaudi
  parametersRef:
    apiGroup: gaudi.resource.intel.com/v1alpha1
    kind: GaudiClaimParameters
    name: claim-params-1
```

See details in [GaudiClaimParameters](#gaudiclaimparameters) object reference

## Gaudi monitor deployment

Gaudi monitor deployment ResourceClaim must specify a ResourceClass with GaudiClassParams having `monitor` field set to `true` (see [Resource Class specs](../deployments/gaudi/resource-class.yaml).

Unlike with normal Gaudi ResourceClaims:
* Monitor deployment gets access to all Gaudi devices on a node
* Resource Claim parameters are ignored
* Only (default) delayed allocation mode supported for Monitor resource class, not immediate

## Objects reference

### GaudiClaimParameters

Intel Gaudi resource driver `GaudiClaimParameters` format is following:
```yaml
apiVersion: gaudi.resource.intel.com/v1alpha1
kind: GaudiClaimParameters
metadata:
  name: gaudi-claim-params-1
  namespace: default
spec:
  count: 1
```

`count`: Required. Quantity of resource requested [1-8].

See [deployments/](../deployments/gaudi/) directory for use cases examples.

### GaudiClassParameters

Intel Gaudi resource driver `GaudiClasParameters` format is following:

```YAML
apiVersion: gaudi.resource.intel.com/v1alpha1
kind: GaudiClassParameters
metadata:
  name: gaudi-class-params-1
spec:
  monitor: false
```

`monitor`: Optional, default: false. Indicates whether the ResourceClass is intended for monitoring
workloads.