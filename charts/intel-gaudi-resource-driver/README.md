# Dynamic Resource Allocation (DRA) Intel Gaudi Driver Helm Chart

## The chart installs Gaudi resource driver:

- [Gaudi](https://github.com/intel/intel-resource-drivers-for-kubernetes/tree/main/doc/gaudi/README.md)

More info: [Intel Resource Drivers for Kubernetes](https://github.com/intel/intel-resource-drivers-for-kubernetes/tree/main)


## Installing the chart

```console
helm install \
    --namespace intel-gaudi-resource-driver \
    --create-namespace \
    intel-gaudi-resource-driver oci://ghcr.io/intel/intel-resource-drivers-for-kubernetes/intel-gaudi-resource-driver-chart
```

> [!NOTE]
> For Kubernetes clusters using [Pod Security Standards](https://kubernetes.io/docs/concepts/security/pod-security-standards/),
> pre-create the namespace with the respective label allowing to use HostPath Volumes.

```console
kubectl create namespace intel-gaudi-resource-driver
kubectl label --overwrite namespace intel-gaudi-resource-driver pod-security.kubernetes.io/enforce=privileged
helm install \
    --namespace intel-gaudi-resource-driver \
    intel-gaudi-resource-driver oci://ghcr.io/intel/intel-resource-drivers-for-kubernetes/intel-gaudi-resource-driver-chart
```

## Uninstalling the chart
```console
helm uninstall intel-gaudi-resource-driver --namespace intel-gaudi-resource-driver
```
(Optional) Delete the namespace:
```console
kubectl delete ns intel-gaudi-resource-driver
```

## Configuration
See [Customizing the Chart Before Installing](https://helm.sh/docs/intro/using_helm/#customizing-the-chart-before-installing). To see all configurable options with detailed comments:

```console
helm show values oci://ghcr.io/intel/intel-resource-drivers-for-kubernetes/intel-gaudi-resource-driver-chart
```

You may also run `helm show values` on this chart's dependencies for additional options.

| Key | Type | Default |
|-----|------|---------|
| image.repository | string | `intel` |
| image.name | string | `"intel-gaudi-resource-driver"` |
| image.pullPolicy | string | `"IfNotPresent"` |
| image.tag | string | `"v0.5.1"` |

> [!Note]
> If you change the image tag to be used in Helm chart deployment, ensure that the version of the container image is consistent with deployment YAMLs - they might change between releases.
