# Overlay for installing Intel GPU resource driver in Red Hat OpenShift

- supported RHOS version: 4.21+

# Installing

```shell
kubectl apply -k deployments/gpu/overlays/openshift
```

The following warning could be shown during the installation:

```shell
Warning: would violate PodSecurity "restricted:latest": privileged (container "kubelet-plugin" must not set securityContext.privileged=true), allowPrivilegeEscalation != false (container "kubelet-plugin" must set securityContext.allowPrivilegeEscalation=false), restricted volume types (volumes "plugins-registry", "plugins", "cdi", "varruncdi", "sysfs" use restricted volume type "hostPath"), runAsNonRoot != true (pod or container "kubelet-plugin" must set securityContext.runAsNonRoot=true), runAsUser=0 (container "kubelet-plugin" must not set runAsUser=0)

```

This happens when the SecurityContextConstraints gets created later than the DaemonSet,
causing DaemonSet Pod creation to initially fail. Kubernetes will retry creating the Pods,
and will eventually find the needed SecurityContextConstraints object.
