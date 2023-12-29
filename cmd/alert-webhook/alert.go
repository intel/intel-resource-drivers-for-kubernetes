/*
 * Copyright (c) 2023, Intel Corporation.  All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"k8s.io/klog/v2"
)

const (
	// for parsing HTTP notifications.
	contentType  = "Content-Type"
	acceptedType = "application/json"
	maxJSONSize  = 1024 * 1024
)

// documented at: https://prometheus.io/docs/alerting/latest/configuration/#webhook_config
type alertsType struct {
	Version           string
	GroupKey          string // key identifying the group of alerts (e.g. to deduplicate)
	Status            string // <resolved|firing>
	Receiver          string
	ExternalURL       string // backlink to the Alertmanager.
	GroupLabels       map[string]string
	CommonLabels      map[string]string
	CommonAnnotations map[string]string
	Alerts            *[]alertType
	TruncatedAlerts   int // how many alerts have been truncated due to "max_alerts"
}

type alertType struct {
	Status       string // <resolved|firing>
	StartsAt     string // <rfc3339>
	EndsAt       string // <rfc3339>
	GeneratorURL string // identifies the entity that caused the alert
	Fingerprint  string // fingerprint to identify the alert
	Annotations  map[string]string
	Labels       map[string]string
}

// uid: reason
type gpuTaints map[string]string

// node: uid: reason
type nodeTaints map[string]gpuTaints

// Alerter filters received Alert notifications and calls tainter when needed.
// Below members are not modified during alerter life time.
type alerter struct {
	tainter *tainter
	// command separated list of accepted values (for logging)
	vlist *string
	// lookup maps for them
	groups map[string]bool
	values map[string]bool
	alerts map[string]bool
}

func string2map(s *string) map[string]bool {
	nmap := make(map[string]bool)
	if s == nil {
		return nmap
	}
	for _, name := range strings.Split(*s, ",") {
		if name != "" {
			nmap[name] = true
		}
	}
	return nmap
}

// newAlerter filtering flags are comma separated strings with
// accepted alert & group names, and group values.
func newAlerter(f *filterFlags, tainter *tainter) (*alerter, error) {
	klog.V(5).Info("newAlerter()")
	alerter := &alerter{
		tainter: tainter,
		vlist:   f.values,
		alerts:  string2map(f.alerts),
		groups:  string2map(f.groups),
		values:  string2map(f.values),
	}
	klog.V(5).Infof("Alerter initialized: %+v", alerter)
	return alerter, nil
}

func requestCheck(r *http.Request) int {
	if r.Method != http.MethodPost {
		return http.StatusMethodNotAllowed
	}
	if r.URL.Path != alertURL {
		return http.StatusNotFound
	}
	if r.ContentLength > maxJSONSize {
		return http.StatusRequestEntityTooLarge
	}
	if ctype, found := r.Header[contentType]; !found || len(ctype) == 0 || ctype[0] != acceptedType {
		return http.StatusUnsupportedMediaType
	}
	return http.StatusOK
}

// TODO: handle connection timeouts (server option + deadline for read here).
func (a *alerter) parseRequests(w http.ResponseWriter, r *http.Request) {
	klog.V(5).Info("parseRequests()")

	if status := requestCheck(r); status != http.StatusOK {
		klog.V(3).Infof("Received invalid notification/type from: %s", r.RemoteAddr)
		http.Error(w, http.StatusText(status), status)
		return
	}

	data := make([]byte, r.ContentLength)
	if count, err := io.ReadFull(r.Body, data); int64(count) != r.ContentLength {
		status := http.StatusRequestTimeout
		http.Error(w, http.StatusText(status), status)
		klog.V(3).Infof("Full notification body read (%d/%d) from '%s' failed: %v",
			count, r.ContentLength, r.RemoteAddr, err)
		return
	}

	klog.V(5).Infof("%d bytes notification from '%s': %s\n", r.ContentLength, r.RemoteAddr, string(data))

	if err := a.parseNotification(data); err != nil {
		status := http.StatusBadRequest
		http.Error(w, http.StatusText(status), status)
		klog.V(3).Infof("Failed: %v", err)
	}
}

func (a *alerter) parseNotification(data []byte) error {
	klog.V(5).Info("parseNotification()")

	if !json.Valid(data) {
		return errors.New("invalid JSON")
	}

	var alerts []alertType
	var notification alertsType

	// data can be either a full notification or just a list of alerts.
	if err := json.Unmarshal(data, &notification); err != nil {
		if err := json.Unmarshal(data, &alerts); err != nil {
			return errors.New("content unrecognized as notification or alerts list")
		}
		notification.Alerts = &alerts
	} else {
		if notification.Alerts == nil {
			return errors.New("missing 'alerts' array")
		}
		alerts = *notification.Alerts
	}

	if len(alerts) == 0 {
		return errors.New("empty 'alerts' array")
	}

	if !a.matchGroupFilters(notification.GroupLabels) {
		klog.V(3).Infof("%d alerts skipped due to group label mismatches: %v",
			len(alerts), alertNameCounts(alerts))
		return nil
	}

	taints, count, msg := a.filteredAlerts(alerts)
	if len(taints) == 0 {
		return fmt.Errorf("all %d alerts filtered out, last due to '%s'", len(alerts), msg)
	}

	if count < len(alerts) {
		klog.V(3).Infof("%d/%d alerts passed checks (last fail: %s)", count, len(alerts), msg)
	}

	// issues are logged in callers
	return a.updateNodeTaints(taints)
}

// matchGroupFilters checks whether notification should be processed further.
func (a *alerter) matchGroupFilters(labels map[string]string) bool {
	if len(a.groups) > 0 && len(a.values) > 0 {
		for name := range a.groups {
			value := labels[name]
			if _, found := a.values[value]; !found {
				klog.V(5).Infof("group '%s' label value '%s' does not match '%s'",
					name, value, *a.vlist)
				return false
			}
		}
	}
	return true
}

// alertNameCounts return alername:count mapping for logging.
func alertNameCounts(alerts []alertType) map[string]int {
	names := make(map[string]int)
	for _, alert := range alerts {
		labels := alert.Labels
		if labels == nil {
			labels = map[string]string{}
		}
		name := labels["alertname"]
		if _, found := names[name]; !found {
			names[name] = 0
		}
		names[name]++
	}
	return names
}

// filteredAlerts parses alerts into node:uid:reason taint mapping.
// It returns that mapping, a count of alerts that matched alert
// filter list + had the required labels, and an error description
// for last mismatch (if any).
func (a *alerter) filteredAlerts(alerts []alertType) (nodeTaints, int, string) {
	klog.V(5).Info("filteredAlerts()")

	msg := ""
	count := 0
	taints := make(nodeTaints)

	for i, alert := range alerts {
		info := fmt.Sprintf(" for alert-%d", i)

		labels := alert.Labels
		if labels == nil {
			msg = "'labels' map missing" + info
			continue
		}

		reason := labels["alertname"]
		if reason == "" {
			msg = "'alertname' label missing" + info
			continue
		}

		// add alert name to info
		info = fmt.Sprintf("%s '%s'", info, reason)

		if len(a.alerts) > 0 && !a.alerts[reason] {
			msg = "no alert list match" + info
			continue
		}

		// identify failing/recovered GPU for accepted alerts
		node := labels["node"]
		if node == "" {
			msg = "'node' label missing" + info
			continue
		}
		uid, errmsg := deviceUID(labels)
		if errmsg != "" {
			msg = errmsg + info
			continue
		}

		// TODO:
		// - Check rfc3339 dates and drop firing alerts that
		//   have already ended or been resolved
		// - Track each reason for each GPU on each node separately,
		//   so that GPU can be untainted when *all* taint reasons
		//   have been resolved
		switch alert.Status {
		case "firing":
		case "resolved":
			msg = "resolving alerts not supported" + info
			continue
		default:
			msg = fmt.Sprintf("invalid status '%s'%s", alert.Status, info)
			continue
		}

		if _, found := taints[node]; !found {
			taints[node] = make(gpuTaints)
		}

		// add previous tainting reason
		if old := taints[node][uid]; old != "" {
			reason = reason + "+" + old
		}
		taints[node][uid] = reason
		count++
	}

	return taints, count, msg
}

// deviceUID returns device UID strings matching to
// GpuAllocationStateSpec.AllocatableDevices[] keys, based
// on GPU metric labels in the given 'labels' map, and
// a non-empty error string (when labels are missing).
func deviceUID(labels map[string]string) (string, string) {
	uid := ""
	bdf := labels["pci_bdf"]
	if bdf == "" {
		return uid, "'pci_pdf' label missing"
	}
	dev := labels["pci_dev"]
	if dev == "" {
		return uid, "'pci_dev' label missing"
	}

	// use full name for abbreviated PCI BDF strings
	if len(bdf) < 10 && !strings.HasPrefix(bdf, "0000:") {
		bdf = "0000:" + bdf
	}

	uid = bdf + "-" + dev
	return uid, ""
}

// updateNodeTaints sets GPU taints for each node in 'taints'.
func (a *alerter) updateNodeTaints(taints nodeTaints) error {
	klog.V(5).Info("updateNodeTaints()")
	err := error(nil)
	for node, gpus := range taints {
		if a.tainter != nil {
			err = a.tainter.setNodeTaints(node, gpus)
			if err != nil {
				klog.Errorf("node GAS update failed: %v", err)
			}
		} else {
			// "--only-http" option used
			klog.V(5).Infof("Tainter missing, would be used to taint '%s' node GPUs: %v", node, gpus)
		}
	}

	return err
}
