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
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"strings"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	coreclientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"

	intelclientset "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gpu/clientset/versioned"
	intelcrd "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gpu/v1alpha2/api"
)

type clientsetType struct {
	core  coreclientset.Interface
	intel intelclientset.Interface
}

// Tainter updates GpuAllocationState CR tainting section.
// Except for mutex, members are not updated after newTainter() call.
type tainter struct {
	ctx       context.Context
	nsname    string
	csconfig  *rest.Config
	clientset *clientsetType
	mutex     sync.Mutex
}

func newTainter(ctx context.Context, kubeconfig string) (*tainter, error) {
	klog.V(5).Info("newTainter()")

	csconfig, err := getClientsetConfig(kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("create client configuration: %v", err)
	}

	coreclient, err := coreclientset.NewForConfig(csconfig)
	if err != nil {
		return nil, fmt.Errorf("create core client: %v", err)
	}

	intelclient, err := intelclientset.NewForConfig(csconfig)
	if err != nil {
		return nil, fmt.Errorf("create Intel client: %v", err)
	}

	nsname, nsnamefound := os.LookupEnv("POD_NAMESPACE")
	if !nsnamefound {
		nsname = "default"
	}

	tainter := &tainter{
		ctx:      ctx,
		nsname:   nsname,
		csconfig: csconfig,
		clientset: &clientsetType{
			coreclient,
			intelclient,
		},
		mutex: sync.Mutex{},
	}

	klog.V(5).Infof("Tainter initialized: %+v", tainter)
	return tainter, nil
}

func getClientsetConfig(kubeconfig string) (*rest.Config, error) {
	klog.V(5).Info("getClientsetConfig()")

	kubeconfigEnv := os.Getenv("KUBECONFIG")
	if kubeconfigEnv != "" {
		klog.V(5).Info("Found KUBECONFIG environment variable set, using that...")
		kubeconfig = kubeconfigEnv
	}

	var csconfig *rest.Config
	var err error

	if kubeconfig == "" {
		csconfig, err = rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("create in-cluster client configuration: %v", err)
		}
	} else {
		csconfig, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("create out-of-cluster client configuration: %v", err)
		}
	}

	return csconfig, nil
}

// convert string with comma separated items to map[name]false, with nil indicating "all" items.
func string2map(value *string) (map[string]bool, error) {
	if value == nil || *value == "" {
		return nil, errors.New("invalid (empty) option value")
	}
	if *value == "all" {
		return nil, nil
	}
	items := make(map[string]bool)
	for _, name := range strings.Split(*value, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, errors.New("invalid comma separated (empty) option value")
		}
		items[name] = false
	}
	return items, nil
}

// if nodes names are specified, return those as a map[name]false,
// otherwise fetch & return that mapping for all cluster nodes,
// and a bool to indicate whether all were returned + error.
func (t *tainter) expandNodes(value *string) (map[string]bool, bool, error) {
	all := false

	nodes, err := string2map(value)
	if nodes != nil || err != nil {
		return nodes, all, err
	}

	// get all node names from cluster
	all = true

	items, err := t.clientset.core.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nodes, all, fmt.Errorf("cluster nodes List() call failed: %v", err)
	}

	nodes = make(map[string]bool, len(items.Items))
	for _, node := range items.Items {
		nodes[node.Name] = false
	}

	return nodes, all, nil
}

const (
	actionList    = "list"
	actionTaint   = "taint"
	actionUntaint = "untaint"
)

type taintInfoType struct {
	reasons map[string]bool // unique taint reasons
	devices int             // total devices count
	tainted int             // tainted devices count
}

// initially all map keys are false, and later set to true, if matched.
type taintArgsType struct {
	// 'nil' value = all items
	devices map[string]bool // which devices to taint/untaint/list
	reasons map[string]bool // which reasons to use for tainting, or to untaint/list
	// on which nodes to act
	nodes    map[string]bool
	allNodes bool
}

