# GPU health management for Intel GPU resource driver

Contents:
* Description
* Alert rules
  - Alert notification delays
  - Alert resolving on alert rule changes
* YAML files
* Setup
* Manual testing
  - Sending taint notifications to webhook
  - Clearing / setting taint reason for all node GPUs
* TODOs


## Description

GPU alerts are specified in Alertmanager PrometheusRule k8s object.
Each alert rule has a name, a Prometheus metric query triggering the
alert, and some extra annotations & labels for their severity and
description.

Alertmanager config specifies that alerts with `service` label matching
Intel GPU monitor names are routed to `alert-webhook`.

Webhook maps (GPU metric) labels in the alert notifications to GPU
UIDs used in node GAS CR (GpuAllocationState CustomResource) and
taints matching GPUs in that CR.  Each named alert is tracked as as
separate taint reason, and all of those alert reasons need to be
resolved before webhook clears the GPU taint.

Intel GPU resource driver checks that GPU is not tainted, before
it schedules workloads to it.


## Alert rules

Alert rule files include examples for all potentially relevant GPU
health related metrics currently supported by the Intel GPU monitors.
However, all GPUs do not provide all the metrics, and they can have
different maximum values.

=> Review rules and update / leave only ones that are relevant for
   given cluster use-cases, before applying the rule file.

PS. If one wants to specify which alerts are accepted (rest will be
logged, but not acted upon), webhook `--alerts` option can be used to
specify list of alerts (names) tainting the GPUs, and --groups`
options can be used to filter alerts based on their metric labels.

To disable node CR updates completely, one can use `--only-http`
option (e.g. for verifying alert/option handling from webhook logs
before real deployment).


### Alert notification delays

There are several configurable delays between metric change, and
webhook receiving Alert notification about it:

* Alerts are intended for continuing conditions and their Prometheus
  metric rules specify a period "FOR" over which that condition is
  evaluated
* Prometheus has configurable delays on how often it processes these
  rules, and by default those take at least 1 minute
* Alertmanager waits configured amount of time for new alerts within
  given group, before sending notification about them

=> By default it can take few minutes before GPU gets tainted, or its
   taint is removed


### Alert resolving on alert rule changes

As long as only the rule conditions are changed (nor other alert
details, nor Alertmanager configuration), Alertmanager will resolve
current alerts, if current values are not any more within the updated
rule conditions (e.g. alert power limit is increased).


## YAML files

alertmanager.yaml:
* Alertmanager configuration for sending alert notifications to alert-webhook

alert-webhook.yaml:
* Webhook updating GPU taints in (per-node) GAS CRs based on Alertmanager notifications

collectd-alert-rules.yaml:
* Alertmanager rules for Collectd Sysman plugin GPU metrics based alerts:
  https://github.com/collectd/collectd/pull/3968

xpum-alert-rules.yaml:
* Alertmanager rules for Intel XPU manager GPU metrics based alerts:
  https://github.com/intel/xpumanager

node-taint-cleaner.yaml:
* Example Job for clearing GPU taint reasons from specified node


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

### Sending taint notifications to webhook

GPUs can be tainted manually, by calling webhook HTTP server with
suitable GPU alert notification.

Sending "GpuNeedsReset" alert for NODE node from cluster master host,
using service IP:
```
$ kubectl -n intel-gpu-resource-driver get service
NAME            TYPE        CLUSTER-IP     EXTERNAL-IP   PORT(S)   AGE
alert-webhook   ClusterIP   10.101.165.71  <none>        80/TCP    100m

$ WEBHOOK_IP=10.101.165.71

$ curl -H "Content-Type: application/json" -d '
  {"alerts":[{"status": "firing","labels":{
    "alertname":"GpuNeedsReset","node":"NODE","pci_dev":"0x56a0","pci_bdf":"03:00.0"
  }}],"groupLabels":{"namespace":"monitoring","service":"intel-xpumanager"}}' \
  ${WEBHOOK_IP}/alertmanager/api/v1/alerts
```

Calling webhook HTTP server from ad-hoc pod:
```
$ kubectl run -n monitoring -it busybox --image=busybox:stable --restart=Never -- sh

# wget -S -O- --header "Content-Type: application/json" --post-data '
  {"alerts":[{"status": "firing","labels":{
    "alertname":"GpuNeedsReset","node":"NODE","pci_dev":"0x56a0","pci_bdf":"03:00.0"
  }}],"groupLabels":{"namespace":"monitoring","service":"intel-xpumanager"}}' \
  http://alert-webhook.intel-gpu-resource-driver.svc.cluster.local/alertmanager/api/v1/alerts
# exit
```

Removing the ad-hoc pod, before another call:
```
$ kubectl -n monitoring delete pod/busybox
```


### Clearing / setting taint reason for all node GPUs

This removes given taint REASON from GPUs on given NODE, using
webhook image, and service account created by "alert-webhook.yaml":
```
$ IMAGE=$(kubectl -n intel-gpu-resource-driver describe \
  deployment.apps/intel-gpu-dra-alert-webhook | awk '/Image:/{print $2}')

$ kubectl -n intel-gpu-resource-driver run -it clear-taints \
  --env=POD_NAMESPACE=intel-gpu-resource-driver --restart=Never \
  --overrides='{"spec":{"serviceAccount":"intel-gpu-alert-webhook-service-account"}}' \
  --image=${IMAGE} -- /alert-webhook -v 5 --node NODE --reason '!REASON'
```

To taint GPUs on given node (instead of clearing taint reasons), just
drop '!' from front of the taint REASON name.

Removing the ad-hoc pod, before running it again:
```
$ kubectl -n intel-gpu-resource-driver delete pod/clear-taints
```


## TODOS

Before installing the webhook by default with Helm (production use):
* Test with latest Prometheus / Alertmanager release
* Switch webhook to secure transport from plain unauthenticated http,
  and handle potential connection timeout
* Finish migrating collectd to OpenTelemetry spec & update rules file accordingly
* Document how to configure XPUM / Prometheus / Alertmanager to produce health alerts
  (as there is not yet collectd v6 release / container with Sysman plugin)
* Add (internal) E2E testing for XPUM / webhook-based health management
* Fine-tune *-rules.yaml files alert thresholds & timings for production use
