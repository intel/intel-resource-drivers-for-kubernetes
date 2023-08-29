# Limitations

- Max 8 GPUs can be requested for one resource claim
- Max 128GiB can be requested per GPU

# Requirements

- Kubernetes 1.26+
- Runtime that supports CDI injection, Containerd 1.7+ or CRI-O 1.23+
- Go 1.19+ is required to build the image
- Either docker, or podman, or buildah to build the resource-driver container image

# Building container image

When building Intel GPU resource driver container image it is possible to specify
custom registry and container image name, and tag to overriding release container image tag:

registry, image name or image version respectively and separately as variables to build command:
```bash
REGISTRY=myregistry IMAGENAME=myimage IMAGE_VERSION=myversion make container-build
```

or whole desired image tag
```bash
IMAGE_TAG=myregistry/myimagename:myversion make container-build
```

To just build an image locally without pushing it to registry, run
```bash
make container-build
```

To build and push images straight away:
```bash
REGISTRY=registry.local make container-push
```
or
```bash
IMAGE_TAG=registry.local/intel-gpu-resource-driver:latest make container-push
```

# Creating K8s cluster

- ensure you're running recent CRI-O 1.23+ or Containerd 1.7+ (ses [#Requirements](#requirements)), and respective `criSocket` in [cluster-config](hack/clusterconfig.yaml) file
- make sure to enable `DynamicResourceAllocation`
[feature-gate](https://kubernetes.io/docs/reference/command-line-tools-reference/feature-gates/) in your cluster during initialization and alpha API in Kubernetes api-server, recommended cluster initialization is with the [cluster-config](hack/clusterconfig.yaml) file, for instance
```bash
sudo -E kubeadm init --config clusterconfig.yaml
```
- deploy cni
- ensure coredns pod is up `kubectl get pods -A`

## Deploy resource-driver

Fix the image location of [resource-driver](deployments/resource-driver.yaml) to be where your built image is

Deploy Custom Resource Definitions, resource-class and finally resoruce-driver
```bash
kubectl apply -f deployments/static/crds/
kubectl apply -f deployments/resource-class.yaml
kubectl apply -f deployments/resource-driver.yaml
```

NB: kubelet-plugin is a priviliged container since it has to read and write to sysfs to manipulate
state of Linux kernel driver for Intel GPU.

After the controller and kubelet-plugin pods get up and ready, check your GpuAllocationState Custom Resource objects,
there should be one per every cluster node with relevant GPUs describing available GPUs:
```bash
$ kubectl get gas
NAME       AGE
icx-cp-1   2d19h
icx-cp-3   2d19h
spr-es-1   2d19h

$ kubectl get gas/icx-cp-3 -o yaml
apiVersion: gpu.dra.intel.com/v1alpha
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
      maxvfs: 0
      memory: 14248
      model: "0x56c0"
      parentuid: ""
      type: gpu
      uid: 0000:b3:00.0-0x56c0
status: Ready
```

# Deploying test pod to verify GPU resource-driver works
```bash
$ kubectl apply -f deployments/delayed/inline/pod-inline.yaml
gpuclaimparameters.gpu.dra.intel.com/inline-claim-parameters created
resourceclaimtemplate.resource.k8s.io/test-inline-claim-template created
pod/test-inline-claim created

# when the Pod gets into Running state, check that it has got a GPU
$ kubectl logs test-inline-claim -c with-resource
drwxr-xr-x    2 root     root            80 Nov 28 13:35 .
drwxr-xr-x    6 root     root           380 Nov 28 13:35 ..
crw-rw-rw-    1 root     root      226,   0 Nov 28 13:35 card0
crw-rw-rw-    1 root     root      226, 128 Nov 28 13:35 renderD128
```

# Workload deployment, migrating to Resource Claims from device drivers Resources style

In previous generation of GPU resource requests based on device drivers Pod has to have i915 resource requested:
```yaml
      resources:
        requests:
          memory: "300Mi"
          cpu: 2
          gpu.intel.com/i915: 1
```

With Dynamic Resource Allocation, the GPU resource is requested with Resource Claim,
a standalone object outside Pod definition, but it has to be referenced inside
Pod's resourceClaims field.

There are two ways of creating the Resource Claim - either explicitly, or as a
template inside Pod object, in latter case the Resource Claim will be generated
based on template specified. Respectively, the reference to the resource claim
inside the Pod will be either `resourceClaimName` or `resourceClaimTemplateName`:

```yaml
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

The Resource Claim object is resource-agnostic, but it has a `parametersRef` and
`resourceClassName` that define resource-specific parameters, and which Resource
Driver should handle thee allocation request respectively:
```yaml
apiVersion: resource.k8s.io/v1alpha1
kind: ResourceClaim
metadata:
  name: gpu-claim
spec:
  resourceClassName: intel-gpu
  parametersRef:
    apiGroup: gpu.dra.intel.com/v1alpha
    kind: GpuClaimParameters
    name: gpu-params-1
```

## GpuClaimParameters

Intel GPU Resource Driver supports next format of `GpuClaimParameters`:
```yaml
apiVersion: gpu.dra.intel.com/v1alpha
kind: GpuClaimParameters
metadata:
  name: gpu-params-1
  namespace: default
spec:
  count: 1
  type: "gpu"
  memory: 512
```

`count`: quantity of resource requested [1-8]

`type`: type of resource, possible values are `gpu`, `vf`, GPU is a plain GPU, `vf` is a SR-IOV Virtual Function

`memory`: amount of local memory per requested `gpu` or `vf`

For different use cases examples, see [deployments/](deployments/) directory.

# Use cases

## GPU

One Resource Claim can be used by any number of containers inside the same Pod.

If the workload Pod needs multiple GPUs, guarranteed to be different GPUs per container,
it is recommended to specify memory amount in the GpuClaimParameters to be more than half
of GPU's local memory. Since both requests' total memory amount will be greater than
the single GPU has, it will force Resource Driver to allocate different GPUs to each claim.

## SR-IOV Virtual Functions

While plain GPU allocation operates on overcommitment, meaning that the amount of GPU local
memory for particular container is not guaranteed, but calculated based on expected / requested
memory consumption, VFs on the other hand provide guaranteed memory amount to a GPU consumer
container, and are recommended for cases when the amount of GPU memory needed is known exactly.

If there are no VFs available in the cluster, the Resource Driver will attempt to find
a GPU that can have VFs, and is not allocated for the moment. If found, this GPU will then
be planned to have VFs respective to the requested amount and corresponding memory sizes.
If the allocation succeeds and the sufficient amount of VFs is planned, the Pod scheduling
will then proceed to node resource preparation where the new VFs will be provisioned
by the Intel Linux GPU Driver, and announced to the cluster resources by the kubelet-plugin.

AFter the workload that required VFs is complete. the VFs are removed from the worker node, and
the GPU is treated back as a plain GPU device.
