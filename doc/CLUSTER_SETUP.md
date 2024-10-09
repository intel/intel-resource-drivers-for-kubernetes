# Setting up new K8s cluster for usage with Dynamic Resource Allocation resource drivers

- In any uncertainty, refer to main [Kubernetes installation documentation](https://kubernetes.io/docs/setup/independent/create-cluster-kubeadm/) .
- Check what version of Kubernetes is [required](../README.md#supported-kubernetes-versions)
- Ensure you are running either CRI-O 1.23+ or Containerd 1.7+ with CDI support enabled, and that [cluster-config](../hack/clusterconfig.yaml) file uses `criSocket` matching it.
- Make sure to enable both `DynamicResourceAllocation`
  [feature-gate](https://kubernetes.io/docs/reference/command-line-tools-reference/feature-gates/),
  and alpha API for the Kubernetes api-server during your cluster initialization.
  - Example cluster initialization is in [cluster-config](../hack/clusterconfig.yaml) file
```bash
sudo -E kubeadm init --config hack/clusterconfig.yaml
```
- Deploy cni .
- Verify that `coredns` pod(s) are up: `kubectl get pods -A | grep dns`.

## Enable CDI in Containerd

Containerd config file should have `enable_cdi` and `cdi_specs_dir`. Example `/etc/containerd/config.toml`:
```
version = 2
[plugins]
  [plugins."io.containerd.grpc.v1.cri"]
    enable_cdi = true
    cdi_specs_dir = ["/etc/cdi", "/var/run/cdi"]
```
