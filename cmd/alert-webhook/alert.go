/*
 * Copyright (c) 2023-2024, Intel Corporation.  All Rights Reserved.
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
	"sync"
	"time"

	"k8s.io/klog/v2"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
)

const (
	// for parsing HTTP notifications.
	contentType  = "Content-Type"
	acceptedType = "application/json"
	maxJSONSize  = 1024 * 1024
	// alert types.
	alertFiring   = "firing"
	alertResolved = "resolved"
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
	StartsAt     string // <rfc3339>, when alert started firing, or time.Now()
	EndsAt       string // <rfc3339>, when alert ends, or start + timeout period
	GeneratorURL string // identifies the entity that caused the alert
	Fingerprint  string // fingerprint to identify the alert
	Annotations  map[string]string
	Labels       map[string]string
}

type reasonInfo struct {
	name   string
	status string
	start  time.Time
	end    time.Time
	// whether stored reason has been processed
	processed bool
}

// reason name: info.
type taintReasons map[string]reasonInfo

// gpu UID: taint reasons.
type gpuTaints map[string]taintReasons

// node name: gpu taints.
type nodeTaints map[string]gpuTaints

// Alerter filters received Alert notifications and calls tainter when needed.
// Below members are not modified during alerter life time.
type alerter struct {
	tainter *tainter
	// store taint info between notifications for improved timestamp checks
	taints nodeTaints
	// syncing for updating taints
	mutex sync.Mutex
	// starting time for pre-existing taints in GAS CR
	start time.Time
	// maps used for alert filtering
	groups map[string]map[string]bool
	alerts map[string]bool
}

func string2alerts(s *string) map[string]bool {
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

// string2groups parses 'group1=value1,value2:group2=...' format and returns corresponding map.
func string2groups(s *string) (map[string]map[string]bool, error) {
	groups := make(map[string]map[string]bool)
	if s == nil {
		return groups, nil
	}
	for _, spec := range strings.Split(*s, ":") {
		parts := strings.Split(spec, "=")
		group := parts[0]
		if len(parts) != 2 || group == "" {
			return nil, fmt.Errorf("invalid <group>=<values> spec: '%s'", spec)
		}
		if _, found := groups[group]; found {
			return nil, fmt.Errorf("group '%s' already specified in '%s'", group, *s)
		}
		groups[group] = make(map[string]bool)
		for _, value := range strings.Split(parts[1], ",") {
			groups[group][value] = true
		}
	}
	return groups, nil
}

// newAlerter filtering flags are comma separated strings with
// accepted alert & group names, and group values.
func newAlerter(f *filterFlags, tainter *tainter) (*alerter, error) {
	klog.V(5).Info("newAlerter()")
	groups, err := string2groups(f.groups)
	if err != nil {
		return nil, err
	}
	alerter := &alerter{
		tainter: tainter,
		taints:  make(nodeTaints),
		start:   time.Now(),
		alerts:  string2alerts(f.alerts),
		groups:  groups,
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

// Webhooks are assumed to respond with 2xx response codes on a successful
// request and 5xx response codes are assumed to be recoverable.
func (a *alerter) parseRequests(w http.ResponseWriter, r *http.Request) {
	klog.V(5).Info("parseRequests()")

	if status := requestCheck(r); status != http.StatusOK {
		klog.V(3).Infof("Received invalid notification/type from: %s", r.RemoteAddr)
		http.Error(w, http.StatusText(status), status)
		return
	}

	data := make([]byte, r.ContentLength)
	// TODO: handle connection timeouts (server option + deadline for read here).
	if count, err := io.ReadFull(r.Body, data); int64(count) != r.ContentLength {
		status := http.StatusRequestTimeout
		http.Error(w, http.StatusText(status), status)
		klog.V(3).Infof("Full notification body read (%d/%d) from '%s' failed: %v",
			count, r.ContentLength, r.RemoteAddr, err)
		return
	}

	klog.V(6).Infof("%d bytes notification from '%s': %s\n", r.ContentLength, r.RemoteAddr, string(data))

	msg, status := a.parseNotification(data)
	if status != http.StatusOK {
		http.Error(w, msg, status)
	} else {
		fmt.Fprintf(w, "%s\n", msg)
	}

	klog.V(3).Info(msg)
}

func (a *alerter) parseNotification(data []byte) (string, int) {
	// unmarshal (and group filter) notification into alerts.
	alerts, err := a.parseAlerts(data)
	if err != nil {
		msg := fmt.Sprintf("Failed: %v", err)
		return msg, http.StatusBadRequest
	}

	// parse alerts into internal taints.
	count, err := a.parseTaints(alerts)
	msg := fmt.Sprintf("%d/%d alerts passed notification processing", count, len(alerts))

	// no error return for successfully unmarshalled notifications which content
	// updates were discarded, otherwise Alertmanager just continues re-sending them.
	if err != nil {
		msg += fmt.Sprintf(", and taint updates failed: %v", err)
		return msg, http.StatusOK
	}

	// returning error on GAS update failure should result in
	// Alertmanager resending the notifications after a while.
	if err := a.updateNodeTaints(); err != nil {
		msg += fmt.Sprintf(", updating GAS CRs failed: %v", err)
		return msg, http.StatusServiceUnavailable
	}

	return msg, http.StatusOK
}

// parseAlerts unmarshals notification and returns list of alerts to process.
func (a *alerter) parseAlerts(data []byte) ([]alertType, error) {
	klog.V(5).Info("parseNotification()")

	if !json.Valid(data) {
		return nil, errors.New("invalid JSON")
	}

	var alerts []alertType
	var notification alertsType

	// data can be either a full notification or just a list of alerts.
	if err := json.Unmarshal(data, &notification); err != nil {
		if err := json.Unmarshal(data, &alerts); err != nil {
			return nil, errors.New("content unrecognized as notification or alerts list")
		}
		notification.Alerts = &alerts
	} else {
		if notification.Alerts == nil {
			return nil, errors.New("missing 'alerts' array")
		}
		alerts = *notification.Alerts
	}

	if len(alerts) == 0 {
		return nil, errors.New("empty 'alerts' array")
	}

	if !a.matchGroupFilters(notification.GroupLabels) {
		return nil, fmt.Errorf("all alerts skipped due to their group label mismatch: %v",
			alertNameCounts(alerts))
	}

	return alerts, nil
}

// matchGroupFilters checks whether notification should be processed further.
func (a *alerter) matchGroupFilters(labels map[string]string) bool {
	if len(a.groups) > 0 {
		for name, group := range a.groups {
			value := labels[name]
			if _, found := group[value]; !found {
				klog.V(5).Infof("'%s' group label value '%s' not in '%v'",
					name, value, group)
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
		if name == "" {
			name = "<alertname label missing>"
		}
		if _, found := names[name]; !found {
			names[name] = 0
		}
		names[name]++
	}
	return names
}

// parseTaints filters alerts, parses taint reasons to alerter.taints
// and returns count of how main alerts passed checks.
func (a *alerter) parseTaints(alerts []alertType) (int, error) {

	count, msg := a.filteredAlerts(alerts)
	if count == 0 {
		if msg == "" {
			return count, fmt.Errorf("all alerts filtered out")
		}
		return count, fmt.Errorf("all alerts filtered out, last fail: \"%s\"", msg)
	}

	if count < len(alerts) {
		klog.V(3).Infof("%d/%d alerts passed checks", count, len(alerts))
		if msg != "" {
			klog.V(3).Infof("last fail: \"%s\"", msg)
		}
	}

	return count, nil
}

// filteredAlerts parses alerts into internal node:gpu:reasons taint mapping.
// It returns count of alerts (that matched alert filter list, had the required
// labels, and were not redundant), and an error description for last mismatch (if any).
func (a *alerter) filteredAlerts(alerts []alertType) (int, string) {
	klog.V(5).Info("filteredAlerts()")

	msg := ""
	count := 0

	for i, alert := range alerts {
		info := fmt.Sprintf(" in alert-%d", i+1)

		labels := alert.Labels
		if labels == nil {
			msg = "'labels' map missing" + info
			continue
		}

		name := labels["alertname"]
		if name == "" {
			msg = "'alertname' label missing" + info
			continue
		}

		// add alert name to info
		info = fmt.Sprintf("%s '%s'", info, name)

		if len(a.alerts) > 0 && !a.alerts[name] {
			msg = "no alert list match" + info
			continue
		}

		// identify failing/recovered GPU for accepted alerts
		node := labels["node"]
		if node == "" {
			msg = "'node' label missing" + info
			continue
		}

		info = fmt.Sprintf("%s for '%s' node", info, node)
		dev, errmsg := deviceUID(labels)
		if errmsg != "" {
			msg = errmsg + info
			continue
		}

		reason, errmsg := parseReasonInfo(alert, name)
		if errmsg != "" {
			msg = errmsg + info
			continue
		}

		accepted, errmsg := a.updateReasons(node, dev, reason)
		if errmsg != "" {
			msg = errmsg + info
		} else if accepted {
			count++
		}
	}

	return count, msg
}

func parseReasonInfo(alert alertType, name string) (reasonInfo, string) {
	info := reasonInfo{}
	if alert.Status != alertFiring && alert.Status != alertResolved {
		return info, fmt.Sprintf("invalid status '%s'", alert.Status)
	}
	info.status = alert.Status

	var err error
	now := time.Now()

	if alert.StartsAt == "" {
		info.start = now
	} else if info.start, err = time.Parse(time.RFC3339, alert.StartsAt); err != nil {
		return info, fmt.Sprintf("start time parse error '%v'", err)
	}

	if alert.EndsAt == "" {
		info.end = now
	} else if info.end, err = time.Parse(time.RFC3339, alert.EndsAt); err != nil {
		return info, fmt.Sprintf("end time parse error '%v'", err)
	}

	if info.start.After(now) {
		return info, fmt.Sprintf("start time '%v' in future", info.start)
	}

	info.name = name
	return info, ""
}

// updateReasons returns true if notification requires taint update, and error
// string if given node was unknown or did not have any devices to taint.
func (a *alerter) updateReasons(node, dev string, info reasonInfo) (bool, string) {
	// a.taints access serialization
	a.mutex.Lock()
	defer a.mutex.Unlock()

	name := info.name

	if _, found := a.taints[node]; !found {
		var gpus gpuTaints
		if a.tainter != nil {
			gpus = a.tainter.getNodeTaints(node, a.start)
		} else {
			// running in --only-http mode, fake a node having (untainted) GPUs
			gpus = gpuTaints{}
		}
		if gpus == nil {
			klog.V(3).Infof("reject '%s' alert for node '%s' with no GPUs", name, node)
			return false, fmt.Sprintf("no '%s' GPU node", node)
		}
		a.taints[node] = gpus
	}

	if _, found := a.taints[node][dev]; !found {
		a.taints[node][dev] = make(taintReasons)
	}

	reasons := a.taints[node][dev]

	// new notification?
	if _, found := reasons[name]; !found {
		if info.status == alertResolved {
			klog.V(3).Infof("ignoring '%s' resolve for '%s' / '%s' with no matching taint",
				name, node, dev)
			return false, ""
		}
		reasons[name] = info
		return true, ""
	}

	// alert reason or its resolve already exists
	old := reasons[name]

	// End time can be either when alert really ends, or just start time + timeout period.
	// Newer Prometheus versions should set alert endAt time (after which Alertmanager
	// will send resolve notification for it, if there has been no new alert), but older
	// versions may not set end time for firing alerts.

	replace := false
	switch info.status {
	case alertFiring:
		// firing alert starts _or_ ends later => replace earlier one
		if info.start.After(old.start) || info.end.After(old.end) {
			replace = true
		}
	case alertResolved:
		// resolved alert ends later (end is zero for firing alerts) _and_ starts no later
		// => replace earlier one
		if info.end.After(old.end) &&
			(info.start.After(old.start) || info.start.Equal(old.start)) {
			replace = true
		}
	default:
		panic("unknown alert state")
	}

	if replace {
		// Note: if earlier alert is of same type, but already processed, the new one
		// could also be marked as processed to skip related GAS checks, as CR does not
		// store timestamps (needed for webhook resolve notification processing).
		//
		// For now that optimization is skipped, so that new alerts will
		// (eventually) cause manually removed taints to be re-applied to CR.
		klog.V(5).Infof("replacing %s '%s' alert for '%s' / '%s' with newer %s (%v) one",
			old.status, old.name, node, dev, info.status, info.start)
		reasons[name] = info
		return true, ""
	}

	klog.V(5).Infof("ignoring stale %s '%s' (%v < %v | %v < %v) alert for '%s' / '%s'",
		info.status, info.name, info.start, old.start, info.end, old.end, node, dev)
	return false, ""
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

	uid = device.DeviceUIDFromPCIinfo(bdf, dev)
	return uid, ""
}

// updateNodeTaints updates GPU taints for each node.
func (a *alerter) updateNodeTaints() error {
	klog.V(5).Info("alerter.updateNodeTaints()")

	// a.taints access serialization
	a.mutex.Lock()
	defer a.mutex.Unlock()

	lastErr := error(nil)
	for node := range a.taints {

		gpus := a.taintsToProcess(node)
		if len(gpus) == 0 {
			klog.V(5).Infof("no unprocessed GPU taints for node '%s'", node)
			continue
		}

		klog.V(5).Infof("'%s' node GPU taints to process: %+v", node, gpus)

		if a.tainter != nil {
			err := a.tainter.updateNodeTaints(node, gpus)
			if err != nil {
				klog.Errorf("keep new taints, '%s' node GAS update failed: %v", node, err)
				lastErr = err
				continue
			}
		} else {
			// "--only-http" option used
			klog.V(5).Infof("Tainter missing, would be used to taint '%s' node GPUs: %v", node, gpus)
		}

		a.handleProcessedTaints(node, gpus)

		klog.V(5).Infof("remaining '%s' node alerter taints: %+v", node, a.taints[node])
	}

	return lastErr
}

// taintsToProcess goes through alerter reasons for given node GPUs, and returns
// map of unprocessed firing & resolved ones (that need to be updated to GAS CR).
func (a *alerter) taintsToProcess(node string) gpuTaints {

	taints := make(gpuTaints)
	for dev, reasons := range a.taints[node] {
		newreasons := make(taintReasons)
		for name, info := range reasons {
			if info.processed {
				if info.status != alertFiring {
					panic("other than firing alert marked as processed!")
				}
				continue
			}
			newreasons[name] = info
		}
		if len(newreasons) > 0 {
			taints[dev] = newreasons
		}
	}

	return taints
}

// handleProcessedTaints goes through given node taints map, marks firing ones
// as processed and removes resolved ones from the internal mapping.
func (a *alerter) handleProcessedTaints(node string, gpus gpuTaints) {

	for dev, reasons := range gpus {
		for name, info := range reasons {
			switch info.status {
			case alertFiring:
				info.processed = true
				a.taints[node][dev][name] = info
			case alertResolved:
				delete(a.taints[node][dev], name)
			}
		}
		if len(a.taints[node][dev]) == 0 {
			delete(a.taints[node], dev)
		}
	}

	// Note: node entries with no gpuTaints are still left to a.taints
	// as they that their gpuTaints have been already pre-fetched from
	// given node GAS (by a.updateReasons()),
}