// Depending on CLI flags, list or update specified (or all) taint reasons for
// specified (or all) devices on specified (or all) nodes.
func (t *tainter) setTaintsFromFlags(f *cliFlags) error {
	klog.V(5).Info("setTaintsFromFlags()")
	if f.action == nil || *f.action == "" {
		klog.V(5).Info("No CLI action requested")
		return nil
	}

	action := *f.action
	if action != actionList && action != actionTaint && action != actionUntaint {
		return fmt.Errorf("invalid CLI action '%s'", action)
	}

	var err error
	args := taintArgsType{}

	if args.nodes, args.allNodes, err = t.expandNodes(f.nodes); err != nil {
		return fmt.Errorf("nodes mapping for action '%s' failed: %v", action, err)
	}

	if args.reasons, err = string2map(f.reasons); err != nil {
		return fmt.Errorf("taint reasons mapping for action '%s' failed: %v", action, err)
	}

	if args.devices, err = string2map(f.devices); err != nil {
		return fmt.Errorf("devices mapping for action '%s' failed: %v", action, err)
	}

	if action == actionTaint && args.reasons == nil {
		return fmt.Errorf("no reasons specified for tainting")
	}

	info := taintInfoType{
		reasons: make(map[string]bool),
	}

	for node := range args.nodes {
		if err := t.handleNodeAction(&args, &info, action, node); err != nil {
			return err
		}
	}

	klog.Info("DONE!")
	taintInfoSummary(args, info, action)

	return nil
}

func logMatchInfo(kinds string, items map[string]bool) {
	if items == nil {
		return
	}

	klog.Infof("Specified %s:", kinds)
	missing := 0

	for name, found := range items {
		if !found {
			klog.Infof("- %s: NO MATCH", name)
			missing++
		}
	}

	if missing == 0 {
		klog.Info("- all matched")
	}
}

func taintInfoSummary(args taintArgsType, info taintInfoType, action string) {
	if !args.allNodes {
		logMatchInfo("nodes", args.nodes)
	}
	logMatchInfo("devices", args.devices)
	logMatchInfo("reasons", args.reasons)

	if action != actionList {
		return
	}

	klog.Info("Summary:")
	if info.devices == 0 {
		klog.Infof("- No (matching) devices on specified %d nodes", len(args.nodes))
		return
	}
	klog.Infof("- %d devices on %d nodes", info.devices, len(args.nodes))

	if info.tainted == 0 {
		if len(info.reasons) > 0 {
			panic("taint reasons not empty, although tainted dev count = 0")
		}
		klog.Info("- None tainted (matching specified devices/reasons)")
		return
	}

	if len(info.reasons) == 0 {
		panic("taint reasons is empty, although tainted dev count != 0")
	}

	klog.Infof("- %d of them tainted", info.tainted)

	klog.Info("Unique taint reasons:")
	for name := range info.reasons {
		klog.Infof("- %s", name)
	}
}

func (t *tainter) handleNodeAction(args *taintArgsType, info *taintInfoType, action, node string) error {
	// CRD access serialization
	t.mutex.Lock()
	defer t.mutex.Unlock()

	crdconfig := &intelcrd.GpuAllocationStateConfig{
		Namespace: t.nsname,
		Name:      node,
	}

	klog.V(5).Infof("New '%s' action for '%s' node in '%s' ns", action, node, t.nsname)
	gas := intelcrd.NewGpuAllocationState(crdconfig, t.clientset.intel)

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {

		if err := gas.Get(t.ctx); err != nil {
			return nil
		}

		args.nodes[node] = true

		var changed bool
		switch action {
		case actionList:
			return t.listNodeTaints(args, info, &gas.Spec, node)
		case actionTaint:
			klog.V(3).Infof("Taint node '%s' GPUs with specified reasons", node)
			changed = addNodeTaints(args, &gas.Spec, node)
		case actionUntaint:
			klog.V(3).Infof("Remove specified taint reasons from node '%s' GPUs", node)
			changed = removeNodeTaints(args, &gas.Spec, node)
		default:
			panic(fmt.Sprintf("unknown action %v", action)) // bug in caller
		}

		if !changed {
			klog.V(3).Info("=> No changes needed")
			return nil
		}

		return gas.Update(t.ctx, &gas.Spec)
	})
}

// add specified taint reasons for specified devices (nil=all) on node.
func addNodeTaints(args *taintArgsType, spec *intelcrd.GpuAllocationStateSpec, node string) bool {
	changed := false
	if spec.AllocatableDevices == nil {
		return changed
	}

	// Add given taint reason to specified GPUs on given node
	for uid := range spec.AllocatableDevices {
		// no need to taint VFs, PFs are enough
		if spec.AllocatableDevices[uid].Type == intelcrd.VfDeviceType {
			continue
		}

		if args.devices != nil {
			if !args.devices[uid] {
				continue
			}
			args.devices[uid] = true
		}

		for reason := range args.reasons {
			if addGpuTaint(spec, node, uid, reason) {
				args.reasons[reason] = true
				changed = true
			}
		}
	}

	return changed
}

