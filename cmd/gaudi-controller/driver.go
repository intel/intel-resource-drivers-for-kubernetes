/*
 * Copyright (c) 2024, Intel Corporation.  All Rights Reserved.
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
	"sort"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"

	resourcev1 "k8s.io/api/resource/v1alpha2"
	"k8s.io/dynamic-resource-allocation/controller"
	"k8s.io/klog/v2"

	helpers "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/controllerhelpers"
	intelclientset "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gaudi/clientset/versioned"
	intelcrd "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gaudi/v1alpha1/api"
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
}

// compile-time test if implementation is conformant with the interface.
var _ controller.Driver = (*driver)(nil)

func newDriver(config *configType) *driver {
	klog.V(5).Info("Creating new driver")
	driverVersion.PrintDriverVersion(intelcrd.APIGroupName, intelcrd.APIVersion)

	driver := driver{
		lock:                 newPerNodeMutex(),
		namespace:            config.namespace,
		clientset:            config.clientset.intel,
		PendingClaimRequests: newPerNodeClaimRequests(),
	}

	return &driver
}

// GetClassParameters returns GaudiClassParameters after sanitization or error.
func (d driver) GetClassParameters(ctx context.Context, class *resourcev1.ResourceClass) (interface{}, error) {
	klog.V(5).InfoS("GetClassParameters called", "resource class", class.Name)

	if class.ParametersRef == nil {
		return intelcrd.DefaultGaudiClassParametersSpec(), nil
	}

	if class.ParametersRef.APIGroup != apiGroupVersion {
		return nil, fmt.Errorf(
			"incorrect resource-class API group and version: %v, expected: %v",
			class.ParametersRef.APIGroup,
			apiGroupVersion)
	}

	if class.ParametersRef.Kind != intelcrd.GaudiClassParametersKind {
		klog.Errorf("unsupported ResourceClass.ParametersRef.Kind: %v", class.ParametersRef.Kind)
		return nil, fmt.Errorf("unsupported ResourceClass.ParametersRef.Kind: %v", class.ParametersRef.Kind)
	}

	classParams, err := d.clientset.GaudiV1alpha1().GaudiClassParameters().Get(ctx, class.ParametersRef.Name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("could not get GaudiClassParameters '%v': %v", class.ParametersRef.Name, err)
	}

	klog.V(5).Infof("GaudiClassParameters: %+v", classParams.Spec)
	return &classParams.Spec, nil
}

// GetClaimParameters returns GaudiClaimParameters after sanitization or error.
func (d driver) GetClaimParameters(ctx context.Context, claim *resourcev1.ResourceClaim, class *resourcev1.ResourceClass, classParameters interface{}) (interface{}, error) {
	klog.V(5).InfoS("GetClaimParameters called", "resource claim", claim.Namespace+"/"+claim.Name)

	if claim.Spec.ParametersRef == nil {
		return intelcrd.DefaultGaudiClaimParametersSpec(), nil
	}

	if claim.Spec.ParametersRef.APIGroup != apiGroupVersion {
		return nil, fmt.Errorf(
			"incorrect claim spec parameter API group and version: %v, expected: %v",
			claim.Spec.ParametersRef.APIGroup,
			apiGroupVersion)
	}

	if claim.Spec.ParametersRef.Kind != intelcrd.GaudiClaimParametersKind {
		klog.Errorf("unsupported ResourceClaimParametersRef Kind: %v", claim.Spec.ParametersRef.Kind)
		return nil, fmt.Errorf("unsupported ResourceClaimParametersRef Kind: %v", claim.Spec.ParametersRef.Kind)
	}

	claimParams, err := d.clientset.GaudiV1alpha1().GaudiClaimParameters(claim.Namespace).Get(ctx, claim.Spec.ParametersRef.Name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("could not get GaudiClaimParameters '%v' in namespace '%v': %v",
			claim.Spec.ParametersRef.Name, claim.Namespace, err)
	}

	return &claimParams.Spec, nil
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
	d.lock.Get(selectedNode).Lock()
	defer d.lock.Get(selectedNode).Unlock()

	crdconfig := &intelcrd.GaudiAllocationStateConfig{
		Name:      selectedNode,
		Namespace: d.namespace,
	}

	gas := intelcrd.NewGaudiAllocationState(crdconfig, d.clientset)

	gasUpdateErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		err := gas.Get(ctx)
		if err != nil {
			return fmt.Errorf("error retrieving GaudiAllocationState for node %v: %v", selectedNode, err)
		}

		if gas.Status != intelcrd.GaudiAllocationStateStatusReady {
			return fmt.Errorf("GaudiAllocationStateStatus: %v", gas.Status)
		}

		if gas.Spec.AllocatedClaims == nil {
			gas.Spec.AllocatedClaims = intelcrd.AllocatedClaims{}
		}

		gas.UpdateAvailable()

		gasNeedsUpdate := false
		for _, ca := range claims {
			allocation, error := d.allocateSinglePendingClaim(ctx, ca, gas)
			if error != nil {
				ca.Error = error
				continue
			}
			ca.Allocation = allocation
			gasNeedsUpdate = true
		}

		if gasNeedsUpdate {
			if err = gas.Update(ctx, &gas.Spec); err != nil {
				klog.V(5).Infof("error updating GaudiAllocationState: %v", err)
				return err
			}
		}

		return nil
	})

	if gasUpdateErr != nil {
		klog.Errorf("allocating devices on node %v: %v", selectedNode, gasUpdateErr)
		return
	}

	// If both - the allocation and GAS update succeeded, cleanup pending claims.
	for _, claim := range claims {
		if claim.Error == nil {
			// monitoring requests are not in pending list, so nothing happens for them here
			d.PendingClaimRequests.remove(string(claim.Claim.UID))
		}
	}
}

func (d driver) allocateSinglePendingClaim(
	ctx context.Context,
	ca *controller.ClaimAllocation,
	gas *intelcrd.GaudiAllocationState) (*resourcev1.AllocationResult, error) {

	nodename := gas.Name

	classParams, ok := ca.ClassParameters.(*intelcrd.GaudiClassParametersSpec)
	if !ok {
		return nil, fmt.Errorf("error parsing Resource Class Parameters")
	}

	claimParams, ok := ca.ClaimParameters.(*intelcrd.GaudiClaimParametersSpec)
	if !ok {
		return nil, fmt.Errorf("error parsing Resource Claim Parameters")
	}

	claimUID := string(ca.Claim.UID)

	if classParams.Monitor {
		// Monitoring requests use neither pending nor allocated claims structs
		return helpers.BuildMonitorAllocationResult(nodename, true, intelcrd.APIGroupName, intelcrd.MonitorAllocType), nil
	}

	if _, exists := gas.Spec.AllocatedClaims[claimUID]; exists {
		klog.V(5).Infof("GaudiAllocationState already has AllocatedClaim %v, nothing to do", claimUID)
		return helpers.BuildAllocationResult(nodename, false), nil
	}

	if !d.PendingClaimRequests.exists(claimUID, nodename) {
		return nil, fmt.Errorf("no allocation requests generated for claim '%v' on node '%v' yet", claimUID, nodename)
	}

	if !d.pendingClaimStillValid(gas, claimParams, classParams, claimUID, nodename) {
		newAllocation, err := d.selectDevices(gas, []*controller.ClaimAllocation{ca})
		if err != nil {
			klog.V(5).Infof("Insufficient resource for claim %v on node %v", claimUID, nodename)
			return nil, fmt.Errorf("insufficient resources for claim %v on node %v", claimUID, nodename)
		}

		klog.V(5).Infof("Successfully created new allocation for %v", claimUID)
		d.PendingClaimRequests.set(claimUID, gas.Name, newAllocation[claimUID])
	}

	gas.Spec.AllocatedClaims[claimUID] = d.PendingClaimRequests.get(claimUID, nodename)
	klog.V(5).Infof("enough resources for claim %v: %+v", claimUID, gas.Spec.AllocatedClaims[claimUID])

	return helpers.BuildAllocationResult(nodename, false), nil
}

func (d driver) allocateImmediateClaim(
	ctx context.Context,
	claim *resourcev1.ResourceClaim,
	claimParameters interface{},
	class *resourcev1.ResourceClass,
	classParameters interface{},
) (*resourcev1.AllocationResult, error) {
	klog.V(5).Info("Allocating immediately")

	classParams, ok := classParameters.(*intelcrd.GaudiClassParametersSpec)
	if ok && classParams.Monitor {
		return nil, fmt.Errorf("immediate Gaudi *monitor* class claims are unsupported")
	}

	crdconfig := &intelcrd.GaudiAllocationStateConfig{
		Namespace: d.namespace,
	}

	gas := intelcrd.NewGaudiAllocationState(crdconfig, d.clientset)

	gasnames, err := gas.ListNames(ctx)
	if err != nil {
		return nil, fmt.Errorf("error retrieving list of GaudiAllocationState objects: %v", err)
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

	crdconfig := &intelcrd.GaudiAllocationStateConfig{
		Namespace: d.namespace,
		Name:      nodename,
	}

	allocateErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		gas := intelcrd.NewGaudiAllocationState(crdconfig, d.clientset)

		err := gas.Get(ctx)
		if err != nil {
			return fmt.Errorf("error retrieving GaudiAllocationState %v: %v", nodename, err)
		}

		if gas.Status != intelcrd.GaudiAllocationStateStatusReady {
			return fmt.Errorf("GaudiAllocationStateStatus: %v", gas.Status)
		}

		if gas.Spec.AllocatedClaims == nil {
			gas.Spec.AllocatedClaims = intelcrd.AllocatedClaims{}
		}

		gas.UpdateAvailable()

		allocatedClaims, err := d.selectDevices(gas, cas)
		if err != nil {
			klog.V(3).Infof("no suitable devices, skipping node %v", nodename)

			return fmt.Errorf("no suitable devices: %v", err)
		}

		klog.V(5).Infof("Successfully allocated claim %v", cas[0].Claim.UID)

		for claimUID, ca := range allocatedClaims {
			gas.Spec.AllocatedClaims[claimUID] = ca
		}

		// sync AllocatedClaims from allocations
		err = gas.Update(ctx, &gas.Spec)
		if err != nil {
			klog.V(5).Infof("error updating GaudiAllocationState: %v", err)
		}
		return err
	})
	if allocateErr != nil {
		return nil, fmt.Errorf("allocating devices on node %v: %v", nodename, allocateErr)
	}

	return helpers.BuildAllocationResult(nodename, false), nil
}

func (d driver) Deallocate(ctx context.Context, claim *resourcev1.ResourceClaim) error {
	klog.V(5).InfoS("Deallocate called", "resource claim", claim.Namespace+"/"+claim.Name)

	selectedNode := helpers.GetSelectedNode(claim)
	if selectedNode == "" {
		return nil
	}

	d.lock.Get(selectedNode).Lock()
	defer d.lock.Get(selectedNode).Unlock()

	crdconfig := &intelcrd.GaudiAllocationStateConfig{
		Name:      selectedNode,
		Namespace: d.namespace,
	}

	gas := intelcrd.NewGaudiAllocationState(crdconfig, d.clientset)

	deallocateErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		err := gas.Get(ctx)
		if err != nil {
			return fmt.Errorf("error retrieving GaudiAllocationState %v: %v", selectedNode, err)
		}

		claimUID := string(claim.UID)
		_, exists := gas.Spec.AllocatedClaims[claimUID]
		if !exists {
			klog.Warningf("Resource claim %v does not exist internally in resource driver", claimUID)
			return nil
		}

		delete(gas.Spec.AllocatedClaims, claimUID)
		err = gas.Update(ctx, &gas.Spec)
		if err != nil {
			klog.V(5).Infof("error updating GaudiAllocationState: %v", err)
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
// that do not suit request into ClaimAllocation.UnsuitableNodes. It also populates
// internal d.PendingClaimRequests struct for later scheduler Allocate() call.
func (d driver) UnsuitableNodes(
	ctx context.Context, pod *corev1.Pod,
	allcas []*controller.ClaimAllocation, potentialNodes []string) error {
	klog.V(5).Infof("UnsuitableNodes called for %d claim allocations", len(allcas))

	for _, node := range potentialNodes {
		klog.V(5).InfoS("UnsuitableNodes processing", "node", node)
		d.unsuitableNode(ctx, allcas, node)
	}

	for _, ca := range allcas {
		ca.UnsuitableNodes = helpers.Unique(ca.UnsuitableNodes)
	}

	return nil
}

func (d driver) unsuitableNode(ctx context.Context, allcas []*controller.ClaimAllocation, potentialNode string) {
	d.lock.Get(potentialNode).Lock()
	defer d.lock.Get(potentialNode).Unlock()

	crdconfig := &intelcrd.GaudiAllocationStateConfig{
		Name:      potentialNode,
		Namespace: d.namespace,
	}
	gas := intelcrd.NewGaudiAllocationState(crdconfig, d.clientset)

	klog.V(5).InfoS("Getting GaudiAllocationState", "node", potentialNode, "namespace", d.namespace)
	err := gas.Get(ctx)
	if err != nil {
		klog.V(3).Infof("Could not get node %v allocation state: %v", potentialNode, err)
		for _, ca := range allcas {
			klog.V(5).Infof("Adding node %v to unsuitable nodes for CA %v", potentialNode, ca)
			ca.UnsuitableNodes = append(ca.UnsuitableNodes, potentialNode)
		}

		return
	}

	gas.UpdateAvailable()

	if gas.Status != intelcrd.GaudiAllocationStateStatusReady {
		klog.V(3).Infof("GaudiAllocationState status is %v, adding node %v to unsuitable nodes", gas.Status, potentialNode)
		for _, ca := range allcas {
			ca.UnsuitableNodes = append(ca.UnsuitableNodes, potentialNode)
		}

		return
	}

	if gas.Spec.AllocatedClaims == nil {
		gas.Spec.AllocatedClaims = intelcrd.AllocatedClaims{}
	}

	d.unsuitableGaudiNode(gas, allcas)
}

func (d *driver) unsuitableGaudiNode(
	gas *intelcrd.GaudiAllocationState,
	allcas []*controller.ClaimAllocation) {
	// remove pending claim requests that are in GaudiAllocationState already
	d.PendingClaimRequests.cleanupNode(gas)

	filteredCAs := []*controller.ClaimAllocation{} // TODO: might not be needed if get claim parameters called for each

	klog.V(5).Info("Filtering ResourceClaimParameters")
	for _, ca := range allcas {
		if _, ok := ca.ClaimParameters.(*intelcrd.GaudiClaimParametersSpec); !ok {
			klog.V(3).Infof("Unsupported claim parameters type: %T", ca.ClaimParameters)
			continue
		}

		// CA filtering (= pending request struct updates) are skipped for
		// monitoring because potential device checks are not relevant for it
		classParams, ok := ca.ClassParameters.(*intelcrd.GaudiClassParametersSpec)
		if ok && classParams.Monitor {
			continue
		}

		filteredCAs = append(filteredCAs, ca)
	}

	pendingClaims, err := d.selectDevices(gas, filteredCAs)
	if err != nil {
		klog.V(5).Infof("Could not allocate request: %v", err)
		for _, ca := range allcas {
			ca.UnsuitableNodes = append(ca.UnsuitableNodes, gas.Name)
		}
		return
	}

	klog.V(5).Info("Saving pending allocations")
	for claimUID, ca := range pendingClaims {
		d.PendingClaimRequests.set(claimUID, gas.Name, ca)
	}
}

func (d *driver) selectDevices(gas *intelcrd.GaudiAllocationState, caRequests []*controller.ClaimAllocation) (intelcrd.AllocatedClaims, error) {

	cas := intelcrd.AllocatedClaims{}

	for _, ca := range caRequests {
		claimUID := string(ca.Claim.UID)
		klog.V(5).Infof("Selecting devices for claim %v", claimUID)

		claimParams, _ := ca.ClaimParameters.(*intelcrd.GaudiClaimParametersSpec)
		devices := intelcrd.AllocatedDevices{}

		if uint64(len(gas.Available)) < claimParams.Count {
			return nil, fmt.Errorf("not enough devices")
		}

		// Order otherwise unpredictable map order for repeatable testing.
		for _, deviceUID := range orderedDeviceIds(gas.Available) {
			devices = append(devices, intelcrd.AllocatedDevice{UID: deviceUID})
			delete(gas.Available, deviceUID)
			if uint64(len(devices)) == claimParams.Count {
				break
			}
		}

		cas[claimUID] = intelcrd.AllocatedClaim{
			Devices: devices,
		}
	}

	return cas, nil
}

func orderedDeviceIds(devices map[string]intelcrd.AllocatableDevice) []string {
	deviceIds := make([]string, len(devices))
	idx := 0
	for deviceId := range devices {
		deviceIds[idx] = deviceId
		idx++
	}
	sort.Strings(deviceIds)
	return deviceIds
}

// pendingClaimStillValid ensures that previously selected devices are still available resources.
func (d *driver) pendingClaimStillValid(
	gas *intelcrd.GaudiAllocationState,
	claimParams *intelcrd.GaudiClaimParametersSpec,
	classParams *intelcrd.GaudiClassParametersSpec,
	pendingClaimUID string,
	selectedNode string) bool {
	klog.V(5).Infof("pendingClaimStillValid called for claim %v", pendingClaimUID)

	pendingClaim := d.PendingClaimRequests.get(pendingClaimUID, selectedNode)
	for _, device := range pendingClaim.Devices {
		if _, exists := gas.Available[device.UID]; !exists {
			klog.Errorf("device %v from pending claim %v is not available", device.UID, pendingClaimUID)
			return false
		}

		delete(gas.Available, device.UID)
	}

	klog.V(5).Infof("Pending claim allocation %v is still valid", pendingClaimUID)
	return true
}
