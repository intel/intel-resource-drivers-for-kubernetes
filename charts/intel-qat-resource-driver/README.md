# Dynamic Resource Allocation (DRA) Intel QAT Driver Helm Chart

## The chart installs QAT resource driver:

- [QAT](https://github.com/intel/intel-resource-drivers-for-kubernetes/tree/main/doc/qat/README.md)

More info: [Intel Resource Drivers for Kubernetes](https://github.com/intel/intel-resource-drivers-for-kubernetes/tree/main)


## Installing the chart

```
helm install intel-qat-resource-driver oci://ghcr.io/intel/intel-resource-drivers-for-kubernetes/intel-qat-resource-driver \
    --create-namespace \
    --namespace intel-qat-resource-driver
```

## Uninstalling the chart
```
helm uninstall intel-qat-resource-driver --namespace intel-qat-resource-driver
```
(Optional) Delete the namespace:
```
kubectl delete ns intel-qat-resource-driver
```

## Configuration
See [Customizing the Chart Before Installing](https://helm.sh/docs/intro/using_helm/#customizing-the-chart-before-installing). To see all configurable options with detailed comments:

```console
helm show values oci://ghcr.io/intel/intel-resource-drivers-for-kubernetes/intel-qat-resource-driver
```

You may also run `helm show values` on this chart's dependencies for additional options.

| Key | Type | Default |
|-----|------|---------|
| image.repository | string | `intel` |
| image.name | string | `"intel-qat-resource-driver"` |
| image.pullPolicy | string | `"IfNotPresent"` |
| image.tag | string | `"v0.2.0"` |

If you change the image tag to be used in Helm chart deployment, ensure that the version of the container image is consistent with deployment YAMLs - they might change between releases.


## Read-only file system error for QAT

When the following error appears in the logs of the QAT Kubelet plugin:
```
kubectl logs -n intel-qat-resource-driver intel-qat-resource-driver-kubelet-plugin-ttcs6
DRA kubelet plugin
In-cluster config
Setting up CDI
failed to create kubelet plugin driver: cannot enable PF device '0000:6b:00.0': open /sysfs/bus/pci/devices/0000:6b:00.0/sriov_numvfs: read-only file system
```

Try reseting QAT by reloading its kernel driver:
```
rmmod qat_4xxx
modprobe qat_4xxx
```