// remove specified taint reasons (nil=all) for specified devices (nil=all) on node.
func removeNodeTaints(args *taintArgsType, spec *intelcrd.GpuAllocationStateSpec, node string) bool {
	changed := false
	if spec.TaintedDevices == nil {
		return changed
	}

	for uid := range spec.TaintedDevices {
		if args.devices != nil {
			if !args.devices[uid] {
				continue
			}
			args.devices[uid] = true
		}

		if args.reasons == nil {
			// remove all reasons
			delete(spec.TaintedDevices, uid)

			if len(spec.TaintedDevices) == 0 {
				spec.TaintedDevices = nil
			}
			changed = true
			continue
		}

		for reason := range args.reasons {
			if removeGpuTaint(spec, node, uid, reason) {
				args.reasons[reason] = true
				changed = true
			}
		}
	}

	return changed
}

// list all available devices and their taint reasons on given node, filtered by
// given devices + reasons lists. Output warnings on invalid taint information.
func (t *tainter) listNodeTaints(args *taintArgsType, info *taintInfoType, spec *intelcrd.GpuAllocationStateSpec, node string) error {
	klog.Infof("%s:", node)

	checkTaints(spec)

	if spec.AllocatableDevices == nil {
		klog.Info("- NO devices")
		return nil
	}

	total := 0
	tainted := 0
	unique := make(map[string]bool)

	for uid := range spec.AllocatableDevices {
		if args.devices != nil {
			if !args.devices[uid] {
				continue
			}
			args.devices[uid] = true
		}
		total++

		if spec.TaintedDevices == nil {
			klog.Infof("- %s", uid)
			continue
		}

		taint, found := spec.TaintedDevices[uid]
		if !found || len(taint.Reasons) == 0 {
			klog.Infof("- %s", uid)
			if found {
				klog.Info("  - WARN: empty (instead of missing) taint reasons")
			}
			continue
		}

		names := make([]string, 0)

		if args.reasons != nil {
			// filtered list of reasons
			for reason := range args.reasons {
				if _, found := taint.Reasons[reason]; found {
					names = append(names, reason)
					args.reasons[reason] = true
					unique[reason] = true
				}
			}
		} else {
			// all reasons
			for reason := range taint.Reasons {
				names = append(names, reason)
				unique[reason] = true
			}
		}

		if len(names) > 0 {
			tainted++
		}

		klog.Infof("- %s: %v", uid, names)
	}

	if len(unique) > 0 {
		maps.Copy(info.reasons, unique)
	}
	info.tainted += tainted
	info.devices += total

	return nil
}

// check taint info against available device info and warn of mismatches.
func checkTaints(spec *intelcrd.GpuAllocationStateSpec) {
	if spec.TaintedDevices == nil {
		return
	}

	if len(spec.TaintedDevices) == 0 {
		klog.Info("- WARN: empty (instead of missing) tainted devices list")
		return
	}

	if spec.AllocatableDevices == nil {
		klog.Infof("- WARN: %d tainted devices, although no available devices",
			len(spec.TaintedDevices))
		return
	}

	for uid := range spec.TaintedDevices {
		if _, found := spec.AllocatableDevices[uid]; found {
			continue
		}
		klog.Infof("- WARN: '%s' device listed as tainted, does not exist!", uid)
	}
}

// getNodeTaints reads pre-existing taints from node's GAS CR, and returns
// gpu:reasonInfo taint map created from it, or nil if node did not have GPUs
func (t *tainter) getNodeTaints(node string, start time.Time) gpuTaints {
	klog.V(5).Info("getNodeTaints()")
	if node == "" {
		panic("getNodeTaints: no node specified")
	}

	// CRD access serialization
	t.mutex.Lock()
	defer t.mutex.Unlock()

	crdconfig := &intelcrd.GpuAllocationStateConfig{
		Namespace: t.nsname,
		Name:      node,
	}

	klog.V(5).Infof("Get GAS for node '%s' in '%s' ns", node, t.nsname)
	gas := intelcrd.NewGpuAllocationState(crdconfig, t.clientset.intel)
	if err := gas.Get(t.ctx); err != nil {
		return nil
	}

	if gas.Spec.AllocatableDevices == nil {
		return nil
	}

	taints := make(gpuTaints)
	if gas.Spec.TaintedDevices == nil {
		return taints
	}

	// map string:bool to reasonInfo structs
	for uid, taint := range gas.Spec.TaintedDevices {
		taints[uid] = make(taintReasons)
		for name := range taint.Reasons {
			taints[uid][name] = reasonInfo{
				processed: true,
				status:    alertFiring,
				start:     start,
				name:      name,
			}
		}
	}

	klog.V(5).Infof("Pre-existing taints on node '%s': %+v", node, taints)
	return taints
}

