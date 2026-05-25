# Dynamic Resource Allocation (DRA) Intel GPU Driver Helm Chart

## The chart installs GPU resource driver:

- [GPU](https://github.com/intel/intel-resource-drivers-for-kubernetes/tree/main/doc/gpu/README.md)

More info: [Intel Resource Drivers for Kubernetes](https://github.com/intel/intel-resource-drivers-for-kubernetes/tree/main)


## Installing the chart

```console
helm install \
    --namespace "intel-gpu-resource-driver" \
    --create-namespace \
    intel-gpu-resource-driver oci://ghcr.io/intel/intel-resource-drivers-for-kubernetes/intel-gpu-resource-driver-chart
```

> [!NOTE]
> Starting v0.10.0, [XPUM Daemon](https://github.com/intel/xpumanager/tree/v2.x/xpumd) is used for health monitoring and devices' details discovery.
> Starting v0.11.0 [XPUM Daemon] is enabled by default as a Helm chart dependency.


> [!NOTE]
> For Kubernetes clusters using [Pod Security Standards](https://kubernetes.io/docs/concepts/security/pod-security-standards/),
> pre-create the namespace with the respective label allowing to use HostPath Volumes:

```console
kubectl create namespace intel-gpu-resource-driver
kubectl label --overwrite namespace intel-gpu-resource-driver pod-security.kubernetes.io/enforce=privileged
helm install \
    --namespace intel-gpu-resource-driver \
    intel-gpu-resource-driver oci://ghcr.io/intel/intel-resource-drivers-for-kubernetes/intel-gpu-resource-driver-chart
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
helm show values oci://ghcr.io/intel/intel-resource-drivers-for-kubernetes/intel-gpu-resource-driver-chart
```

You may also run `helm show values` on this chart's dependencies for additional options.

| Key | Type | Default | Comment |
|-----|------|---------|---------|
| image.repository | string | `intel` ||
| image.name | string | `"intel-gpu-resource-driver"` ||
| image.pullPolicy | string | `"IfNotPresent"` ||
| image.tag | string | `"v0.11.0"` ||
| kubeletPlugin.healthMonitoring.enabled | bool | true | Enable (default) GPU details discovery method. Also, [health monitoring](../../doc/gpu/USAGE.md#health-monitoring-support). Requires [xpumd](https://github.com/intel/xpumanager/tree/v2.x/xpumd) |
| kubeletPlugin.privileged | bool | false | Enable alternative method for discovering GPU details when health monitoring is disabled |
| kubeletPlugin.manageBinding.enabled | bool | true | Enable dynamic switching between DRM and VFIO-PCI kernel drivers |

## Deploying to RedHat OpenShift Container Platform

### OpenShift 4.20
```console
helm install \
    --set openshift.enabled=true \
    --set openshift.version=4.20 \
    --namespace "intel-gpu-resource-driver" \
    --create-namespace \
    intel-gpu-resource-driver oci://ghcr.io/intel/intel-resource-drivers-for-kubernetes/intel-gpu-resource-driver-chart
```

### OpenShift 4.21+
The default value for `openshift.version` is `4.21`, so specifying a version is not necessary. Older versions than 4.20 are not supported.

```console
helm install \
    --set openshift.enabled=true \
    --namespace "intel-gpu-resource-driver" \
    --create-namespace \
    intel-gpu-resource-driver oci://ghcr.io/intel/intel-resource-drivers-for-kubernetes/intel-gpu-resource-driver-chart
```

> [!NOTE]
> Chart contains SecurityContextConstraints, which requires cluster admin privileges. Ensure the chart is installed by the cluster admin.


### Enabling health monitoring

The [Helm chart](../../charts/intel-gpu-resource-driver) controls health monitoring with two values:

| Value | Default | Effect |
|-------|---------|--------|
| `kubeletPlugin.healthMonitoring.enabled` | `true` | run the GPU DRA driver with `-m` parameter, requires xpumd |
| `xpumdEnabled` | `false` | deploy the [xpumd](https://github.com/intel/xpumanager/blob/v2.x/xpumd/charts/xpumd/README.md) chart as a dependency together with the GPU DRA driver |

When health monitoring is enabled, xpumd must be present in the cluster, either deployed by this
chart (`xpumdEnabled=true`) or installed separately. If it is enabled but xpumd is not reachable,
the kubelet-plugin exits about 5 minutes after start.

xpumd monitors GPUs through a DRA `adminAccess` ResourceClaim, so the namespace it runs in must be
labeled `resource.kubernetes.io/admin-access=true`. Pre-create and label that namespace before
installing:

```console
kubectl create namespace intel-gpu-resource-driver
kubectl label namespace intel-gpu-resource-driver resource.kubernetes.io/admin-access=true
```

With `xpumdEnabled=true` xpumd runs in the release namespace above. To install xpumd separately
instead, follow the [xpumd chart documentation](https://github.com/intel/xpumanager/blob/v2.x/xpumd/charts/xpumd/README.md) (deploy with `gpuAccess=dra`) and label its namespace the same way.
