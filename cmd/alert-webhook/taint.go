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
	"fmt"
	"os"
	"sync"
	"time"

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

// parseReason returns stripped reason string, and true if it was prefixed with "!".
func parseReason(value *string) (string, bool) {
	if value == nil || len(*value) < 2 {
		return "", false
	}

	reason := *value
	if reason[0] == '!' {
		return reason[1:], true
	}

	return reason, false
}

// Add given taint reason for all GPUs on indicated node.
// Remove reason from them if reason name is prefixed with '!'.
func (t *tainter) setTaintsFromFlags(f *cliFlags) error {
	klog.V(5).Info("setTaintsFromFlags()")
	if f.node == nil || *f.node == "" {
		klog.V(5).Info("No node given which GPUs should be un/tainted")
		return nil
	}

	node := *f.node
	reason, untaint := parseReason(f.reason)
	if reason == "" {
		klog.V(5).Infof("Missing or too short node '%s' un/taint reason", node)
		return nil
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

		if untaint {
			klog.V(3).Infof("Remove '%s' taint from all node '%s' GPUs (in '%s' ns)", reason, node, t.nsname)

			// Remove given taint reason from all node GPUs
			if !removeTaintFromAllGpus(&gas.Spec, node, reason) {
				klog.V(3).Infof("There were no '%s' taints on node '%s' GPUs (in '%s' ns)")
				return nil
			}

			if err := gas.Update(t.ctx, &gas.Spec); err != nil {
				return err
			}

			return nil
		}

		klog.V(3).Infof("Taint all node '%s' GPUs with '%s' (in '%s' ns)", node, reason, t.nsname)

		if gas.Spec.AllocatableDevices == nil {
			klog.V(3).Infof("Node '%s' has no GPU devices to taint", node)
			return nil
		}

		changed := false
		// Add given taint reason to all GPUs on given node
		for uid := range gas.Spec.AllocatableDevices {
			// no need to taint VFs, PFs are enough
			if gas.Spec.AllocatableDevices[uid].Type == intelcrd.VfDeviceType {
				continue
			}

			if addGpuTaint(&gas.Spec, node, uid, reason) {
				klog.V(3).Infof("Tainting node '%s' GPU '%s' with '%s'", node, uid, reason)
				changed = true
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

// getNodeTaints reads pre-existing taints from node's GAS CR, and returns
// gpu:reasonInfo taint map created from it, or nil if node did not have GPUs
func (t *tainter) getNodeTaints(node string, start time.Time) gpuTaints {
	klog.V(5).Info("getNodeTaints()")
	if node == "" {
		panic("getNodeTaints: no node or GPUs to update")
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

// removeTaintFromAllGpus removes given taint reason from all of node's GPUs in GAS spec,
// or returns false if no GPU taints needed to be removed.
func removeTaintFromAllGpus(spec *intelcrd.GpuAllocationStateSpec, node, reason string) bool {
	if spec.TaintedDevices == nil {
		return false
	}

	changed := false
	for uid := range spec.TaintedDevices {
		if removeGpuTaint(spec, node, uid, reason) {
			changed = true
		}
	}

	return changed
}
