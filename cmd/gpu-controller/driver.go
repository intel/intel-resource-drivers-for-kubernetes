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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"

	resourcev1 "k8s.io/api/resource/v1alpha2"
	"k8s.io/dynamic-resource-allocation/controller"
	"k8s.io/klog/v2"

	intelclientset "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gpu/clientset/versioned"
	intelcrd "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gpu/v1alpha2/api"
	sriov "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/sriov"
	driverVersion "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/version"
)

const (
	apiGroupVersion = intelcrd.APIGroupName + "/" + intelcrd.APIVersion
)

type driver struct {
	lock                 *perNodeMutex
	namespace            string
	clientset            intelclientset.Interface
	PendingClaimRequests *perNodeClaimRequests
	policyResourceValue
	preferredOrder
}

// compile-time test if implementation is conformant with the interface.
var _ controller.Driver = (*driver)(nil)

func newDriver(config *configType) *driver {
	klog.V(5).Infof("Creating new driver")
	driverVersion.PrintDriverVersion()

	driver := driver{
		lock:                 newPerNodeMutex(),
		namespace:            config.namespace,
		clientset:            config.clientset.intel,
		PendingClaimRequests: newPerNodeClaimRequests(),
	}

	driver.initPolicy(config)

	return &driver
}

// GetClassParameters returns GpuClassParameters after sanitization or error.
func (d driver) GetClassParameters(ctx context.Context, class *resourcev1.ResourceClass) (interface{}, error) {
	klog.V(5).InfoS("GetClassParameters called", "resource class", class.Name)

	if class.ParametersRef == nil {
		return intelcrd.DefaultGpuClassParametersSpec(), nil
	}

	if class.ParametersRef.APIGroup != apiGroupVersion {
		return nil, fmt.Errorf(
			"incorrect resource-class API group and version: %v, expected: %v",
			class.ParametersRef.APIGroup,
			apiGroupVersion)
	}

	if class.ParametersRef.Kind != intelcrd.GpuClassParametersKind {
		klog.Error("unsupported ResourceClass.ParametersRef.Kind: %v", class.ParametersRef.Kind)
		return nil, fmt.Errorf("unsupported ResourceClass.ParametersRef.Kind: %v", class.ParametersRef.Kind)
	}

	dc, err := d.clientset.GpuV1alpha2().GpuClassParameters().Get(ctx, class.ParametersRef.Name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("could not get GpuClassParameters '%v': %v", class.ParametersRef.Name, err)
	}

	klog.V(5).Infof("GpuClassParameters: %+v", dc.Spec)
	return &dc.Spec, nil
}

// GetClaimParameters returns GpuClaimParameters after sanitization or error.
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
		klog.Error("unsupported ResourceClaimParametersRef Kind: %v", claim.Spec.ParametersRef.Kind)
		return nil, fmt.Errorf("unsupported ResourceClaimParametersRef Kind: %v", claim.Spec.ParametersRef.Kind)
	}

	gcp, err := d.clientset.GpuV1alpha2().
		GpuClaimParameters(claim.Namespace).
		Get(ctx, claim.Spec.ParametersRef.Name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("could not get GpuClaimParameters '%v' in namespace '%v': %v",
			claim.Spec.ParametersRef.Name, claim.Namespace, err)
	}

	err = validateGpuClaimParameters(&gcp.Spec, classParameters)
	if err != nil {
		return nil, fmt.Errorf("could not validate GpuClaimParameters '%v' in namespace '%v': %v",
			claim.Spec.ParametersRef.Name, claim.Namespace, err)
	}

	return &gcp.Spec, nil
}

