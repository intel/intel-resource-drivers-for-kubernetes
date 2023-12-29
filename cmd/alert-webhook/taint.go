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
	"context"
	"fmt"
	"os"
	"sync"

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

// Set given taint status to all GPUs on indicated node, no reason => remove taints.
func (t *tainter) setTaintsFromFlags(f *cliFlags) error {
	klog.V(5).Info("setTaintsFromFlags()")
	if f.node == nil || *f.node == "" {
		klog.V(5).Info("No node given which GPUs should be un/tainted")
		return nil
	}

	// CRD access serialization
	t.mutex.Lock()
	defer t.mutex.Unlock()

	node := *f.node
	crdconfig := &intelcrd.GpuAllocationStateConfig{
		Namespace: t.nsname,
		Name:      node,
	}

	klog.V(5).Infof("New GAS for node '%s' in '%s' ns", node, t.nsname)
	gas := intelcrd.NewGpuAllocationState(crdconfig, t.clientset.intel)

	reason := ""
	if f.reason != nil {
		reason = *f.reason
	}

	updateErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {

		err := gas.Get(t.ctx)
		if err != nil {
			return fmt.Errorf("failed to get GAS CRD for '%s' in '%s': %v", node, t.nsname, err)
		}

		klog.V(3).Infof("Updating node '%s' CR taints for all GPUs to '%s' (in '%s' ns)", node, reason, t.nsname)

		if reason == "" {
			// no health issue => unset node taints
			if gas.Spec.TaintedDevices == nil {
				klog.V(3).Info("Nothing to untaint")
				return nil
			}

			// Remove all node taints & sync CRD
			gas.Spec.TaintedDevices = nil
			if err = gas.Update(t.ctx, &gas.Spec); err != nil {
				return fmt.Errorf("updating node '%s' GAS CRD: %v", node, err)
			}

			klog.V(3).Infof("Removed all node '%s' GPU taints", node)
			return nil
		}

		if gas.Spec.AllocatableDevices == nil {
			klog.V(3).Infof("Node '%s' has no GPU devices to taint", node)
			return nil
		}

		// Set all given node GPUs to be tainted (override earlier mapping)
		gas.Spec.TaintedDevices = intelcrd.TaintedDevices{}
		for uid := range gas.Spec.AllocatableDevices {
			// no need to taint VFs, PFs are enough
			if gas.Spec.AllocatableDevices[uid].Type == intelcrd.VfDeviceType {
				continue
			}
			klog.V(3).Infof("Tainting node '%s' GPU '%s' with: '%s'", node, uid, reason)
			gas.Spec.TaintedDevices[uid] = intelcrd.TaintedGpu{Reason: reason}
		}

		if err = gas.Update(t.ctx, &gas.Spec); err != nil {
			return fmt.Errorf("updating node '%s' GAS CRD: %v", node, err)
		}

		return nil
	})

	return updateErr
}

// return given uid if PF, VF's PF otherwise, or error if PF not found.
func mapVfToParent(gas *intelcrd.GpuAllocationState, uid string) (string, error) {

	if gas.Spec.AllocatableDevices[uid].Type != intelcrd.VfDeviceType {
		return uid, nil
	}

	parent := gas.Spec.AllocatableDevices[uid].ParentUID
	if _, found := gas.Spec.AllocatableDevices[parent]; !found {
		return "", fmt.Errorf("VF '%v' parent '%v' missing", uid, parent)
	}

	if gas.Spec.AllocatableDevices[parent].Type == intelcrd.VfDeviceType {
		return "", fmt.Errorf("VF '%v' parent '%v' is also VF", uid, parent)
	}

	return parent, nil

}

// Taint all listed GPUs for given node.
func (t *tainter) setNodeTaints(node string, taints gpuTaints) error {
	klog.V(5).Info("setNodeTaints()")
	// TODO: add tests with notifications that miss these
	if node == "" || len(taints) == 0 {
		panic("setNodeTaints: no node or GPUs to update")
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

	updateErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {

		err := gas.Get(t.ctx)
		if err != nil {
			return fmt.Errorf("failed to get GAS CRD for '%s' in '%s': %v", node, t.nsname, err)
		}

		if gas.Spec.TaintedDevices == nil {
			gas.Spec.TaintedDevices = intelcrd.TaintedDevices{}
		}

		changed := false
		for uid, reason := range taints {
			// no corresponding GPU?
			if _, found := gas.Spec.AllocatableDevices[uid]; !found {
				klog.V(3).Infof("Cannot taint, '%s' node has no allocatable '%s' GPU", node, uid)
				continue
			}

			// if VF, taint VF' PF instead
			uid, err = mapVfToParent(gas, uid)
			if err != nil {
				return err
			}

			// already up to date?
			status, found := gas.Spec.TaintedDevices[uid]
			if found && status.Reason == reason {
				continue
			}

			klog.V(3).Infof("Tainting node '%s' GPU '%s' with '%s'", node, uid, reason)
			gas.Spec.TaintedDevices[uid] = intelcrd.TaintedGpu{Reason: reason}
			changed = true
		}

		if !changed {
			return nil
		}

		if err = gas.Update(t.ctx, &gas.Spec); err != nil {
			return fmt.Errorf("updating node '%s' GAS CRD: %v", node, err)
		}

		return nil
	})

	return updateErr
}
