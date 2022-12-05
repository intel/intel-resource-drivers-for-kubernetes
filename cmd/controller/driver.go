/*
 * Copyright (c) 2022, Intel Corporation.  All Rights Reserved.
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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	resourcev1alpha1 "k8s.io/api/resource/v1alpha1"
	"k8s.io/dynamic-resource-allocation/controller"
	"k8s.io/klog/v2"

	intelclientset "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/crd/intel/clientset/versioned"
	intelcrd "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/crd/intel/v1alpha/api"
	driverVersion "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/version"
)

const (
	apiGroupVersion = intelcrd.ApiGroupName + "/" + intelcrd.ApiVersion
	minMemory       = 8
)

type driver struct {
	lock                 *PerNodeMutex
	namespace            string
	clientset            intelclientset.Interface
	PendingClaimRequests *PerNodeClaimRequests
}

type onSuccessCallback func()

var _ controller.Driver = (*driver)(nil)

func newDriver(config *config_t) *driver {
	klog.Infof("Creating new driver")
	driverVersion.PrintDriverVersion()

	return &driver{
		lock:                 NewPerNodeMutex(),
		namespace:            config.namespace,
		clientset:            config.clientset.intel,
		PendingClaimRequests: NewPerNodeClaimRequests(),
	}
}

func (d driver) GetClassParameters(ctx context.Context, class *resourcev1alpha1.ResourceClass) (interface{}, error) {
	klog.V(5).InfoS("GetClassParameters called", "resource class", class.Name)

	if class.ParametersRef == nil {
		return intelcrd.DefaultDeviceClassParametersSpec(), nil
	}
	if class.ParametersRef.APIGroup != apiGroupVersion {
		return nil, fmt.Errorf(
			"incorrect resource-class API group and version: %v, expected: %v",
			class.ParametersRef.APIGroup,
			apiGroupVersion)
	}
	dc, err := d.clientset.GpuV1alpha().DeviceClassParameters().Get(ctx, class.ParametersRef.Name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("could not get DeviceClassParameters '%v': %v", class.ParametersRef.Name, err)
	}
	return &dc.Spec, nil
}

func (d driver) GetClaimParameters(ctx context.Context, claim *resourcev1alpha1.ResourceClaim, class *resourcev1alpha1.ResourceClass, classParameters interface{}) (interface{}, error) {
	klog.V(5).InfoS("GetClaimParameters called", "resource claim", claim.Namespace+"/"+claim.Name)
	if claim.Spec.ParametersRef == nil {
		return intelcrd.DefaultGpuClaimParametersSpec(), nil
	}

	if claim.Spec.ParametersRef.APIGroup != apiGroupVersion {
		return nil, fmt.Errorf(
			"incorrect claim spec parameter API group and version: %v, expected: %v",
			claim.Spec.ParametersRef.APIGroup,
			apiGroupVersion)
	}
	switch claim.Spec.ParametersRef.Kind {
	case intelcrd.GpuClaimParametersKind:
		gcp, err := d.clientset.GpuV1alpha().GpuClaimParameters(claim.Namespace).Get(ctx, claim.Spec.ParametersRef.Name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("could not get GpuClaimParameters '%v' in namespace '%v': %v", claim.Spec.ParametersRef.Name, claim.Namespace, err)
		}
		err = validateGpuClaimParameters(&gcp.Spec)
		if err != nil {
			return nil, fmt.Errorf("could not validate GpuClaimParameters '%v' in namespace '%v': %v", claim.Spec.ParametersRef.Name, claim.Namespace, err)
		}
		return &gcp.Spec, nil
	}

	return nil, fmt.Errorf("unknown ResourceClaim.ParametersRef.Kind: %v", claim.Spec.ParametersRef.Kind)
}

// Sanitize resource request
func validateGpuClaimParameters(claimParams *intelcrd.GpuClaimParametersSpec) error {
	klog.V(5).Infof("validateGpuClaimParameters called")
	// Count is mandatory
	if claimParams.Count < 1 {
		return fmt.Errorf("invalid number of GPUs requested: %v", claimParams.Count)
	}
	// type is mandatory
	if claimParams.Type != intelcrd.GpuDeviceType {
		return fmt.Errorf("unsupported device type requested: %v", claimParams.Type)
	}
	// Memory is not mandatory
	if claimParams.Memory != 0 && claimParams.Memory < minMemory {
		return fmt.Errorf("invalid number of Memory requested: %v", claimParams.Memory)
	}

	return nil
}

func (d driver) Allocate(
	ctx context.Context,
	claim *resourcev1alpha1.ResourceClaim,
	claimParameters interface{},
	class *resourcev1alpha1.ResourceClass,
	classParameters interface{},
	selectedNode string) (*resourcev1alpha1.AllocationResult, error) {
	klog.V(5).InfoS("Allocate called", "resource claim", claim.Namespace+"/"+claim.Name, "selectedNode", selectedNode)

	if selectedNode == "" { // immediate allocation with no pendingResourceClaims
		klog.V(5).Infof("Allocating immediately")

		crdconfig := &intelcrd.GpuAllocationStateConfig{
			Namespace: d.namespace,
		}

		gas := intelcrd.NewGpuAllocationState(crdconfig, d.clientset)
		gasnames, err := gas.ListNames()
		if err != nil {
			return nil, fmt.Errorf("error retrieving GAS CRD for node %v: %v", selectedNode, err)
		}

		// create claimAllocation
		ca := controller.ClaimAllocation{
			Claim:           claim,
			ClaimParameters: claimParameters,
			Class:           class,
			ClassParameters: classParameters,
		}
		cas := []*controller.ClaimAllocation{&ca}

		for _, nodename := range gasnames {
			klog.V(5).Infof("Fetching gas item: %v", nodename)
			d.lock.Get(nodename).Lock()

			crdconfig.Name = nodename
			gas := intelcrd.NewGpuAllocationState(crdconfig, d.clientset)
			err := gas.Get()
			if err != nil {
				d.lock.Get(nodename).Unlock()
				klog.Errorf("error retrieving GAS CRD for node %v: %v", nodename, err)
				continue
			}

			allocated := d.selectPotentialGpus(gas, cas)
			klog.V(5).Infof("Allocated: %v", allocated)
			claimUID := string(claim.UID)
			claimParamsSpec := claimParameters.(*intelcrd.GpuClaimParametersSpec)

			if claimParamsSpec.Count != len(allocated[claimUID]) {
				d.lock.Get(nodename).Unlock()
				klog.V(3).Infof("Requested does not match allocated, skipping node %v", nodename)
				continue // next node
			}

			klog.V(5).Infof("Allocated as much as requested, processing devices")
			var devices []intelcrd.RequestedGpu
			for _, gpu := range allocated[claimUID] {
				device := intelcrd.RequestedGpu{
					UUID: gpu,
				}
				devices = append(devices, device)
			}

			requestedDevices := intelcrd.RequestedDevices{
				Spec: *claimParamsSpec,
				GPUs: devices,
			}

			if gas.Spec.ResourceClaimRequests == nil {
				gas.Spec.ResourceClaimRequests = make(map[string]intelcrd.RequestedDevices)
			}
			gas.Spec.ResourceClaimRequests[claimUID] = requestedDevices

			allocation := intelcrd.AllocatedDevices{}
			for _, device := range gas.Spec.ResourceClaimRequests[claimUID].GPUs {
				sourceDevice := gas.Spec.AllocatableGpus[device.UUID]
				allocation = append(allocation, intelcrd.AllocatedGpu{
					CDIDevice: sourceDevice.CDIDevice,
					Type:      sourceDevice.Type,
					UUID:      sourceDevice.UUID,
					Memory:    gas.Spec.ResourceClaimRequests[claimUID].Spec.Memory,
				})
			}

			if gas.Spec.ResourceClaimAllocations == nil {
				gas.Spec.ResourceClaimAllocations = make(map[string]intelcrd.AllocatedDevices)
			}
			gas.Spec.ResourceClaimAllocations[claimUID] = allocation

			err = gas.Update(&gas.Spec)
			if err != nil {
				d.lock.Get(nodename).Unlock()
				klog.Error("Could not update GpuAllocationState %v. Error: %+v", gas.Name, err)
				return nil, fmt.Errorf("error updating GpuAllocationState CRD: %v", err)
			}

			d.lock.Get(nodename).Unlock()
			return buildAllocationResult(nodename, true), nil
		}

		klog.V(3).InfoS("Could not immediately allocate", "resource claim", claim.Namespace+"/"+claim.Name)
		return nil, fmt.Errorf("no suitable node found")
	}

	return d.allocatePendingClaim(claim, claimParameters, selectedNode)
}

func (d driver) allocatePendingClaim(
	claim *resourcev1alpha1.ResourceClaim,
	claimParameters interface{},
	nodename string) (*resourcev1alpha1.AllocationResult, error) {

	d.lock.Get(nodename).Lock()
	defer d.lock.Get(nodename).Unlock()

	crdconfig := &intelcrd.GpuAllocationStateConfig{
		Name:      nodename,
		Namespace: d.namespace,
	}

	gas := intelcrd.NewGpuAllocationState(crdconfig, d.clientset)
	err := gas.Get()
	if err != nil {
		return nil, fmt.Errorf("error retrieving GAS CRD for node %v: %v", nodename, err)
	}

	if gas.Spec.ResourceClaimRequests == nil {
		gas.Spec.ResourceClaimRequests = make(map[string]intelcrd.RequestedDevices)
	} else if _, exists := gas.Spec.ResourceClaimRequests[string(claim.UID)]; exists {
		klog.V(5).Infof("GAS already has ClaimRequest %v, building allocation result", claim.UID)
		return buildAllocationResult(nodename, true), nil
	}

	if gas.Status != intelcrd.GpuAllocationStateStatusReady {
		return nil, fmt.Errorf("GpuAllocationStateStatus: %v", gas.Status)
	}

	var onSuccess onSuccessCallback
	switch claimParameters.(type) {
	case *intelcrd.GpuClaimParametersSpec:
		claimUID := string(claim.UID)
		if claim.Spec.AllocationMode != resourcev1alpha1.AllocationModeImmediate && !d.PendingClaimRequests.Exists(claimUID, nodename) {
			err = fmt.Errorf(
				"no allocation requests generated for claim '%v' on node '%v' yet",
				claim.UID, nodename)
		}
		// validate that there is still resource for it
		enoughResource := d.enoughResourcesForPendingClaim(gas, claimUID, nodename)
		if enoughResource {
			klog.V(5).Infof("enough resources. Setting GAS ClaimRequest %v", claimUID)
			gas.Spec.ResourceClaimRequests[claimUID] = d.PendingClaimRequests.Get(claimUID, nodename)
			allocated := intelcrd.AllocatedDevices{}
			for _, device := range gas.Spec.ResourceClaimRequests[claimUID].GPUs {
				sourceDevice := gas.Spec.AllocatableGpus[device.UUID]
				allocated = append(allocated, intelcrd.AllocatedGpu{
					CDIDevice: sourceDevice.CDIDevice,
					Type:      sourceDevice.Type,
					UUID:      sourceDevice.UUID,
					Memory:    gas.Spec.ResourceClaimRequests[claimUID].Spec.Memory,
				})
			}
			if gas.Spec.ResourceClaimAllocations == nil {
				gas.Spec.ResourceClaimAllocations = make(map[string]intelcrd.AllocatedDevices)
			}
			gas.Spec.ResourceClaimAllocations[claimUID] = allocated
			onSuccess = func() {
				d.PendingClaimRequests.Remove(claimUID)
			}
		} else {
			klog.V(5).Infof("Insufficient resource for claim %v on allocation", claimUID)
			err = fmt.Errorf("insufficient resources for claim %v on node %v", claimUID, nodename)
		}
	// TODO: case SR-IOV params
	default:
		err = fmt.Errorf("unknown ResourceClaim.ParametersRef.Kind: %v", claim.Spec.ParametersRef.Kind)
	}
	if err != nil {
		return nil, fmt.Errorf("unable to allocate devices on node '%v': %v", nodename, err)
	}

	err = gas.Update(&gas.Spec)
	if err != nil {
		return nil, fmt.Errorf("error updating GpuAllocationState CRD: %v", err)
	}

	onSuccess()

	return buildAllocationResult(nodename, true), nil
}

func (d driver) Deallocate(ctx context.Context, claim *resourcev1alpha1.ResourceClaim) error {
	klog.V(5).InfoS("Deallocate called", "resource claim", claim.Namespace+"/"+claim.Name)
	selectedNode := getSelectedNode(claim)
	if selectedNode == "" {
		return nil
	}

	d.lock.Get(selectedNode).Lock()
	defer d.lock.Get(selectedNode).Unlock()

	crdconfig := &intelcrd.GpuAllocationStateConfig{
		Name:      selectedNode,
		Namespace: d.namespace,
	}

	gas := intelcrd.NewGpuAllocationState(crdconfig, d.clientset)
	err := gas.Get()
	if err != nil {
		return fmt.Errorf("error retrieving GAS CRD for node %v: %v", selectedNode, err)
	}

	claimUID := string(claim.UID)
	devices := gas.Spec.ResourceClaimRequests[claimUID]
	switch devices.Spec.Type {
	case intelcrd.GpuDeviceType:
		d.PendingClaimRequests.Remove(claimUID)
	default:
		err = fmt.Errorf("unknown RequestedDevices.Type(): %v", devices.Spec.Type)
	}
	if err != nil {
		return fmt.Errorf("unable to deallocate devices '%v': %v", devices, err)
	}

	if gas.Spec.ResourceClaimRequests != nil {
		delete(gas.Spec.ResourceClaimRequests, claimUID)
	}
	if gas.Spec.ResourceClaimAllocations != nil {
		delete(gas.Spec.ResourceClaimAllocations, claimUID)
	}

	err = gas.Update(&gas.Spec)
	if err != nil {
		return fmt.Errorf("error updating GpuAllocationState CRD: %v", err)
	}
	return nil
}

// Unsuitable nodes call chain
// mark nodes that do not suit request into .UnsuitableNodes and populate d.PendingClaimAllocations
func (d driver) UnsuitableNodes(ctx context.Context, pod *corev1.Pod, cas []*controller.ClaimAllocation, potentialNodes []string) error {
	klog.V(5).InfoS("UnsuitableNodes called", "cas length", len(cas))

	for _, node := range potentialNodes {
		klog.V(5).InfoS("UnsuitableNodes processing", "node", node)
		err := d.unsuitableNode(cas, node)
		if err != nil {
			return fmt.Errorf("error processing node '%v': %v", node, err)
		}
	}

	// remove duplicates from UnsuitableNodes
	for _, claimallocation := range cas {
		claimallocation.UnsuitableNodes = unique(claimallocation.UnsuitableNodes)
	}
	return nil
}

func (d driver) unsuitableNode(allcas []*controller.ClaimAllocation, potentialNode string) error {
	d.lock.Get(potentialNode).Lock()
	defer d.lock.Get(potentialNode).Unlock()

	crdconfig := &intelcrd.GpuAllocationStateConfig{
		Name:      potentialNode,
		Namespace: d.namespace,
	}

	gas := intelcrd.NewGpuAllocationState(crdconfig, d.clientset)
	klog.V(5).InfoS("Getting GPU allocation state", "node", potentialNode, "namespace", d.namespace)
	err := gas.Get()
	if err != nil {
		klog.V(3).Infof("Could not get node %v allocation state", potentialNode)
		for _, ca := range allcas {
			klog.V(5).Infof("Adding node %v to unsuitable nodes for CA %v", potentialNode, ca)
			ca.UnsuitableNodes = append(ca.UnsuitableNodes, potentialNode)
		}
		return nil
	}
	klog.V(5).InfoS("Success getting GPU allocation state", "node", potentialNode, "GAS", gas)

	if gas.Status != intelcrd.GpuAllocationStateStatusReady {
		klog.V(3).Infof("GAS status not ready, adding node %v to unsuitable nodes", potentialNode)
		for _, ca := range allcas {
			ca.UnsuitableNodes = append(ca.UnsuitableNodes, potentialNode)
		}
		return nil
	}
	klog.V(5).Infof("GAS status OK")

	if gas.Spec.ResourceClaimRequests == nil {
		klog.V(5).Infof("Creating blank map for claim requests")
		gas.Spec.ResourceClaimRequests = make(map[string]intelcrd.RequestedDevices)
	}

	// TODO: split requests per kind to forward to respective driver: might not be needed
	perKindCas := make(map[string][]*controller.ClaimAllocation)
	klog.V(5).Infof("Compiling per-kind-CAS")
	for _, ca := range allcas {
		var kind string
		switch ca.ClaimParameters.(type) {
		case *intelcrd.GpuClaimParametersSpec:
			klog.V(5).Info("Matched claim parameters type: gpu")
			kind = intelcrd.GpuClaimParametersKind
		default:
			klog.V(3).Infof("Unsupported claim parameters type %v", ca.ClaimParameters)
			continue
		}
		perKindCas[kind] = append(perKindCas[kind], ca)
	}

	err = d.unsuitableGpuNode(gas, perKindCas[intelcrd.GpuClaimParametersKind], allcas)
	if err != nil {
		return fmt.Errorf("error processing '%v': %v", intelcrd.GpuClaimParametersKind, err)
	}

	return nil
}

func (d *driver) unsuitableGpuNode(
	gas *intelcrd.GpuAllocationState,
	gpucas []*controller.ClaimAllocation,
	allcas []*controller.ClaimAllocation) error {
	klog.V(5).Infof("unsuitableGpuNode called")

	// remove pending claim requests that are in CRD already
	// Add pending claim requests to CRD
	d.PendingClaimRequests.CleanupNode(gas)
	allocated := d.selectPotentialGpus(gas, gpucas)
	klog.V(5).Infof("Allocated: %v", allocated)
	for _, ca := range gpucas {
		claimUID := string(ca.Claim.UID)
		claimParamsSpec := ca.ClaimParameters.(*intelcrd.GpuClaimParametersSpec)

		if claimParamsSpec.Count != len(allocated[claimUID]) {
			klog.V(3).Infof("Requested does not match allocated, skipping node")
			for _, ca := range allcas {
				ca.UnsuitableNodes = append(ca.UnsuitableNodes, gas.Name)
			}
			return nil
		}

		klog.V(5).Infof("Allocated as much as requested, processing devices")
		var devices []intelcrd.RequestedGpu
		for _, gpu := range allocated[claimUID] {
			device := intelcrd.RequestedGpu{
				UUID: gpu,
			}
			devices = append(devices, device)
		}

		requestedDevices := intelcrd.RequestedDevices{
			Spec: *claimParamsSpec,
			GPUs: devices,
		}

		d.PendingClaimRequests.Set(claimUID, gas.Name, requestedDevices)
		gas.Spec.ResourceClaimRequests[claimUID] = requestedDevices // useless without updating GAS
	}
	klog.V(5).Info("Leaving unsuitableGpuNode")
	return nil
}

// Allocate GPUs out of available for all claim allocations or fail
func (d *driver) selectPotentialGpus(
	gas *intelcrd.GpuAllocationState,
	gpucas []*controller.ClaimAllocation) map[string][]string {
	klog.V(5).Infof("selectPotentialGpus called")

	available, consumed := gas.AvailableAndConsumed()
	// actual allocation is here
	allocated := make(map[string][]string)
	for _, ca := range gpucas {
		claimUID := string(ca.Claim.UID)
		if _, exists := gas.Spec.ResourceClaimRequests[claimUID]; exists {
			klog.V(5).Infof("Found existing GAS ClaimRequest, reusing without recalculation")
			devices := gas.Spec.ResourceClaimRequests[claimUID].GPUs
			for _, device := range devices {
				klog.V(5).Infof("Assigning device %v for claim %v", device.UUID, claimUID)
				allocated[claimUID] = append(allocated[claimUID], device.UUID)
			}
			continue
		}

		claimParams := ca.ClaimParameters.(*intelcrd.GpuClaimParametersSpec)
		var devices []string
		for i := 0; i < claimParams.Count; i++ {
			for _, device := range available {
				if gpuFitsRequest(claimParams, device, consumed[device.UUID]) {
					devices = append(devices, device.UUID)
					// TODO: if not shareable or no millicores were requested - remove from available
					// delete(available, device.UUID)
					break
				}
			}
		}
		allocated[claimUID] = devices
	}

	return allocated
}

// ensure claim fits to selected previously GPUs' resources
func (d *driver) enoughResourcesForPendingClaim(
	gas *intelcrd.GpuAllocationState,
	pendingClaimUID string,
	selectedNode string) bool {
	klog.V(5).Infof("enoughResourcesForPendingClaim called for claim %v", pendingClaimUID)

	// has to be copy of allocatable because unavailable GPUs are removed from the available map
	available, consumed := gas.AvailableAndConsumed()

	pendingClaim := d.PendingClaimRequests.Get(pendingClaimUID, selectedNode)
	for _, device := range pendingClaim.GPUs {
		if _, exists := available[device.UUID]; !exists {
			klog.Errorf("Device %v from pending claim %v is not available", device.UUID, pendingClaimUID)
			return false
		}
		if !gpuFitsRequest(&pendingClaim.Spec, available[device.UUID], consumed[device.UUID]) {
			return false
		}
	}

	return true
}

func gpuFitsRequest(
	request *intelcrd.GpuClaimParametersSpec,
	deviceRefSpec *intelcrd.AllocatableGpu,
	consumed *intelcrd.AllocatedGpu) bool {
	if request.Memory > 0 {
		memoryleft := deviceRefSpec.Memory - consumed.Memory
		if memoryleft >= request.Memory {
			klog.V(3).Infof("Sufficient memory on device %v (%v left / %v requested)", deviceRefSpec.UUID, memoryleft, request.Memory)
			consumed.Memory += request.Memory
		} else {
			klog.V(3).Infof("Not enough memory on device %v (%v left / %v requested)", deviceRefSpec.UUID, memoryleft, request.Memory)
			return false
		}
	} else {
		klog.V(5).Info("Disregarding zero memory request value")
	}
	return true
}

func buildAllocationResult(selectedNode string, shared bool) *resourcev1alpha1.AllocationResult {
	nodeSelector := &corev1.NodeSelector{
		NodeSelectorTerms: []corev1.NodeSelectorTerm{
			{
				MatchFields: []corev1.NodeSelectorRequirement{
					{
						Key:      "metadata.name",
						Operator: "In",
						Values:   []string{selectedNode},
					},
				},
			},
		},
	}
	allocation := &resourcev1alpha1.AllocationResult{
		AvailableOnNodes: nodeSelector,
		// ResourceHandle:
	}
	return allocation
}

func getSelectedNode(claim *resourcev1alpha1.ResourceClaim) string {
	if claim.Status.Allocation == nil {
		return ""
	}
	if claim.Status.Allocation.AvailableOnNodes == nil {
		return ""
	}
	return claim.Status.Allocation.AvailableOnNodes.NodeSelectorTerms[0].MatchFields[0].Values[0]
}

func unique(s []string) []string {
	set := make(map[string]struct{})
	var news []string
	for _, str := range s {
		if _, exists := set[str]; !exists {
			set[str] = struct{}{}
			news = append(news, str)
		}
	}
	return news
}