// Sanitize resource claim request.
func validateGpuClaimParameters(claimParams *intelcrd.GpuClaimParametersSpec, classParameters interface{}) error {
	klog.V(5).Infof("validateGpuClaimParameters called")

	classParams, ok := classParameters.(*intelcrd.GpuClassParametersSpec)
	if ok && classParams.Monitor {
		// allow only default
		if claimParams.Type != intelcrd.GpuDeviceType {
			return fmt.Errorf("unsupported monitor type requested: %v", claimParams.Type)
		}
		return nil
	}

	// type is mandatory
	if claimParams.Type != intelcrd.GpuDeviceType && claimParams.Type != intelcrd.VfDeviceType {
		return fmt.Errorf("unsupported device type requested: %v", claimParams.Type)
	}

	// No millicores when exclusive GPU requested
	if ok && !classParams.Shared && claimParams.Millicores > 0 {
		return fmt.Errorf("millicores are not supported for non-shared Resource Class requests")
	}

	// TODO: Sharing SR-IOV VF is not implemented.
	// The reason is that millicores support for SR-IOV is a prerequisite.
	if ok && classParams.Shared && claimParams.Type == intelcrd.VfDeviceType {
		return fmt.Errorf("sharing SR-IOV VF is not implemented")
	}

	// Millicores are not for vf type
	if claimParams.Type == intelcrd.VfDeviceType && claimParams.Millicores > 0 {
		return fmt.Errorf("device type is vf, Millicores are not supported (params have %v Millicores)", claimParams.Millicores)
	}

	return nil
}

// Allocate is called by scheduler when ResourceClaims need allocation.
func (d driver) Allocate(ctx context.Context, claims []*controller.ClaimAllocation, selectedNode string) {
	klog.V(5).InfoS("Allocate called", "number of resource claims", len(claims), "selectedNode", selectedNode)

	if selectedNode == "" { // immediate allocation with no pendingResourceClaims
		d.allocateImmediateClaims(ctx, claims)
		return
	}

	d.allocateMultiplePendingClaims(ctx, claims, selectedNode)
}

func (d driver) allocateImmediateClaims(ctx context.Context, claims []*controller.ClaimAllocation) {
	for _, ca := range claims {
		allocation, error := d.allocateImmediateClaim(ctx, ca.Claim, ca.ClaimParameters, ca.Class, ca.ClassParameters)
		if error != nil {
			ca.Error = error
			continue
		}
		ca.Allocation = allocation
	}
}

func (d driver) allocateMultiplePendingClaims(ctx context.Context, claims []*controller.ClaimAllocation, selectedNode string) {
	for _, ca := range claims {
		allocation, error := d.allocateSinglePendingClaim(ctx, ca.Claim, ca.ClaimParameters, ca.ClassParameters, selectedNode)
		if error != nil {
			ca.Error = error
			continue
		}
		ca.Allocation = allocation
	}
}

func (d driver) allocateImmediateClaim(
	ctx context.Context,
	claim *resourcev1.ResourceClaim,
	claimParameters interface{},
	class *resourcev1.ResourceClass,
	classParameters interface{},
) (*resourcev1.AllocationResult, error) {
	klog.V(5).Infof("Allocating immediately")

	classParams, ok := classParameters.(*intelcrd.GpuClassParametersSpec)
	if ok && classParams.Monitor {
		return nil, fmt.Errorf("immediate GPU *monitor* class claims are unsupported")
	}

	crdconfig := &intelcrd.GpuAllocationStateConfig{
		Namespace: d.namespace,
	}

	gas := intelcrd.NewGpuAllocationState(crdconfig, d.clientset)

	gasnames, err := gas.ListNames(ctx)
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
		if allocationResult, err := d.allocateImmediateClaimOnNode(ctx, cas, nodename); err == nil {
			return allocationResult, nil
		}
	}

	klog.V(3).InfoS("Could not immediately allocate", "resource claim", claim.Namespace+"/"+claim.Name)
	return nil, fmt.Errorf("no suitable node found")
}

