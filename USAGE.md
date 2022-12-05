# Limitations
- Full hardware features discovery is not yet implemented, only presence of relevant GPU is detected at the moment

# Requirements
- K8s 1.26+
- Runtime that supports CDI injection, Containerd 1.7+ or CRI-O 1.23+
- Go 1.19+ is required to build the image
- Either docker or podman or buildah to build the actual image

# Building container image
When building resource-driver container image it is possible to specify custom location and name by
overriding part of container image tag:
registry, image name or image version respectively as variables to build command:
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

- ensure you're running recent CRI-O 1.23+ or Containerd 1.7+ (ses [#Requirements](#requirements))
- make sure to enable `DynamicResourceAllocation`
[feature-gate](https://kubernetes.io/docs/reference/command-line-tools-reference/feature-gates/) in your cluster during initialization and alpha API in Kubernetes api-server, recommended cluster initialization is with the [cluster-config](hack/clusterconfig.yaml) file, for instance
```bash
sudo -E kubeadm init --config clusterconfig.yaml
```
- deploy cni
- ensure coredns pod is up `kubectl get pods -A`

## Deploy resource-driver

Fix the image location of [resource-driver](deployments/resource-driver.yaml) to be where your built image is

Deploy Custom Resource Definitions, resource-class and resoruce-driver
```bash
kubectl deploy -f deployments/static/crds/
kubectl deploy -f deployments/resource-class.yaml
kubectl deploy -f deployments/resource-driver.yaml
```

After the controller and kubelet-plugin pods get up and ready, check your GpuAllocationState CRDs.
There should be one per every cluster node with relevant GPUs describing available GPUs:
```bash
$ kubectl get gas
NAME         AGE
ubuntu22-1   12d

$ kubectl get gas/ubuntu22-1 -o yaml
apiVersion: gpu.dra.intel.com/v1alpha
kind: GpuAllocationState
metadata:
  creationTimestamp: "2022-11-16T12:29:28Z"
  generation: 9
  name: ubuntu22-1
  namespace: default
  ownerReferences:
  - apiVersion: v1
    kind: Node
    name: ubuntu22-1
    uid: 910dd9be-8c13-46e6-8e79-f7d2ad201b5f
  resourceVersion: "9186"
  uid: 6450bc89-c6b5-45dc-ac6b-32545fb7b608
spec:
  allocatableGpus:
    003f2d0e-d96a-44e9-bd9e-20264a39fc46:
      cdiDevice: card0
      memory: 1024
      model: UHDGraphics
      type: gpu
      uuid: 003f2d0e-d96a-44e9-bd9e-20264a39fc46
status: Ready
```

# Deploying test pod to verify GPU resource-driver works
```bash
$ kubectl apply -f deployments/delayed/inline/pod-inline.yaml 
gpuclaimparameters.gpu.dra.intel.com/inline-claim-parameters created
resourceclaimtemplate.resource.k8s.io/test-inline-claim-template created
pod/test-inline-claim created

$ kubectl logs test-inline-claim -c with-resource
drwxr-xr-x    2 root     root            80 Nov 28 13:35 .
drwxr-xr-x    6 root     root           380 Nov 28 13:35 ..
crw-rw-rw-    1 root     root      226,   0 Nov 28 13:35 card0
crw-rw-rw-    1 root     root      226, 128 Nov 28 13:35 renderD128
```
