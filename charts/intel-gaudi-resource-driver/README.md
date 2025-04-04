# Dynamic Resource Allocation (DRA) Intel Gaudi Driver Helm Chart

## The chart installs Gaudi resource driver:

- [Gaudi](https://github.com/intel/intel-resource-drivers-for-kubernetes/tree/main/doc/gaudi/README.md)

More info: [Intel Resource Drivers for Kubernetes](https://github.com/intel/intel-resource-drivers-for-kubernetes/tree/main)


## Installing the chart

```
helm install intel-gaudi-resource-driver oci://ghcr.io/intel/intel-resource-drivers-for-kubernetes/intel-gaudi-resource-driver \
    --create-namespace \
    --namespace intel-gaudi-resource-driver
```

## Uninstalling the chart
```
helm uninstall intel-gaudi-resource-driver --namespace intel-gaudi-resource-driver
```
(Optional) Delete the namespace:
```
kubectl delete ns intel-gaudi-resource-driver
```

## Configuration
See [Customizing the Chart Before Installing](https://helm.sh/docs/intro/using_helm/#customizing-the-chart-before-installing). To see all configurable options with detailed comments:

```
helm show values oci://ghcr.io/intel/intel-resource-drivers-for-kubernetes/intel-gaudi-resource-driver
```

You may also run `helm show values` on this chart's dependencies for additional options.

| Key | Type | Default |
|-----|------|---------|
| image.repository | string | `intel` |
| image.name | string | `"intel-gaudi-resource-driver"` |
| image.pullPolicy | string | `"IfNotPresent"` |
| image.tag | string | `"v0.3.0"` |

> [!Note]
> If you change the image tag to be used in Helm chart deployment, ensure that the version of the container image is consistent with deployment YAMLs - they might change between releases.