func (d driver) allocateImmediateClaimOnNode(
	ctx context.Context,
	cas []*controller.ClaimAllocation, nodename string) (*resourcev1.AllocationResult, error) {
	klog.V(5).Infof("Fetching gas item: %v", nodename)
	d.lock.Get(nodename).Lock()
	defer d.lock.Get(nodename).Unlock()

	crdconfig := &intelcrd.GpuAllocationStateConfig{
		Namespace: d.namespace,
		Name:      nodename,
	}

	claimParams, ok := cas[0].ClaimParameters.(*intelcrd.GpuClaimParametersSpec)
	if !ok {
		return nil, fmt.Errorf("error parsing claim parameters")
	}

	allocateErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		gas := intelcrd.NewGpuAllocationState(crdconfig, d.clientset)

		err := gas.Get(ctx)
		if err != nil {
			return fmt.Errorf("error retrieving GpuAllocationState %v: %v", nodename, err)
		}

		if gas.Spec.AllocatedClaims == nil {
			gas.Spec.AllocatedClaims = intelcrd.AllocatedClaims{}
		}

		err = d.selectPotentialDevices(gas, cas)
		if err != nil {
			klog.V(3).Infof("no suitable devices, skipping node %v", nodename)

			return fmt.Errorf("no suitable devices: %v", err)
		}

		klog.V(5).Infof("Successfully allocated claim %v", cas[0].Claim.UID)

		// sync AllocatedClaims from allocations
		err = gas.Update(ctx, &gas.Spec)
		if err != nil {
			klog.V(5).Infof("error updating GpuAllocationState: %v", err)
		}
		return err
	})
	if allocateErr != nil {
		return nil, fmt.Errorf("allocating devices on node %v: %v", nodename, allocateErr)
	}

	d.PendingClaimRequests.remove(string(cas[0].Claim.UID))

	return buildAllocationResult(nodename, claimParams.Shareable), nil
}

func (d driver) allocateSinglePendingClaim(
	ctx context.Context,
	claim *resourcev1.ResourceClaim,
	claimParameters interface{},
	classParameters interface{},
	nodename string) (*resourcev1.AllocationResult, error) {
	d.lock.Get(nodename).Lock()
	defer d.lock.Get(nodename).Unlock()

	classParams, ok := classParameters.(*intelcrd.GpuClassParametersSpec)
	if !ok {
		return nil, fmt.Errorf("error parsing Resource Class Parameters")
	}

	if classParams.Monitor {
		// all allocation claims are accepted for monitoring (true=shared)
		return buildMonitorAllocationResult(nodename, true), nil
	}

	claimParams, ok := claimParameters.(*intelcrd.GpuClaimParametersSpec)
	if !ok {
		return nil, fmt.Errorf("error parsing Resource Claim Parameters")
	}

	claimUID := string(claim.UID)

	crdconfig := &intelcrd.GpuAllocationStateConfig{
		Name:      nodename,
		Namespace: d.namespace,
	}

	gas := intelcrd.NewGpuAllocationState(crdconfig, d.clientset)

	allocateErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		err := gas.Get(ctx)
		if err != nil {
			return fmt.Errorf("error retrieving GpuAllocationState for node %v: %v", nodename, err)
		}

		if gas.Status != intelcrd.GpuAllocationStateStatusReady {
			return fmt.Errorf("GpuAllocationStateStatus: %v", gas.Status)
		}

		if gas.Spec.AllocatedClaims == nil {
			gas.Spec.AllocatedClaims = intelcrd.AllocatedClaims{}
		} else if _, exists := gas.Spec.AllocatedClaims[claimUID]; exists {
			klog.V(5).Infof("GpuAllocationState already has AllocatedClaim %v, nothing to do", claimUID)
			return nil
		}

		if !d.PendingClaimRequests.exists(claimUID, nodename) {
			return fmt.Errorf("no allocation requests generated for claim '%v' on node '%v' yet", claimUID, nodename)
		}

		owner := ""
		if claim.OwnerReferences != nil && len(claim.OwnerReferences) > 0 {
			klog.V(5).Infof("Claim %v is owned at least by %+v", claimUID, claim.OwnerReferences[0])
			owner = string(claim.OwnerReferences[0].UID)
		}

		// validate that there is still resource for it
		if !d.pendingClaimStillValid(gas, claimParams, classParams, claimUID, nodename, owner) {
			// TODO: attempt to recalculate instead of failing?
			klog.V(5).Infof("Insufficient resource for claim %v on node %v", claimUID, nodename)
			return fmt.Errorf("insufficient resources for claim %v on node %v", claimUID, nodename)
		}

		gas.Spec.AllocatedClaims[claimUID] = d.PendingClaimRequests.get(claimUID, nodename)
		klog.V(5).Infof("enough resources for claim %v: %+v", claimUID, gas.Spec.AllocatedClaims[claimUID])

		err = gas.Update(ctx, &gas.Spec)
		if err != nil {
			klog.V(5).Infof("error updating GpuAllocationState: %v", err)
		}
		return err
	})
	if allocateErr != nil {
		return nil, fmt.Errorf("allocating devices on node %v: %v", nodename, allocateErr)
	}

	d.PendingClaimRequests.remove(claimUID)

	return buildAllocationResult(nodename, claimParams.Shareable), nil
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

	deallocateErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		err := gas.Get(ctx)
		if err != nil {
			return fmt.Errorf("error retrieving GpuAllocationState %v: %v", selectedNode, err)
		}

		claimUID := string(claim.UID)
		_, exists := gas.Spec.AllocatedClaims[claimUID]
		if !exists {
			klog.Warning("Resource claim %v does not exist internally in resource driver", claimUID)
			return nil
		}

		delete(gas.Spec.AllocatedClaims, claimUID)
		err = gas.Update(ctx, &gas.Spec)
		if err != nil {
			klog.V(5).Infof("error updating GpuAllocationState: %v", err)
			return err
		}

		d.PendingClaimRequests.remove(claimUID)
		return nil
	})

	if deallocateErr != nil {
		klog.Errorf("failed to deallocate claim: %v", deallocateErr)
		return fmt.Errorf("deallocating claim: %v", deallocateErr)
	}

	return nil
}

