# GPU health management for Intel GPU resource driver

## YAML files

alertmanager.yaml:
* Alertmanager configuration for sending alert notifications to alert-webhook

alert-webhook.yaml:
* Webhook tainting GPUs in per-node GpuAllocationState CRs based on Alertmanager notifications

collectd-alert-rules.yaml:
* Alertmanager rules for Collectd Sysman plugin GPU metrics based alerts:
  https://github.com/collectd/collectd/pull/3968

xpum-alert-rules.yaml:
* Alertmanager rules for Intel XPU manager GPU metrics based alerts:
  https://github.com/intel/xpumanager

node-taint-cleaner.yaml:
* Example Job for clearing GPU taints from specified node


## Description

GPU alerts are specified in Alertmanager PrometheusRule k8s object.
Each alert rule has a name, a Prometheus metric query triggering the
alert, and some extra annotations & labels for their severity and
description.

Alertmanager config specifies that alerts with `service` label matching
Intel GPU monitor names are routed to alert-webhook.

Webhook maps (GPU metric) labels in the alert notifications to GPU
UIDs (used in node GpuAllocationState CR) and taints matching GPUs,
with alert name set as a reason for the tainting.


## Alert rules

Alert rule files include examples for all supported metrics that
are potentially relevant for GPU health.  However, all GPUs do not
provide all the metrics, and they can have different maximum values.

Additionally, currently webhook will only taint GPUs, NOT remove them,
so _one should not enable rules that disable GPUs unnecessarily_.

=> Review rules and update / leave only ones that are relevant for
   given cluster use-cases, before applying the rule file.

PS. If one wants alerts to be logged, but not acted on, webhook
options can be used either for additional (metric label based) alert
filtering, or to completely disable device tainting.


## Setup

Note: provided setup files and instructions assume GPU metric
exporters + Prometheus + Alertmanager + their operator run in
`monitoring` namespace, and Alertmanager to be named as "main".

Start Alertmanager webhook:
```
$ kubectl apply -f alert-webhook.yaml
```

After reading [Alert rules](#alert-rules) section, one can apply alert
rules either for XPU Manager, and/or collect Sysman plugin (GPU metric
exporter):
```
$ kubectl apply -f collectd-alert-rules.yaml
$ kubectl apply -f xpum-alert-rules.yaml
```

Check whether existing Alertmanager config has items that should be
added to the new config before applying:
```
$ kubectl get -n monitoring secret alertmanager-main -o json |\
  jq '.data."alertmanager.yaml"' | tr -d '"' | base64 -d
```

Apply new Alertmanager config over existing one:
```
$ kubectl -n monitoring create secret generic --dry-run=client -o yaml \
  alertmanager-main --from-file=alertmanager.yaml | kubectl apply -f -
```


## Manual testing

### Sending alerts to webhook

GPUs can be tainted manually, by calling webhook HTTP server with
suitable GPU alert notification.

Sending GPU alert from cluster master host, using service IP:
```
$ kubectl -n intel-gpu-resource-driver get all
  ...
  service/alert-webhook   ClusterIP   10.101.165.71   <none> 80/TCP    21h
  ...
$ WEBHOOK_IP=10.101.165.71
$ curl -H "Content-Type: application/json" -d \
  '{"alerts":[{"status": "firing","labels":{\
    "alertname":"GpuNeedsReset","node":"test-node","pci_dev":"0x56a0","pci_bdf":"03:00.0"\
  }}],"groupLabels":{"namespace":"monitoring","service":"xpu-manager"}}' \
  ${WEBHOOK_IP}/alertmanager/api/v1/alerts
```

Calling webhook HTTP server from ad-hoc pod:
```
$ kubectl run -n monitoring -it busybox --image=busybox:stable --restart=Never -- sh
# wget -S -O- --header "Content-Type: application/json" --post-data \
  '{"alerts":[{"status": "firing","labels":{\
    "alertname":"GpuNeedsReset","node":"test-node","pci_dev":"0x56a0","pci_bdf":"03:00.0"\
    }}],"groupLabels":{"namespace":"monitoring","service":"xpu-manager"}}' \
  http://alert-webhook.intel-gpu-resource-driver.svc.cluster.local/alertmanager/api/v1/alerts
# exit
```

Removing the ad-hoc pod, before another call:
```
$ kubectl -n monitoring delete pod/busybox
```


### Clearing / setting node GPU taints

This clears node taints, similarly to the example job (after setting $IMAGE + $NODE):
```
$ kubectl -n intel-gpu-resource-driver run -it clear-taints \
  --env=POD_NAMESPACE=intel-gpu-resource-driver --restart=Never \
  --overrides='{"spec":{"serviceAccount":"intel-gpu-alert-webhook-service-account"}}' \
  --image=${IMAGE} -- /alert-webhook -v 5 --node ${NODE}
```

And to taint GPUs on given node (instead of clearing taints), one just needs to add
`--reason '<reason>'` to that command.

Removing the ad-hoc pod, before another call:
```
$ kubectl -n intel-gpu-resource-driver delete pod/clear-taints
```