// Update taint reasons for listed GPUs on given node.
func (t *tainter) updateNodeTaints(node string, taints gpuTaints) error {
	klog.V(5).Info("tainter.updateNodeTaints()")
	// TODO: add tests with notifications that miss these
	if node == "" || len(taints) == 0 {
		panic("updateNodeTaints: no node or GPUs to update")
	}

	// CRD access serialization
	t.mutex.Lock()
	defer t.mutex.Unlock()

	crdconfig := &intelcrd.GpuAllocationStateConfig{
		Namespace: t.nsname,
		Name:      node,
	}

	klog.V(5).Infof("New GAS for node '%s' in '%s' ns", node, t.nsname)
	gas := intelcrd.NewGpuAllocationState(crdconfig, t.clientset.intel)

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {

		if err := gas.Get(t.ctx); err != nil {
			return err
		}

		changed := false
		for uid, reasons := range taints {
			// no corresponding GPU?
			if _, found := gas.Spec.AllocatableDevices[uid]; !found {
				klog.V(3).Infof("Cannot update taints, '%s' node has no allocatable '%s' GPU", node, uid)
				continue
			}

			// VF may have changed as alerts are for longer period conditions
			// and VFs are transitory.  While taint could be mapped to PF if
			// some VF with a same UID still exits, that's not given. And lastly,
			// metrics for VFs are either same as for PF, or less relevant for
			// health, so it should be safe to ignore them.
			if gas.Spec.AllocatableDevices[uid].Type == intelcrd.VfDeviceType {
				klog.V(3).Infof("Ignoring alert notifications for node '%s' VF '%s'", node, uid)
				continue
			}

			for name, info := range reasons {
				var (
					updated bool
					verb    string
				)
				switch info.status {
				case alertFiring:
					updated = addGpuTaint(&gas.Spec, node, uid, name)
					verb = "added"
				case alertResolved:
					updated = removeGpuTaint(&gas.Spec, node, uid, name)
					verb = "removed"
				default:
					panic("unknown alert state")
				}

				if updated {
					klog.V(3).Infof("'%s' node '%s' GPU '%s' taint %s",
						node, uid, name, verb)
					if gas.Spec.TaintedDevices == nil {
						klog.V(3).Infof("Taints removed from all GPUs on node '%s'", node)
					}
					changed = true
				}
			}
		}

		if !changed {
			return nil
		}

		if err := gas.Update(t.ctx, &gas.Spec); err != nil {
			return err
		}

		return nil
	})
}

// addGpuTaint updates GAS spec by setting given taint reason for to given device
// in tainted devices map, or returns false if taint is already there.
func addGpuTaint(spec *intelcrd.GpuAllocationStateSpec, node, uid, reason string) bool {
	reasons := make(map[string]bool)
	reasons[reason] = true

	if spec.TaintedDevices == nil {
		spec.TaintedDevices = intelcrd.TaintedDevices{}
	}

	taint, found := spec.TaintedDevices[uid]
	if !found {
		spec.TaintedDevices[uid] = intelcrd.TaintedGpu{Reasons: reasons}
		return true
	}

	if taint.Reasons == nil || len(taint.Reasons) == 0 {
		klog.Warningf("node '%s' GPU '%s' has empty taint reasons map", node, uid)
		spec.TaintedDevices[uid] = intelcrd.TaintedGpu{Reasons: reasons}
		return true
	}

	if !taint.Reasons[reason] {
		spec.TaintedDevices[uid].Reasons[reason] = true
		return true
	}

	klog.V(5).Infof("Node '%s' GPU '%s' already tainted with '%s'", node, uid, reason)
	return false
}

// removeGpuTaint removes given taint reason from given GPU in GAS spec,
// or returns false if given taint reason was already missing.
func removeGpuTaint(spec *intelcrd.GpuAllocationStateSpec, node, uid, reason string) bool {
	if spec.TaintedDevices == nil {
		return false
	}

	taint, found := spec.TaintedDevices[uid]
	if !found {
		return false
	}

	if taint.Reasons == nil || len(taint.Reasons) == 0 {
		klog.Warningf("Removed empty taint map for node '%s' GPU '%s'", node, uid)
		delete(spec.TaintedDevices, uid)
		return true
	}

	if _, found = taint.Reasons[reason]; !found {
		// no match for resolved taint reason
		return false
	}

	if len(taint.Reasons) > 1 {
		delete(spec.TaintedDevices[uid].Reasons, reason)
		return true
	}

	// all taint reasons for given GPU gone
	delete(spec.TaintedDevices, uid)
	if len(spec.TaintedDevices) == 0 {
		spec.TaintedDevices = nil
	}
	return true
}