// UnsuitableNodes responds to the scheduler by writing the list of node names
// that do not suit request into ClaimAllocation.UnsuitableNodes and populates
// d.PendingClaimAllocations.
func (d driver) UnsuitableNodes(
	ctx context.Context, pod *corev1.Pod,
	allcas []*controller.ClaimAllocation, potentialNodes []string) error {
	klog.V(5).Infof("UnsuitableNodes called for %d claim allocations", len(allcas))

	i := 0
	// filter out monitoring CAs (typically none) from further checks
	nodecas := make([]*controller.ClaimAllocation, len(allcas))
	for _, ca := range allcas {
		classParams, ok := ca.ClassParameters.(*intelcrd.GpuClassParametersSpec)
		if !(ok && classParams.Monitor) {
			nodecas[i] = ca
			i++
		}
	}

	nodecas = nodecas[:i]
	for _, node := range potentialNodes {
		klog.V(5).InfoS("UnsuitableNodes processing", "node", node)
		d.unsuitableNode(ctx, nodecas, node)
	}

	for _, ca := range nodecas {
		ca.UnsuitableNodes = unique(ca.UnsuitableNodes)
	}

	return nil
}

func (d driver) unsuitableNode(ctx context.Context, allcas []*controller.ClaimAllocation, potentialNode string) {
	d.lock.Get(potentialNode).Lock()
	defer d.lock.Get(potentialNode).Unlock()

	crdconfig := &intelcrd.GpuAllocationStateConfig{
		Name:      potentialNode,
		Namespace: d.namespace,
	}
	gas := intelcrd.NewGpuAllocationState(crdconfig, d.clientset)

	klog.V(5).InfoS("Getting GPU allocation state", "node", potentialNode, "namespace", d.namespace)
	err := gas.Get(ctx)
	if err != nil {
		klog.V(3).Infof("Could not get node %v allocation state: %v", potentialNode, err)
		for _, ca := range allcas {
			klog.V(5).Infof("Adding node %v to unsuitable nodes for CA %v", potentialNode, ca)
			ca.UnsuitableNodes = append(ca.UnsuitableNodes, potentialNode)
		}

		return
	}

	if gas.Status != intelcrd.GpuAllocationStateStatusReady {
		klog.V(3).Infof("GpuAllocationState status is %v, adding node %v to unsuitable nodes", gas.Status, potentialNode)
		for _, ca := range allcas {
			ca.UnsuitableNodes = append(ca.UnsuitableNodes, potentialNode)
		}

		return
	}

	if gas.Spec.AllocatedClaims == nil {
		gas.Spec.AllocatedClaims = make(map[string]intelcrd.AllocatedClaim)
	}

	d.unsuitableGpuNode(gas, allcas)
}

func (d *driver) unsuitableGpuNode(
	gas *intelcrd.GpuAllocationState,
	allcas []*controller.ClaimAllocation) {
	// remove pending claim requests that are in GpuAllocationState already
	d.PendingClaimRequests.cleanupNode(gas)

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
}

