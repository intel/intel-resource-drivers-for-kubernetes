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
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	resourcev1 "k8s.io/api/resource/v1alpha1"
	"k8s.io/dynamic-resource-allocation/controller"
	"k8s.io/klog/v2"

	intelclientset "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/crd/intel/clientset/versioned"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/crd/intel/v1alpha"
	intelcrd "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/crd/intel/v1alpha/api"
	sriov "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/sriov"
	driverVersion "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/version"
)

const (
	apiGroupVersion = intelcrd.APIGroupName + "/" + intelcrd.APIVersion
	minMemory       = 8
	bytesInMB       = 1048576
)

type driver struct {
	lock                 *PerNodeMutex
	namespace            string
	clientset            intelclientset.Interface
	PendingClaimRequests *PerNodeClaimRequests
}

var _ controller.Driver = (*driver)(nil)

func newDriver(config *configType) *driver {
	klog.V(5).Infof("Creating new driver")
	driverVersion.PrintDriverVersion()

	return &driver{
		lock:                 NewPerNodeMutex(),
		namespace:            config.namespace,
		clientset:            config.clientset.intel,
		PendingClaimRequests: NewPerNodeClaimRequests(),
	}
}

func (d driver) GetClassParameters(ctx context.Context, class *resourcev1.ResourceClass) (interface{}, error) {
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

func (d driver) GetClaimParameters(
	ctx context.Context, claim *resourcev1.ResourceClaim,
	class *resourcev1.ResourceClass, classParameters interface{}) (interface{}, error) {
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

	if claim.Spec.ParametersRef.Kind != intelcrd.GpuClaimParametersKind {
		klog.Error("Unsupported ResourceClaimParametersRef Kind: %v", claim.Spec.ParametersRef.Kind)
		return nil, fmt.Errorf("Unsupported ResourceClaimParametersRef Kind: %v", claim.Spec.ParametersRef.Kind)
	}

	gcp, err := d.clientset.GpuV1alpha().
		GpuClaimParameters(claim.Namespace).
		Get(ctx, claim.Spec.ParametersRef.Name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("could not get GpuClaimParameters '%v' in namespace '%v': %v",
			claim.Spec.ParametersRef.Name, claim.Namespace, err)
	}

	err = validateGpuClaimParameters(&gcp.Spec)
	if err != nil {
		return nil, fmt.Errorf("could not validate GpuClaimParameters '%v' in namespace '%v': %v",
			claim.Spec.ParametersRef.Name, claim.Namespace, err)
	}

	return &gcp.Spec, nil
}

// Sanitize resource request.
func validateGpuClaimParameters(claimParams *intelcrd.GpuClaimParametersSpec) error {
	klog.V(5).Infof("validateGpuClaimParameters called")

	// Count is mandatory
	if claimParams.Count < 1 {
		return fmt.Errorf("invalid number of GPUs requested: %v", claimParams.Count)
	}

	// type is mandatory
	if claimParams.Type != intelcrd.GpuDeviceType && claimParams.Type != intelcrd.VfDeviceType {
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
	claim *resourcev1.ResourceClaim,
	claimParameters interface{},
	class *resourcev1.ResourceClass,
	classParameters interface{},
	selectedNode string) (*resourcev1.AllocationResult, error) {
	klog.V(5).InfoS("Allocate called", "resource claim", claim.Namespace+"/"+claim.Name, "selectedNode", selectedNode)

	if selectedNode == "" { // immediate allocation with no pendingResourceClaims
		return d.allocateImmediateClaim(claim, claimParameters, class, classParameters)
	}

	return d.allocatePendingClaim(claim, claimParameters, selectedNode)
}

func (d driver) allocateImmediateClaim(
	claim *resourcev1.ResourceClaim,
	claimParameters interface{},
	class *resourcev1.ResourceClass,
	classParameters interface{},
) (*resourcev1.AllocationResult, error) {
	klog.V(5).Infof("Allocating immediately")

	crdconfig := &intelcrd.GpuAllocationStateConfig{
		Namespace: d.namespace,
	}

	gas := intelcrd.NewGpuAllocationState(crdconfig, d.clientset)

	gasnames, err := gas.ListNames()
	if err != nil {
		return nil, fmt.Errorf("error retrieving list of GpuAllocationState objects: %v", err)
	}

	// create claimAllocation
	caTemp := controller.ClaimAllocation{
		Claim:           claim,
		ClaimParameters: claimParameters,
		Class:           class,
		ClassParameters: classParameters,
	}
	cas := []*controller.ClaimAllocation{&caTemp}

	for _, nodename := range gasnames {
		if allocationResult, err := d.allocateImmediateClaimOnNode(cas, nodename); err == nil {
			return allocationResult, nil
		}
	}

	klog.V(3).InfoS("Could not immediately allocate", "resource claim", claim.Namespace+"/"+claim.Name)
	return nil, fmt.Errorf("no suitable node found")
}

func (d driver) allocateImmediateClaimOnNode(
	cas []*controller.ClaimAllocation, nodename string) (*resourcev1.AllocationResult, error) {
	klog.V(5).Infof("Fetching gas item: %v", nodename)
	d.lock.Get(nodename).Lock()
	defer d.lock.Get(nodename).Unlock()

	crdconfig := &intelcrd.GpuAllocationStateConfig{
		Namespace: d.namespace,
		Name:      nodename,
	}

	gas := intelcrd.NewGpuAllocationState(crdconfig, d.clientset)

	err := gas.Get()
	if err != nil {
		klog.Errorf("error retrieving GAS CRD for node %v: %v", nodename, err)

		return nil, fmt.Errorf("error retrieving GAS CRD for node %v: %v", nodename, err)
	}

	if gas.Spec.ResourceClaimAllocations == nil {
		gas.Spec.ResourceClaimAllocations = intelcrd.ResourceClaimAllocations{}
	}

	err = d.selectPotentialDevices(gas, cas)
	if err != nil {
		klog.V(3).Infof("Could not find suitable devices, skipping node %v", nodename)

		return nil, fmt.Errorf("Could not find suitable devices: %v", err)
	}

	klog.V(5).Infof("Allocated: %v", cas[0].Claim.UID)

	// sync ResourceClaimAllocations from allocations
	err = gas.Update(&gas.Spec)
	if err != nil {
		klog.Error("Could not update GpuAllocationState %v. Error: %+v", gas.Name, err)
		return nil, fmt.Errorf("error updating GpuAllocationState CRD: %v", err)
	}

	d.PendingClaimRequests.Remove(string(cas[0].Claim.UID))

	claimParams, ok := cas[0].ClaimParameters.(*intelcrd.GpuClaimParametersSpec)
	if !ok {
		return nil, fmt.Errorf("unknown ResourceClaim.ParametersRef.Kind: %v", cas[0].Claim.Spec.ParametersRef.Kind)
	}

	var claimParamsSpecString string
	claimParamsSpecJSON, err := json.Marshal(claimParams)
	if err == nil {
		claimParamsSpecString = string(claimParamsSpecJSON)
	} else {
		klog.Errorf("Failed to marshall GpuClaimParametersSpec")
	}

	return buildAllocationResult(nodename, true, claimParamsSpecString), nil
}

func (d driver) allocatePendingClaim(
	claim *resourcev1.ResourceClaim,
	claimParameters interface{},
	nodename string) (*resourcev1.AllocationResult, error) {
	d.lock.Get(nodename).Lock()
	defer d.lock.Get(nodename).Unlock()

	claimUID := string(claim.UID)

	crdconfig := &intelcrd.GpuAllocationStateConfig{
		Name:      nodename,
		Namespace: d.namespace,
	}

	klog.V(5).Infof("fetching GAS %v", nodename)
	gas := intelcrd.NewGpuAllocationState(crdconfig, d.clientset)

	err := gas.Get()
	if err != nil {
		return nil, fmt.Errorf("error retrieving GAS CRD for node %v: %v", nodename, err)
	}
	klog.V(5).Infof("GAS %v RCA: %+v", nodename, gas.Spec.ResourceClaimAllocations)

	if gas.Status != intelcrd.GpuAllocationStateStatusReady {
		return nil, fmt.Errorf("GpuAllocationStateStatus: %v", gas.Status)
	}

	var claimParamsSpecString string
	claimParams, ok := claimParameters.(*intelcrd.GpuClaimParametersSpec)
	if !ok {
		klog.Warningf("unknown ResourceClaim.ParametersRef.Kind: %v", claim.Spec.ParametersRef.Kind)
	} else {
		claimParamsSpecJSON, err := json.Marshal(claimParams)
		if err == nil {
			claimParamsSpecString = string(claimParamsSpecJSON)
		} else {
			klog.Warningf("Failed to marshall GpuClaimParametersSpec")
		}
	}

	if gas.Spec.ResourceClaimAllocations == nil {
		gas.Spec.ResourceClaimAllocations = intelcrd.ResourceClaimAllocations{}
	} else if _, exists := gas.Spec.ResourceClaimAllocations[claimUID]; exists {
		klog.V(5).Infof("GAS already has ResourceClaimAllocation for claim %v, building allocation result", claimUID)
		return buildAllocationResult(nodename, true, claimParamsSpecString), nil
	}

	if !d.PendingClaimRequests.Exists(claimUID, nodename) {
		return nil, fmt.Errorf(
			"no allocation requests generated for claim '%v' on node '%v' yet",
			claimUID, nodename)
	}

	owner := ""
	if claim.OwnerReferences != nil && len(claim.OwnerReferences) > 0 {
		klog.V(5).Infof("Claim %v is owned at least by %+v", claimUID, claim.OwnerReferences[0])
		owner = string(claim.OwnerReferences[0].UID)
	}

	// validate that there is still resource for it
	if d.pendingClaimStillValid(gas, claimUID, nodename, owner) {
		gas.Spec.ResourceClaimAllocations[claimUID] = d.PendingClaimRequests.Get(claimUID, nodename)
		klog.V(5).Infof("enough resources. Setting GAS ResourceClaimAllocation %v: %+v",
			claimUID, gas.Spec.ResourceClaimAllocations[claimUID])
	} else {
		// TODO: attempt to recalculate instead of failing
		klog.V(5).Infof("Insufficient resource for claim %v on allocation", claimUID)
		err = fmt.Errorf("insufficient resources for claim %v on node %v", claimUID, nodename)
	}

	if err != nil {
		return nil, fmt.Errorf("unable to allocate devices on node '%v': %v", nodename, err)
	}

	klog.V(5).Infof("Updating GAS %v with RCA: %+v", gas.Name, gas.Spec.ResourceClaimAllocations)
	err = gas.Update(&gas.Spec)
	if err != nil {
		return nil, fmt.Errorf("error updating GpuAllocationState CRD: %v", err)
	}

	d.PendingClaimRequests.Remove(claimUID)

	return buildAllocationResult(nodename, true, claimParamsSpecString), nil
}

func (d driver) Deallocate(ctx context.Context, claim *resourcev1.ResourceClaim) error {
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
	claimAllocation, exists := gas.Spec.ResourceClaimAllocations[claimUID]
	if !exists {
		klog.Warning("Resource claim %v does not exist internally in resource driver")
		return nil
	}

	request := intelcrd.GpuClaimParametersSpec{}
	if err := json.Unmarshal([]byte(claim.Status.Allocation.ResourceHandle), &request); err != nil {
		klog.V(5).Infof("Experiment failed: %v", err)
	} else {
		klog.V(5).Infof("Experiment succes: %+v", request)
	}

	delete(gas.Spec.ResourceClaimAllocations, claimUID)
	err = gas.Update(&gas.Spec)
	if err != nil {
		return fmt.Errorf("error updating GpuAllocationState CRD: %v", err)
	}

	switch claimAllocation.Request.Type {
	case intelcrd.GpuDeviceType, intelcrd.VfDeviceType, intelcrd.AnyDeviceType:
		d.PendingClaimRequests.Remove(claimUID)
	default:
		klog.Errorf("Unknown requested devices type: %v", claimAllocation.Request.Type)
		return fmt.Errorf("unable to deallocate devices '%v': unknown requested device type %v",
			claimAllocation, claimAllocation.Request.Type)
	}

	return nil
}

// Mark nodes that do not suit request into .UnsuitableNodes and populate d.PendingClaimAllocations.
func (d driver) UnsuitableNodes(
	ctx context.Context, pod *corev1.Pod,
	cas []*controller.ClaimAllocation, potentialNodes []string) error {
	klog.V(5).Infof("UnsuitableNodes called for %d claim allocations", len(cas))

	for _, node := range potentialNodes {
		klog.V(5).InfoS("UnsuitableNodes processing", "node", node)
		d.unsuitableNode(cas, node)
	}

	for _, claimallocation := range cas {
		claimallocation.UnsuitableNodes = unique(claimallocation.UnsuitableNodes)
	}

	return nil
}

func (d driver) unsuitableNode(allcas []*controller.ClaimAllocation, potentialNode string) {
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

		return
	}

	klog.V(5).InfoS("Success getting GPU allocation state", "node", potentialNode, "GAS", gas)

	if gas.Status != intelcrd.GpuAllocationStateStatusReady {

		klog.V(3).Infof("GAS status not ready, adding node %v to unsuitable nodes", potentialNode)

		for _, ca := range allcas {
			ca.UnsuitableNodes = append(ca.UnsuitableNodes, potentialNode)
		}

		return
	}

	klog.V(5).Infof("GAS status OK")

	if gas.Spec.ResourceClaimAllocations == nil {
		klog.V(5).Infof("Creating blank map for claim requests")
		gas.Spec.ResourceClaimAllocations = make(map[string]intelcrd.ResourceClaimAllocation)
	}

	d.unsuitableGpuNode(gas, allcas)
}

func (d *driver) unsuitableGpuNode(
	gas *intelcrd.GpuAllocationState,
	allcas []*controller.ClaimAllocation) {
	klog.V(5).Infof("unsuitableGpuNode called")

	// remove pending claim requests that are in CRD already
	d.PendingClaimRequests.CleanupNode(gas)

	filteredCAs := []*controller.ClaimAllocation{} // TODO: might not be needed if get claim parameters called for each

	klog.V(5).Infof("Filtering ResourceClaimParameters")

	for _, ca := range allcas {
		if _, ok := ca.ClaimParameters.(*intelcrd.GpuClaimParametersSpec); !ok {
			klog.V(3).Infof("Unsupported claim parameters type: %T", ca.ClaimParameters)

			continue
		}
		filteredCAs = append(filteredCAs, ca)
	}

	err := d.selectPotentialDevices(gas, filteredCAs)
	if err != nil {
		klog.V(5).Infof("Could not allocate request: %v", err)
		for _, ca := range allcas {
			ca.UnsuitableNodes = append(ca.UnsuitableNodes, gas.Name)
		}
		return
	}

	klog.V(5).Info("Leaving unsuitableGpuNode")
}

func sortClaimAllocations(cas []*controller.ClaimAllocation) map[string][]*controller.ClaimAllocation {
	sortedCAs := make(map[string][]*controller.ClaimAllocation)

	for _, ca := range cas {
		claimParams := ca.ClaimParameters.(*intelcrd.GpuClaimParametersSpec)
		sortedCAs[string(claimParams.Type)] = append(sortedCAs[string(claimParams.Type)], ca)
	}

	return sortedCAs
}

// Allocate all claim allocations. This is called from allocateImmedateClaim and unsuitableGpuNode, so
// need to copy allocations to both GAS (will be discarded by unsuitableGpuNode) and
// PendingClaimAllocations (will be cleanued up in allocateImmediateCleaim).
func (d *driver) selectPotentialDevices(
	gas *intelcrd.GpuAllocationState,
	gpuCAs []*controller.ClaimAllocation) error {
	klog.V(5).Infof("selectPotentialDevices called")

	perKindCAs := sortClaimAllocations(gpuCAs)
	available, consumed := gas.AvailableAndConsumed()
	allocations := intelcrd.ResourceClaimAllocations{}

	// ensure VFs are handled in first place - they reduce available GPUs: gpu with VFs should not be directly used
	for _, ca := range perKindCAs[v1alpha.VfDeviceType] {
		claimUID := string(ca.Claim.UID)
		klog.V(5).Infof("Selecting VF devices for claim %v", claimUID)

		// Rescheduling is more expensive than recalculating.
		if _, exists := gas.Spec.ResourceClaimAllocations[claimUID]; exists {
			klog.V(5).Infof("Found existing GAS ClaimRequest, discarding and recalculating")
			delete(gas.Spec.ResourceClaimAllocations, claimUID)
		}

		claimParams := ca.ClaimParameters.(*intelcrd.GpuClaimParametersSpec)
		resourceClaimAllocation := d.selectPotentialVFDevices(available, consumed, ca, gas, allocations)

		if len(resourceClaimAllocation.Gpus) != claimParams.Count {
			klog.V(5).Infof("Not enough resources to serve VF request %v", claimUID)
			klog.V(5).Infof("Requested %v, allocated %v for claim %v",
				claimParams.Count, len(resourceClaimAllocation.Gpus), claimUID)
			return fmt.Errorf("Not enough resources")
		}
		allocations[claimUID] = resourceClaimAllocation
	}

	for _, ca := range perKindCAs[v1alpha.GpuDeviceType] {
		claimUID := string(ca.Claim.UID)
		klog.V(5).Infof("Selecting GPU devices for claim %v", claimUID)

		// Rescheduling is more expensive than recalculating.
		if _, exists := gas.Spec.ResourceClaimAllocations[claimUID]; exists {
			klog.V(5).Infof("Found existing GAS ClaimRequest, discarding and recalculating")
			delete(gas.Spec.ResourceClaimAllocations, claimUID)
		}

		claimParams := ca.ClaimParameters.(*intelcrd.GpuClaimParametersSpec)
		resourceClaimAllocation := selectPotentialGpuDevices(available, consumed, ca)

		if len(resourceClaimAllocation.Gpus) != claimParams.Count {
			klog.V(5).Infof("Not enough resources to serve GPU request %v", claimUID)
			klog.V(5).Infof("Requested %v, allocated %v for claim %v: %+v",
				claimParams.Count, len(resourceClaimAllocation.Gpus), claimUID, resourceClaimAllocation.Gpus)
			return fmt.Errorf("Not enough resources")
		}

		klog.V(5).Infof("Claim %v can be allocated", claimUID)
		allocations[claimUID] = resourceClaimAllocation
	}

	if _, exists := perKindCAs[v1alpha.AnyDeviceType]; exists && len(perKindCAs[v1alpha.AnyDeviceType]) != 0 {
		klog.V(5).Infof("'Any' device type is not yet supported")
		return fmt.Errorf("'Any' device type is not yet supported")
	}

	klog.V(5).Infof("Saving pending allocations")
	for caUID, ca := range allocations {
		claimUID := string(caUID)

		d.PendingClaimRequests.Set(claimUID, gas.Name, ca)
		gas.Spec.ResourceClaimAllocations[claimUID] = ca
	}

	klog.V(5).Infof("Leaving selectPotentialDevices")
	return nil
}

func (d *driver) selectPotentialVFDevices(
	available map[string]*v1alpha.AllocatableGpu,
	consumed map[string]*intelcrd.AllocatableGpu,
	claimAllocation *controller.ClaimAllocation,
	gas *intelcrd.GpuAllocationState,
	rcas intelcrd.ResourceClaimAllocations) intelcrd.ResourceClaimAllocation {
	devices := intelcrd.AllocatedGpus{}
	claimParams := claimAllocation.ClaimParameters.(*intelcrd.GpuClaimParametersSpec)
	var owner string

	if claimAllocation.Claim.OwnerReferences != nil && len(claimAllocation.Claim.OwnerReferences) > 0 {
		owner = string(claimAllocation.Claim.OwnerReferences[0].UID)
	}

	klog.V(5).Infof("Looking for %d VFs. Current consumption: %+v", claimParams.Count, consumed)

	for i := 1; i <= claimParams.Count; i++ {
		klog.V(5).Infof("Picking %d devices for claim", i)

		minCandidateUID := ""
		for deviceUID, device := range available {
			klog.V(5).Infof("  Checking device %v", deviceUID)

			// pick the smallest suitable available VF
			if device.Type == intelcrd.VfDeviceType &&
				gpuFitsRequest(claimParams, device, consumed[deviceUID]) {
				if minCandidateUID == "" || available[minCandidateUID].Memory > available[deviceUID].Memory {
					minCandidateUID = deviceUID
					klog.V(5).Infof("  Found candidate VF %v", deviceUID)

					continue
				}
				klog.V(5).Infof("Ignoring same size or bigger VF candidate %v", deviceUID)
			} else {
				klog.V(5).Infof("  Device %v did not pass criteria", deviceUID)
			}
		}

		if minCandidateUID != "" {
			devices = append(
				devices,
				intelcrd.AllocatedFromAllocatable(available[minCandidateUID], claimParams.Memory))
			// do not use same VF several times, and don't let it be used in other claims
			delete(available, minCandidateUID)
			delete(consumed, minCandidateUID)
		}

		// no point searching for rest if we already ran out and did not find available
		if len(devices) != i {
			klog.V(5).Info("Could not find enough available VFs")
			break
		}
	}

	if len(devices) != claimParams.Count { // add new VFs
		klog.V(5).Info("Looking up available GPUs to create VFs")
		for parentUID, parent := range available {
			klog.V(5).Infof("Checking device %v (%+v) (consumed %+v) of %+v",
				parentUID, parent, consumed[parentUID], available)
			if parent.Maxvfs == 0 {

				continue
			}
			if consumed[parentUID].Maxvfs != 0 &&
				(!gas.SameOwnerVFAllocations(parentUID, owner) ||
					!SameOwnerUnsubmittedVFAllocations(rcas, parentUID, owner)) {
				klog.V(5).Infof("Device %v cannot have or already has VFs, skipping", parentUID)

				continue
			}

			// get profile for new VF on this device, act based on profile, not request - profile
			// might have more memory than requested
			profileName, err := sriov.PickVFProfile(available[parentUID].Model, claimParams.Memory)
			if err != nil {
				klog.Errorf("Could not determine VF profile for device %v, skipping", parentUID)

				continue
			}

			newVFMemMiB := int(sriov.Profiles[profileName]["lmem_quota"]) / bytesInMB

			// in case this function is called from iterating over list of claimAllocations -
			// there will be available GPUs with VFs sketched for provisioning, continue with the last index
			for vfIdx := consumed[parentUID].Maxvfs; vfIdx < parent.Maxvfs && len(devices) < claimParams.Count; vfIdx++ {
				klog.V(5).Infof("vf %d, consumed: %+v", vfIdx, consumed[parentUID])
				if (parent.Memory - consumed[parentUID].Memory) >= newVFMemMiB {
					newVFUID := fmt.Sprintf("%s-vf%d", parentUID, vfIdx)
					klog.V(5).Infof("Allocating new VF %v on device %v", newVFUID, parentUID)
					consumed[parentUID].Maxvfs++
					consumed[parentUID].Memory += newVFMemMiB
					newVF := intelcrd.AllocatedFromAllocatable(available[parentUID], newVFMemMiB)
					newVF.Type = intelcrd.VfDeviceType
					newVF.UID = newVFUID
					newVF.Profile = profileName
					newVF.ParentUID = parentUID
					devices = append(devices, newVF)
				} else {
					klog.V(5).Infof("Device %v has not enough memory left for requested VF, continuing search",
						parentUID)
					break
				}
			}

			if len(devices) == claimParams.Count {
				klog.V(5).Info("Enough VFs were planned")
				break
			}
		}
	}

	allocation := intelcrd.ResourceClaimAllocation{
		Request: *claimParams,
		Gpus:    devices,
		Owner:   owner,
	}

	return allocation
}

// After VFs have been handled, available devices should only contain GPUs with no VFs expected to be provisioned.
func selectPotentialGpuDevices(
	available map[string]*v1alpha.AllocatableGpu,
	consumed map[string]*intelcrd.AllocatableGpu,
	claimAllocation *controller.ClaimAllocation) intelcrd.ResourceClaimAllocation {
	claimParams := claimAllocation.ClaimParameters.(*intelcrd.GpuClaimParametersSpec)

	devices := intelcrd.AllocatedGpus{}

	for deviceUID, device := range available {
		if device.Type == intelcrd.GpuDeviceType && consumed[deviceUID].Maxvfs == 0 &&
			gpuFitsRequest(claimParams, device, consumed[deviceUID]) {
			devices = append(devices, intelcrd.AllocatedFromAllocatable(device, claimParams.Memory))
		}
		if len(devices) == claimParams.Count {
			klog.V(5).Infof("Found enough suitable devices for claim %v (%v): %+v",
				claimAllocation.Claim.UID, len(devices), devices)
			break
		}
	}

	allocation := intelcrd.ResourceClaimAllocation{
		Request: *claimParams,
		Gpus:    devices,
	}

	klog.V(5).Infof("Checking if claim %v has an owner", claimAllocation.Claim.UID)
	if claimAllocation.Claim.OwnerReferences != nil && len(claimAllocation.Claim.OwnerReferences) > 0 {
		allocation.Owner = string(claimAllocation.Claim.OwnerReferences[0].UID)
	}

	klog.V(5).Info("Leaving selectPotentialGpuDevices")
	return allocation
}

// Ensure claim fits to previously selected GPU resources.
func (d *driver) pendingClaimStillValid(
	gas *intelcrd.GpuAllocationState,
	pendingClaimUID string,
	selectedNode string,
	owner string) bool {
	klog.V(5).Infof("enoughResourcesForPendingClaim called for claim %v", pendingClaimUID)

	// has to be copy of allocatable because unavailable GPUs are removed from the available map
	available, consumed := gas.AvailableAndConsumed()

	pendingClaim := d.PendingClaimRequests.Get(pendingClaimUID, selectedNode)
	for _, device := range pendingClaim.Gpus {
		if _, exists := available[device.UID]; !exists { // check if it is a VF and can be provisioned
			if len(device.UID) == sriov.PFUIDLength {
				klog.Errorf("Device %v from pending claim %v is not VF and is not available",
					device.UID, pendingClaimUID)
				return false
			}

			// it is a VF
			parentUID, err2 := sriov.PfUIDFromVfUID(device.UID)
			if err2 != nil {
				klog.Errorf("Could not get PF from VF UID '%v': %v", device.UID, err2)
				return false
			}

			if gas.DeviceIsAllocated(device.UID) {
				klog.Errorf("Device %v is already allocated, need to recalculate allocation %v",
					device.UID, pendingClaimUID)
				return false
			}

			// in case Pod has multiple CAs with VFs, first CA will declare VFs on empty GPU,
			// wiping it from list of available GPUs, subsequent CAs will need to ensure they are
			// declared on GPU with VFs of the same owner, only then all these CAs will be sent for
			// NodeResourcePrepare as a batch and handled together.
			klog.V(5).Infof("Pending device %v is a VF, checking parent %v", device.UID, parentUID)
			if available[parentUID].Maxvfs == 0 || consumed[parentUID].Maxvfs >= available[parentUID].Maxvfs {
				return false
			}

			if consumed[parentUID].Maxvfs > 0 && !gas.SameOwnerVFAllocations(parentUID, owner) {
				klog.V(5).Infof("Cannot allocate unprovisioned VF claim requests for different Pods on same GPU")
				return false
			}

			klog.V(5).Infof("Checking if gpu %v fits request", parentUID)
			// check if GPU has enough resources left for current loop iteration's VF
			if !gpuFitsRequest(&pendingClaim.Request, available[parentUID], consumed[parentUID]) {
				return false
			}

			klog.V(5).Infof("VF %v is OK to be provisioned", device.UID)
			consumed[parentUID].Maxvfs++
			consumed[parentUID].Memory += pendingClaim.Request.Memory
		} else { // if _, exists := available[device.UID]; exists
			if !gpuFitsRequest(&pendingClaim.Request, available[device.UID], consumed[device.UID]) {
				return false
			}
			if available[device.UID].Type == intelcrd.VfDeviceType {
				delete(available, device.UID)
				delete(consumed, device.UID)
			} else {
				consumed[device.UID].Memory += pendingClaim.Request.Memory
			}
		}
	}

	klog.V(5).Infof("Pending claim allocation %v is still valid", pendingClaimUID)
	return true
}

func SameOwnerUnsubmittedVFAllocations(rcas intelcrd.ResourceClaimAllocations, parentUID string, owner string) bool {
	klog.V(5).Infof("Checking if all unsubmitted VFs on device %v owned by %v", parentUID, owner)
	if len(rcas) == 0 {
		klog.V(5).Infof("No VF allocations yet, nothing to check")
		return true
	}

	for _, claimAllocation := range rcas {
		for _, device := range claimAllocation.Gpus {
			if device.Type == intelcrd.VfDeviceType && device.ParentUID == parentUID && claimAllocation.Owner != owner {
				return false
			}
		}
	}

	return true
}

func gpuFitsRequest(
	request *intelcrd.GpuClaimParametersSpec,
	deviceRefSpec *intelcrd.AllocatableGpu,
	consumed *intelcrd.AllocatableGpu) bool {
	if request.Type == intelcrd.VfDeviceType && deviceRefSpec.Type != intelcrd.VfDeviceType &&
		(consumed.Maxvfs == deviceRefSpec.Maxvfs || deviceRefSpec.Maxvfs == 0) {
		return false
	}
	if request.Memory > 0 {
		memoryleft := deviceRefSpec.Memory - consumed.Memory
		if memoryleft >= request.Memory {
			klog.V(3).Infof("Sufficient memory on device %v (%v left / %v requested",
				deviceRefSpec.UID, memoryleft, request.Memory)
		} else {
			klog.V(3).Infof("Not enough memory on device %v (%v left / %v requested)",
				deviceRefSpec.UID, memoryleft, request.Memory)
			return false
		}
	} else {
		klog.V(5).Info("Disregarding zero memory request value")
	}
	return true
}

func buildAllocationResult(selectedNode string, shared bool, requestString string) *resourcev1.AllocationResult {
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
	allocation := &resourcev1.AllocationResult{
		AvailableOnNodes: nodeSelector,
		Shareable:        shared,
		ResourceHandle:   requestString,
	}
	return allocation
}

func getSelectedNode(claim *resourcev1.ResourceClaim) string {
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
