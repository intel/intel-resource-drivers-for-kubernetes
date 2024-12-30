# Test Cases

## Intel® QAT Device Plugin
There are test cases made for [Intel® QAT Device Plugin](https://github.com/intel/intel-device-plugins-for-kubernetes/blob/main/cmd/qat_plugin/README.md).
It is possible to run those images using this resource driver. Those images are
available in the following links.

- [qatlib-sample-code](https://github.com/intel/intel-device-plugins-for-kubernetes/tree/main/demo/openssl-qat-engine)
- [qat-dpdk-test](https://github.com/intel/intel-device-plugins-for-kubernetes/tree/main/demo/crypto-perf)

Build the images in your environment, create a resourceClaimTemplate and run
the pods with the following commands.
```
kubectl apply -f deployments/qat/tests/resource-claim-template.yaml
kubectl apply -k deployments/qat/tests/qatlib-sample-code
kubectl apply -k deployments/qat/tests/qat-dpdk-test
```
All cases include both crypto and compress tests.

To run `qat-dpdk-test`, the cluster should have `CPU Manager Policy` as `static`
in its kubelet configuration. In addition, `hugepages-2Mi` resource should be
available.

There is an example [cluster setup yaml](../../deployments/qat/tests/qat-dpdk-test/modified-cluster-setup.yaml)
for setting cpu manager policy as static. Re-create the cluster with the
configurations enabled.