func sortClaimAllocations(cas []*controller.ClaimAllocation) map[string][]*controller.ClaimAllocation {
	sortedCAs := make(map[string][]*controller.ClaimAllocation)

	for _, ca := range cas {
		claimParams, _ := ca.ClaimParameters.(*intelcrd.GpuClaimParametersSpec)
		sortedCAs[string(claimParams.Type)] = append(sortedCAs[string(claimParams.Type)], ca)
	}

	return sortedCAs
}

// selectPotentialDevices allocates all claim allocations. This is called from
// allocateImmedateClaim and unsuitableGpuNode, so need to copy allocations
// to both: GAS (will be discarded by unsuitableGpuNode) and PendingClaimAllocations
// (will be cleanued up in allocateImmediateCleaim).
func (d *driver) selectPotentialDevices(
	gas *intelcrd.GpuAllocationState,
	gpuCAs []*controller.ClaimAllocation) error {
	klog.V(5).Infof("selectPotentialDevices called")

	perKindCAs := sortClaimAllocations(gpuCAs)
	available, consumed := gas.AvailableAndConsumed()
	allocatedClaims := intelcrd.AllocatedClaims{}

	// Ensure VFs are handled in first place - they reduce available GPUs: GPU with VFs should not be directly used.
	for _, ca := range perKindCAs[intelcrd.VfDeviceType] {
		claimUID := string(ca.Claim.UID)
		klog.V(5).Infof("Selecting VF devices for claim %v", claimUID)

		// Rescheduling is more expensive than recalculating.
		if _, exists := gas.Spec.AllocatedClaims[claimUID]; exists {
			klog.V(5).Infof("Found existing AllocatedClaim in GpuAllocationState, discarding and recalculating")
			delete(gas.Spec.AllocatedClaims, claimUID)
		}

		claimParams, _ := ca.ClaimParameters.(*intelcrd.GpuClaimParametersSpec)
		resourceClaimAllocation := d.selectPotentialVFDevices(available, consumed, ca, gas, allocatedClaims)

		if uint64(len(resourceClaimAllocation.Gpus)) != claimParams.Count {
			klog.V(5).Infof("Not enough resources to serve VF request %v", claimUID)
			klog.V(5).Infof("Requested %v, allocated %v for claim %v",
				claimParams.Count, len(resourceClaimAllocation.Gpus), claimUID)
			return fmt.Errorf("not enough resources")
		}
		allocatedClaims[claimUID] = resourceClaimAllocation
	}

	for _, ca := range perKindCAs[intelcrd.GpuDeviceType] {
		claimUID := string(ca.Claim.UID)
		klog.V(5).Infof("Selecting GPU devices for claim %v", claimUID)

		// Rescheduling is more expensive than recalculating.
		if _, exists := gas.Spec.AllocatedClaims[claimUID]; exists {
			klog.V(5).Infof("Found existing AllocatedClaim, discarding and recalculating")
			delete(gas.Spec.AllocatedClaims, claimUID)
		}

		claimParams, _ := ca.ClaimParameters.(*intelcrd.GpuClaimParametersSpec)
		resourceClaimAllocation := d.selectPotentialGpuDevices(available, consumed, ca)

		if uint64(len(resourceClaimAllocation.Gpus)) != claimParams.Count {
			klog.V(5).Infof("Not enough resources to serve GPU request %v", claimUID)
			klog.V(5).Infof("Requested %v, allocated %v for claim %v: %+v",
				claimParams.Count, len(resourceClaimAllocation.Gpus), claimUID, resourceClaimAllocation.Gpus)
			return fmt.Errorf("not enough resources")
		}

		klog.V(5).Infof("Claim %v can be allocated", claimUID)
		allocatedClaims[claimUID] = resourceClaimAllocation
	}

	if _, exists := perKindCAs[intelcrd.AnyDeviceType]; exists && len(perKindCAs[intelcrd.AnyDeviceType]) != 0 {
		return fmt.Errorf("'any' device type is not yet supported")
	}

	klog.V(5).Infof("Saving pending allocations")
	for caUID, ca := range allocatedClaims {
		claimUID := string(caUID)

		d.PendingClaimRequests.set(claimUID, gas.Name, ca)
		gas.Spec.AllocatedClaims[claimUID] = ca
	}

	return nil
}

