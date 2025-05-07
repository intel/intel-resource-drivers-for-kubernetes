# Dynamic Resource Allocation (DRA) Intel GPU Driver Helm Chart

## The chart installs GPU resource driver:

- [GPU](https://github.com/intel/intel-resource-drivers-for-kubernetes/tree/main/doc/gpu/README.md)

More info: [Intel Resource Drivers for Kubernetes](https://github.com/intel/intel-resource-drivers-for-kubernetes/tree/main)


## Installing the chart

```console
helm install \
    --namespace "intel-gpu-resource-driver" \
    --create-namespace \
    intel-gpu-resource-driver oci://ghcr.io/intel/intel-resource-drivers-for-kubernetes/intel-gpu-resource-driver
```

> [!NOTE]
> For Kubernetes clusters using [Pod Security Standards](https://kubernetes.io/docs/concepts/security/pod-security-standards/),
> pre-create the namespace with the respective label allowing to use HostPath Volumes.

```console
kubectl create namespace intel-gpu-resource-driver
kubectl label --overwrite namespace intel-gpu-resource-driver pod-security.kubernetes.io/enforce=privileged
helm install \
    --namespace intel-gpu-resource-driver \
    intel-gpu-resource-driver oci://ghcr.io/intel/intel-resource-drivers-for-kubernetes/intel-gpu-resource-driver
```

## Uninstalling the chart
```console
helm uninstall intel-gpu-resource-driver --namespace intel-gpu-resource-driver
```
(Optional) Delete the namespace:
```console
kubectl delete ns intel-gpu-resource-driver
```

## Configuration
See [Customizing the Chart Before Installing](https://helm.sh/docs/intro/using_helm/#customizing-the-chart-before-installing). To see all configurable options with detailed comments:

```console
helm show values oci://ghcr.io/intel/intel-resource-drivers-for-kubernetes/intel-gpu-resource-driver
```

You may also run `helm show values` on this chart's dependencies for additional options.

| Key | Type | Default |
|-----|------|---------|
| image.repository | string | `intel` |
| image.name | string | `"intel-gpu-resource-driver"` |
| image.pullPolicy | string | `"IfNotPresent"` |
| image.tag | string | `"v0.7.0"` |
