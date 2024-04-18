# GPU health management for Intel GPU resource driver

Contents:
* Description
* Alert rules
  - Alert notification delays
  - Alert resolving on alert rule changes
* YAML files
* Setup
* Manual tainting / untainting
  - Using webhook binary
  - Using Curl
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
a separate taint reason, and all of those alert reasons need to be
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

As long as only the rule conditions for an alert are changed (not
other alert details, nor Alertmanager configuration), Alertmanager
will resolve that alert if its new rule conditions do not trigger any
more with the related metric values.  E.g. after power limit for an
alert is increased.


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


## Manual tainting / untainting

### Using webhook binary

Webhook binary provides options for listing and manually updating
taints reasons for given GPUs on given nodes.

By default `list` and `untaint` actions concern all GPU devices and
all their taint reasons, but one can limit action to specific ones.

As example, this removes given taint REASON1 from one GPU on given
NODE1, using already running webhook deployment:
```
$ kubectl exec -it deployment/intel-gpu-dra-alert-webhook \
  -n intel-gpu-resource-driver -- /alert-webhook -v 3 \
  --reasons REASON1 --devices 0000:03:00.0-0x4905 \
  --nodes NODE1 --action untaint
```

One can then ask it to list the GPUs and their taints to
verify taint removal:
```
$ kubectl exec -it deployment/intel-gpu-dra-alert-webhook \
  -n intel-gpu-resource-driver -- /alert-webhook -v 3 \
  --nodes NODE1 --action list
I0520 11:57:50.423141 3608388 taint.go:411] NODE1:
I0520 11:57:50.423301 3608388 taint.go:470] - 0000:03:00.0-0x4905:
I0520 11:57:50.423268 3608388 taint.go:470] - 0000:0a:00.0-0x4905: [REASON2]
I0520 11:57:50.423327 3608388 taint.go:231] DONE!
I0520 11:57:50.423340 3608388 taint.go:242] Specified nodes:
I0520 11:57:50.423353 3608388 taint.go:253] - all matched
I0520 11:57:50.423364 3608388 taint.go:268] Summary:
I0520 11:57:50.423380 3608388 taint.go:273] - 2 devices on 1 nodes
I0520 11:57:50.423398 3608388 taint.go:287] - 1 of them tainted
I0520 11:57:50.423415 3608388 taint.go:289] Unique taint reasons:
I0520 11:57:50.423433 3608388 taint.go:291] - REASON2
```

If webhook binary is run directly instead of using its deployment,
correct namespace and Kubernetes config may need to be given.
This will list GPUs and their taints for the whole cluster:
```
$ POD_NAMESPACE=intel-gpu-resource-driver ./alert-webhook -v 3 \
  --kubeconfig ~/.kube/config --nodes all --action list
```


### Using Curl

GPU taint reasons can be added and removed manually also by calling
webhook HTTP server directly with suitable GPU alert notification
message(s).

For webhook to accept such messages, names of the taint reasons and
service (supposedly) sending them, must match webhook accept list for
alert names & group labels (if such constraints are specified for it).

For example, if `AdminTaint` reason from `GpuAdmin` service are
accepted, this will taint a _single_ GPU with `AdminTaint`, by sending
such alert message for NODE node from cluster master host, using
webhook service IP:
```
$ kubectl -n intel-gpu-resource-driver get service
NAME            TYPE        CLUSTER-IP     EXTERNAL-IP   PORT(S)   AGE
alert-webhook   ClusterIP   10.101.165.71  <none>        80/TCP    100m

$ WEBHOOK_IP=10.101.165.71

$ curl -H "Content-Type: application/json" -d '
  {"alerts":[{"status": "firing","labels":{
    "alertname":"AdminTaint","node":"NODE","pci_dev":"0x56a0","pci_bdf":"03:00.0"
  }}],"groupLabels":{"namespace":"monitoring","service":"GpuAdmin"}}' \
  ${WEBHOOK_IP}/alertmanager/api/v1/alerts
```

(Changing `firing` status to `resolved` would clear the specified taint reason.)


## TODOS

Before installing the webhook by default with Helm (production use):
* Test with latest Prometheus / Alertmanager release
* Handle connection timeouts + secure transport support (HTTP -> HTTPS)
* Document how to configure XPUM / Prometheus / Alertmanager to produce health alerts
* Add (internal) E2E testing for XPUM / webhook-based health management
* Fine-tune *-rules.yaml files alert thresholds & timings for production use

Before adding collectd metric alerts to Helm install:
* Collectd Sysman plugin migration to OpenTelemetry spec + corresponding rules file update
* Collectd v6 release / container with Sysman plugin