func (d *driver) selectPotentialVFDevices(
	available map[string]*intelcrd.AllocatableGpu,
	consumed map[string]*intelcrd.AllocatableGpu,
	claimAllocation *controller.ClaimAllocation,
	gas *intelcrd.GpuAllocationState,
	allocatedClaims intelcrd.AllocatedClaims) intelcrd.AllocatedClaim {
	devices := intelcrd.AllocatedGpus{}
	claimParams, _ := claimAllocation.ClaimParameters.(*intelcrd.GpuClaimParametersSpec)
	classParams, _ := claimAllocation.ClassParameters.(*intelcrd.GpuClassParametersSpec)
	var owner string

	if claimAllocation.Claim.OwnerReferences != nil && len(claimAllocation.Claim.OwnerReferences) > 0 {
		owner = string(claimAllocation.Claim.OwnerReferences[0].UID)
	}

	klog.V(5).Infof("Looking for %d VFs. Current consumption: %+v", claimParams.Count, consumed)

	for i := uint64(1); i <= claimParams.Count; i++ {
		klog.V(5).Infof("Picking %d devices for claim", i)

		minCandidateUID := ""
		for deviceUID, device := range available {
			klog.V(5).Infof("  Checking device %v", deviceUID)

			// pick the smallest suitable available VF
			if device.Type == intelcrd.VfDeviceType && gpuFitsRequest(claimParams, device, consumed[deviceUID], classParams.Shared) {
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
				intelcrd.AllocatedFromAllocatable(available[minCandidateUID], claimParams, classParams.Shared))
			// do not use same VF several times, and don't let it be used in other claims
			delete(available, minCandidateUID)
			delete(consumed, minCandidateUID)
		}

		// no point searching for rest if we already ran out and did not find available
		if uint64(len(devices)) != i {
			klog.V(5).Info("Could not find enough available VFs")
			break
		}
	}

	if uint64(len(devices)) != claimParams.Count {
		devicesNeeded := int(claimParams.Count) - len(devices)
		newDevices := d.addNewVFs(available, consumed, gas, allocatedClaims, devicesNeeded, claimParams, owner)
		devices = append(devices, newDevices...)
	}

	allocation := intelcrd.AllocatedClaim{
		Gpus:  devices,
		Owner: owner,
	}

	return allocation
}

func (d *driver) addNewVFs(
	available map[string]*intelcrd.AllocatableGpu,
	consumed map[string]*intelcrd.AllocatableGpu,
	gas *intelcrd.GpuAllocationState,
	allocatedClaims intelcrd.AllocatedClaims,
	devicesNeeded int,
	claimParams *intelcrd.GpuClaimParametersSpec,
	owner string) intelcrd.AllocatedGpus {
	klog.V(5).Info("Looking up available GPUs to create VFs")

	newDevices := intelcrd.AllocatedGpus{}

	for parentUID, parent := range available {
		klog.V(5).Infof("Checking device %v (%+v) (consumed %+v) of %+v",
			parentUID, parent, consumed[parentUID], available)
		if parent.Maxvfs == 0 {

			continue
		}
		if consumed[parentUID].Maxvfs != 0 &&
			(!gas.SameOwnerVFAllocations(parentUID, owner) ||
				!sameOwnerUnsubmittedVFAllocations(allocatedClaims, parentUID, owner)) {
			klog.V(5).Infof("Device %v cannot have or already has VFs, skipping", parentUID)

			continue
		}

		// get profile for new VF on this device, act based on profile, not request - profile
		// might have more memory than requested
		newVFMemMiB, profileName, err := sriov.PickVFProfile(available[parentUID].Model, claimParams.Memory, available[parentUID].Ecc)
		if err != nil {
			klog.Errorf("could not determine VF profile for device %v, skipping", parentUID)

			continue
		}

		// in case this function is called from iterating over list of claimAllocations -
		// there will be available GPUs with VFs sketched for provisioning, continue with the last index
		for vfIdx := consumed[parentUID].Maxvfs; vfIdx < parent.Maxvfs && len(newDevices) < devicesNeeded; vfIdx++ {
			klog.V(5).Infof("vf %d, consumed: %+v", vfIdx, consumed[parentUID])
			if (parent.Memory - consumed[parentUID].Memory) >= newVFMemMiB {
				newVFUID := fmt.Sprintf("%s-vf%d", parentUID, vfIdx)
				klog.V(5).Infof("Allocating new VF %v on device %v", newVFUID, parentUID)
				consumed[parentUID].Maxvfs++
				consumed[parentUID].Memory += newVFMemMiB
				newVF := intelcrd.AllocatedFromAllocatable(available[parentUID], claimParams, false) // TODO: share SR-IOV VF, requires classParams.Shared
				newVF.Memory = newVFMemMiB
				newVF.Type = intelcrd.VfDeviceType
				newVF.UID = newVFUID
				newVF.Profile = profileName
				newVF.ParentUID = parentUID
				newDevices = append(newDevices, newVF)
			} else {
				klog.V(5).Infof("Device %v has not enough memory left for requested VF, continuing search",
					parentUID)
				break
			}
		}

		if len(newDevices) == devicesNeeded {
			klog.V(5).Info("Enough VFs were planned")
			break
		}
	}

	return newDevices
}

