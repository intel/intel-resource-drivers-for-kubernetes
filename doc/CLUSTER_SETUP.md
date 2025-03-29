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

## Configure CDI in Containerd 1.0

Containerd config file should have `enable_cdi` and `cdi_spec_dirs`. Example `/etc/containerd/config.toml`:
```
version = 2
[plugins]
  [plugins."io.containerd.grpc.v1.cri"]
    enable_cdi = true
    cdi_spec_dirs = ["/etc/cdi", "/var/run/cdi"]
```

## Configure CDI in Containerd 2.0

Containerd 2.0 has CDI enabled by default. Pay attention that the plugin name has changed. Example `/etc/containerd/config.toml`:
```
version = 3
[plugins]
  [plugins."io.containerd.cri.v1.runtime"]
    enable_cdi = true
    cdi_spec_dirs = ["/etc/cdi", "/var/run/cdi"]
```

## Using minikube

To create a minikube cluster with DRA, use the command (change the K8s version in the last parameter if needed):
```shell
minikube start \
--feature-gates=DynamicResourceAllocation=true \
--extra-config=apiserver.feature-gates=DynamicResourceAllocation=true \
--extra-config=apiserver.runtime-config=resource.k8s.io/v1beta1=true \
--extra-config=scheduler.feature-gates=DynamicResourceAllocation=true \
--extra-config=controller-manager.feature-gates=DynamicResourceAllocation=true \
--extra-config=kubelet.feature-gates=DynamicResourceAllocation=true \
--container-runtime=containerd \
--kubernetes-version=1.32.0
```

Minikube will start its own Containerd inside the minikube docker container, where CDI needs to be
enabled. Connect to the minikube container and edit containerd config:
```shell
docker exec -it minikube /bin/bash
vi /etc/containerd/config.toml
```

Add two lines into the `[plugins."io.containerd.grpc.v1.cri"]` section:
```
  [plugins."io.containerd.grpc.v1.cri"]
    enable_cdi = true
    cdi_spec_dirs = ["/etc/cdi", "/var/run/cdi"]
```

Then save it, exit editor, and restart the containerd that runs inside the minikube
```
systemctl restart containerd
```

At last, exit from the minikube container.