// selectPotentialGpuDevices selects GPUs devices suitable for request. Should
// be called after potential VF devices were selected, because GPUs with VFs
// provisioned cannot be allocated as plain GPUs.
func (d *driver) selectPotentialGpuDevices(
	available map[string]*intelcrd.AllocatableGpu,
	consumed map[string]*intelcrd.AllocatableGpu,
	claimAllocation *controller.ClaimAllocation) intelcrd.AllocatedClaim {
	claimParams, _ := claimAllocation.ClaimParameters.(*intelcrd.GpuClaimParametersSpec)
	classParams, _ := claimAllocation.ClassParameters.(*intelcrd.GpuClassParametersSpec)

	if claimParams.Millicores == 0 {
		// Use minimum value (to ease calculations) when unspecified.
		claimParams.Millicores = 1
	}

	devices := intelcrd.AllocatedGpus{}

	// selecting the best GPU order here by sorting the available
	ordered := d.preferredOrder(available, consumed)
	for _, deviceUID := range ordered {
		device := available[deviceUID]
		if device.Type == intelcrd.GpuDeviceType && gpuFitsRequest(claimParams, device, consumed[deviceUID], classParams.Shared) {
			allocatedGpu := intelcrd.AllocatedFromAllocatable(device, claimParams, classParams.Shared)

			consumed[deviceUID].Memory += allocatedGpu.Memory
			consumed[deviceUID].Millicores += allocatedGpu.Millicores

			devices = append(devices, allocatedGpu)
		}
		if uint64(len(devices)) == claimParams.Count {
			klog.V(5).Infof("Found enough suitable devices for claim %v (%v): %+v",
				claimAllocation.Claim.UID, len(devices), devices)
			break
		}
	}

	allocation := intelcrd.AllocatedClaim{
		Gpus: devices,
	}

	klog.V(5).Infof("Checking if claim %v has an owner", claimAllocation.Claim.UID)
	if claimAllocation.Claim.OwnerReferences != nil && len(claimAllocation.Claim.OwnerReferences) > 0 {
		allocation.Owner = string(claimAllocation.Claim.OwnerReferences[0].UID)
	}

	klog.V(5).Info("Leaving selectPotentialGpuDevices")
	return allocation
}

// pendingClaimStillValid ensures that the claim fits to previously selected GPU resources.
func (d *driver) pendingClaimStillValid(
	gas *intelcrd.GpuAllocationState,
	claimParams *intelcrd.GpuClaimParametersSpec,
	classParams *intelcrd.GpuClassParametersSpec,
	pendingClaimUID string,
	selectedNode string,
	owner string) bool {
	klog.V(5).Infof("enoughResourcesForPendingClaim called for claim %v", pendingClaimUID)

	if claimParams.Type == intelcrd.GpuDeviceType && claimParams.Millicores == 0 {
		// Use minimum value (to ease calculations) when unspecified.
		claimParams.Millicores = 1
	}

	// has to be copy of allocatable because unavailable GPUs are removed from the available map
	available, consumed := gas.AvailableAndConsumed()

	pendingClaim := d.PendingClaimRequests.get(pendingClaimUID, selectedNode)
	for _, device := range pendingClaim.Gpus {
		if _, exists := available[device.UID]; !exists { // check if it is a VF and can be provisioned
			if len(device.UID) == sriov.PFUIDLength {
				klog.Errorf("device %v from pending claim %v is not VF and is not available", device.UID, pendingClaimUID)
				return false
			}

			// it is a VF
			parentUID, err2 := sriov.PfUIDFromVfUID(device.UID)
			if err2 != nil {
				klog.Errorf("could not get PF from VF UID '%v': %v", device.UID, err2)
				return false
			}

			if gas.DeviceIsAllocated(device.UID) {
				klog.Errorf("device %v is already allocated, need to recalculate allocation %v", device.UID, pendingClaimUID)
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
			if !gpuFitsRequest(claimParams, available[parentUID], consumed[parentUID], classParams.Shared) {
				return false
			}

			klog.V(5).Infof("VF %v is OK to be provisioned", device.UID)
			consumed[parentUID].Maxvfs++
			consumed[parentUID].Memory += claimParams.Memory
			consumed[parentUID].Millicores += claimParams.Millicores
		} else { // if _, exists := available[device.UID]; exists
			if !gpuFitsRequest(claimParams, available[device.UID], consumed[device.UID], classParams.Shared) {
				return false
			}
			// Same VF cannot be used in same ResourceClaim more than once.
			if available[device.UID].Type == intelcrd.VfDeviceType {
				delete(available, device.UID)
				delete(consumed, device.UID)
			} else {
				consumed[device.UID].Memory += claimParams.Memory
				consumed[device.UID].Millicores += claimParams.Millicores
			}
		}
	}

	klog.V(5).Infof("Pending claim allocation %v is still valid", pendingClaimUID)
	return true
}

func sameOwnerUnsubmittedVFAllocations(allocatedClaims intelcrd.AllocatedClaims, parentUID string, owner string) bool {
	klog.V(5).Infof("Checking if all unsubmitted VFs on device %v owned by %v", parentUID, owner)
	if len(allocatedClaims) == 0 {
		klog.V(5).Infof("No VF allocations yet, nothing to check")
		return true
	}

	for _, claimAllocation := range allocatedClaims {
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
	consumed *intelcrd.AllocatableGpu,
	shared bool) bool {

	// Only allowed request.Type / deviceRefSpec.Type mismatch is when
	// VF is requested and GPU is being verified for possibility to have
	// more VFs.
	switch {
	case request.Type == intelcrd.VfDeviceType && deviceRefSpec.Type == intelcrd.GpuDeviceType:
		// Millicores will be non-zero in case GPU is shared by a workload already.
		if deviceRefSpec.Maxvfs == 0 || consumed.Millicores != 0 {
			return false
		}
	case request.Type != deviceRefSpec.Type:
		return false
	case consumed.Maxvfs != 0:
		// If GPU has VFs - do not use it
		// If VF is in use - do not use it.
		// TODO: sharing VFs
		return false
	case !shared && consumed.Millicores != 0:
		// Millicores will be non-zero in case GPU is shared by a workload already.
		return false
	}

	// validate availability of resources requested
	if !validateResourceAvailability("memory", deviceRefSpec.UID, request.Memory, deviceRefSpec.Memory, consumed.Memory) {
		return false
	}

	if !validateResourceAvailability("millicores", deviceRefSpec.UID, request.Millicores, deviceRefSpec.Millicores, consumed.Millicores) {
		return false
	}

	return true
}

func validateResourceAvailability(resourceName, deviceUID string, requested, allocatable, consumed uint64) bool {
	if requested > 0 {
		remaining := allocatable - consumed
		if remaining >= requested {
			klog.V(3).Infof("Sufficient %v on device %v (%v left / %v requested",
				resourceName, deviceUID, remaining, requested)
		} else {
			klog.V(3).Infof("Not enough %v on device %v (%v left / %v requested)",
				resourceName, deviceUID, remaining, requested)
			return false
		}
	} else {
		klog.V(5).Info("Disregarding zero %v request value", resourceName)
	}
	return true
}

func buildAllocationResult(selectedNode string, shareable bool) *resourcev1.AllocationResult {
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
		Shareable:        shareable,
	}
	return allocation
}

func buildMonitorAllocationResult(selectedNode string, shared bool) *resourcev1.AllocationResult {
	allocation := buildAllocationResult(selectedNode, shared)
	allocation.ResourceHandles = []resourcev1.ResourceHandle{
		{
			DriverName: intelcrd.APIGroupName,
			Data:       "monitor",
		},
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
