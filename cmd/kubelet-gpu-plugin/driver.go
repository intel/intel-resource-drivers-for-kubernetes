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
	"path"

	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	drav1 "k8s.io/kubelet/pkg/apis/dra/v1alpha3"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/discovery"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/sriov"
	intelcrd "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gpu/v1alpha2/api"
	driverVersion "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/version"
)

// compile-time test for implementation conformance with the interface.
var _ drav1.NodeServer = (*driver)(nil)

type driver struct {
	// Resource model publisher uses this channel to know when to send updated model.
	updateCh chan bool
	// Resource model publisher uses this channel to know when to stop sending updates to the kubelet and quit.
	doneCh   chan bool
	gas      *intelcrd.GpuAllocationState
	state    *nodeState
	sysfsDir string
}

func newDriver(ctx context.Context, config *configType) (*driver, error) {
	var state *nodeState

	driverVersion.PrintDriverVersion(intelcrd.APIGroupName, intelcrd.APIVersion)

	sysfsDir := device.GetSysfsDir()
	gas := intelcrd.NewGpuAllocationState(config.crdconfig, config.clientset.intel)

	preparedClaimFilePath := path.Join(config.driverPluginPath, device.PreparedClaimsFileName)

	setupErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		klog.V(3).Info("Creating new GpuAllocationState")
		err := gas.GetOrCreate(ctx)
		if err != nil {
			return fmt.Errorf("failed to get GpuAllocationState: %v", err)
		}

		klog.V(3).Info("Setting GpuAllocationState as NotReady")
		err = gas.UpdateStatus(ctx, intelcrd.GpuAllocationStateStatusNotReady)
		if err != nil {
			return fmt.Errorf("failed to set GpuAllocationState as NotReady: %v", err)
		}

		detectedDevices := discovery.DiscoverDevices(sysfsDir)
		if len(detectedDevices) == 0 {
			klog.Info("No supported devices detected")
		}

		klog.V(3).Info("Creating new NodeState")
		state, err = newNodeState(detectedDevices, config.cdiRoot, preparedClaimFilePath)
		if err != nil {
			return fmt.Errorf("failed to create new NodeState: %v", err)
		}

		klog.V(3).Info("Updating GpuAllocationState with detected GPUs")
		err = gas.Update(ctx, state.GetUpdatedSpec(&gas.Spec))
		if err != nil {
			return fmt.Errorf("failed to update GpuAllocationState: %v", err)
		}

		klog.V(3).Info("Setting GpuAllocationState status as Ready")
		err = gas.UpdateStatus(ctx, intelcrd.GpuAllocationStateStatusReady)
		if err != nil {
			return fmt.Errorf("failed to set GpuAllocationState status as Ready: %v", err)
		}

		return nil
	})
	if setupErr != nil {
		return nil, fmt.Errorf("creating driver: %v", setupErr)
	}

	d := &driver{
		gas:      gas,
		state:    state,
		sysfsDir: sysfsDir,
	}
	klog.V(3).Info("Finished creating new driver")

	return d, nil
}

func (d *driver) NodePrepareResources(ctx context.Context, req *drav1.NodePrepareResourcesRequest) (*drav1.NodePrepareResourcesResponse, error) {
	klog.V(5).Infof("NodePrepareResource is called: request: %+v", req)

	preparedResources := &drav1.NodePrepareResourcesResponse{Claims: map[string]*drav1.NodePrepareResourceResponse{}}

	// In production version some common operations of d.nodeUnprepareResources
	// should be done outside of the loop, for instance updating the CR could
	// be done once after all HW was prepared.
	for _, claim := range req.Claims {
		if claim.StructuredResourceHandle != nil && len(claim.StructuredResourceHandle) != 0 {
			preparedResources.Claims[claim.Uid] = d.nodePrepareStructuredResource(ctx, claim)
		} else {
			preparedResources.Claims[claim.Uid] = d.nodePrepareResources(ctx, claim)
		}
	}

	return preparedResources, nil
}

func (d *driver) nodePrepareResources(
	ctx context.Context, claim *drav1.Claim) *drav1.NodePrepareResourceResponse {
	klog.V(5).Infof("NodePrepareResource is called: request: %+v", claim)

	// provide all devices for monitoring claims
	if claim.ResourceHandle == intelcrd.MonitorAllocType {
		cdinames := d.state.getMonitorCDINames(claim.Uid)
		klog.V(3).Infof("Prepared devices for monitor claim '%v': %s", claim.Uid, cdinames)
		return &drav1.NodePrepareResourceResponse{CDIDevices: cdinames}
	}

	// TODO: move retry and gas.Get outside of caller's Claims loop
	prepareErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {

		if _, found := d.state.prepared[claim.Uid]; found {
			klog.V(3).Infof("Claim %s was already prepared, nothing to do", claim.Uid)
			return nil
		}

		err := d.gas.Get(ctx)
		if err != nil {
			return fmt.Errorf("failed to get GpuAllocationState: %v", err)
		}

		// perClaimDevices and toProvision are mutated below by calls taking them as parameters
		perClaimDevices := map[string][]*device.DeviceInfo{}
		toProvision, claimDevices, err := d.sanitizedClaimDevicesToBeProvisioned(claim)
		if err != nil {
			return err
		}

		perClaimDevices[claim.Uid] = claimDevices

		if len(toProvision) != 0 {
			klog.V(5).Infof("Need to provision VFs on %d GPUs", len(toProvision))

			d.pickupMoreClaims(claim.Uid, toProvision, perClaimDevices)

			// VF validation should be called after all claims that need preparation
			// have been gathered into toProvision
			if err := d.validateVFsToBeProvisioned(toProvision); err != nil {
				return err
			}

			d.reuseLeftoverSRIOVResources(toProvision)

			provisionedVFs, err := d.provisionVFs(toProvision)
			if err != nil {
				klog.Errorf("Could not prepare resource: %v", err)
				return err
			}

			// add to CDI registry and d.allocatable
			err = d.state.addNewVFs(provisionedVFs)
			if err != nil {
				return err
			}

			// GAS needs to be updated even if no VFs were provisioned to have preparedClaims entry
			err = d.gas.Update(ctx, d.state.GetUpdatedSpec(&d.gas.Spec))
			if err != nil {
				klog.V(5).Infof("failed to update GpuAllocationState: %v", err)
				return err
			}
		}

		// add resource claim to prepared list
		err = d.state.makePreparedClaimAllocation(perClaimDevices)
		if err != nil {
			return fmt.Errorf("failed creating prepared claim allocation: %v", err)
		}

		return nil
	})

	if prepareErr != nil {
		return &drav1.NodePrepareResourceResponse{Error: fmt.Sprintf("error preparing resource: %v", prepareErr)}
	}

	return d.cdiDevices(claim.Uid)
}

func (d *driver) cdiDevices(claimUID string) *drav1.NodePrepareResourceResponse {
	cdinames := d.state.GetAllocatedCDINames(claimUID)
	if len(cdinames) == 0 {
		klog.Errorf("could not find CDI device name from CDI registry for claim %s", claimUID)
		return &drav1.NodePrepareResourceResponse{Error: "error preparing resource: CDI devices not found in specs"}
	}

	klog.V(3).Infof("Prepared devices for claim '%v': %s", claimUID, cdinames)
	return &drav1.NodePrepareResourceResponse{CDIDevices: cdinames}
}

func (d *driver) NodeUnprepareResources(ctx context.Context, req *drav1.NodeUnprepareResourcesRequest) (*drav1.NodeUnprepareResourcesResponse, error) {
	klog.V(5).Infof("NodeUnprepareResource is called: number of claims: %d", len(req.Claims))
	unpreparedResources := &drav1.NodeUnprepareResourcesResponse{
		Claims: map[string]*drav1.NodeUnprepareResourceResponse{},
	}

	// In production version some common operations of d.nodeUnprepareResources
	// should be done outside of the loop, for instance updating the CR could
	// be done once after all HW was unprepared.
	for _, claim := range req.Claims {
		unpreparedResources.Claims[claim.Uid] = d.nodeUnprepareResource(ctx, claim)
	}

	return unpreparedResources, nil
}

func (d *driver) nodeUnprepareResource(ctx context.Context, claim *drav1.Claim) *drav1.NodeUnprepareResourceResponse {
	klog.V(3).Infof("NodeUnprepareResource is called: claim: %+v", claim)

	// no-op for monitoring claims
	if claim.ResourceHandle == intelcrd.MonitorAllocType {
		klog.V(3).Infof("Freed devices for monitor claim '%v'", claim.Uid)
		return &drav1.NodeUnprepareResourceResponse{}
	}

	unprepareErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		err := d.gas.Get(ctx)
		if err != nil {
			return fmt.Errorf("error freeing devices for claim '%v': %v", claim.Uid, err)
		}

		parentsToCleanup, err := d.state.FreeClaimDevices(claim.Uid)
		if err != nil {
			return fmt.Errorf("error freeing devices for claim '%v': %v", claim.Uid, err)
		}

		if len(parentsToCleanup) != 0 {
			// If there are no VFs used in prepared, remove VFs from this Gpu.
			// uid is PCI address with device PCI ID, e.g. 0000-00-02-0-0x56c0
			if err := d.removeAllVFsFromParents(parentsToCleanup); err != nil {
				klog.Errorf("failed to remove VFs: %v", err)
				return fmt.Errorf("failed to remove VFs: %v", err)
			}

			err = d.gas.Update(ctx, d.state.GetUpdatedSpec(&d.gas.Spec))
			if err != nil {
				klog.V(5).Infof("failed to update GpuAllocationState: %v", err)
				return err
			}
		}

		return nil
	})

	if unprepareErr != nil {
		return &drav1.NodeUnprepareResourceResponse{Error: fmt.Sprintf("error unpreparing resource: %v", unprepareErr)}
	}

	klog.V(3).Infof("Freed devices for claim '%v'", claim.Uid)
	return &drav1.NodeUnprepareResourceResponse{}
}

// sanitizedClaimDevicesToBeProvisioned returns a map of sanitized devices that need provisioning or an error
// in case sanitization failed.
func (d *driver) sanitizedClaimDevicesToBeProvisioned(claim *drav1.Claim) (map[string][]*device.DeviceInfo, []*device.DeviceInfo, error) {
	toProvision := map[string][]*device.DeviceInfo{}
	claimDevices := []*device.DeviceInfo{}

	// map VFs in req context that need provisioning against parent uids
	for _, gpu := range d.gas.Spec.AllocatedClaims[claim.Uid].Gpus {
		if gpu.Type == intelcrd.VfDeviceType {

			if gpu.UID != intelcrd.NewVFUID { // Allocated VF existed at the time of allocation

				if existingVF, exists := d.state.allocatable[gpu.UID]; exists {

					klog.V(5).Infof("VF %v is already provisioned", gpu.UID)
					// verify profile and parent fields
					if existingVF.ParentUID != gpu.ParentUID || existingVF.MemoryMiB != gpu.Memory || (gpu.Profile != "" && existingVF.VFProfile != gpu.Profile) {

						return nil, nil, fmt.Errorf("malformed allocated device %v: fields mismatch existing allocatable device", gpu.UID)
					}

					claimDevices = append(claimDevices, d.state.DeviceInfoFromAllocated(gpu))
					continue
				}

				klog.V(5).Infof("Allocated VF %v was removed, needs provisioning", gpu.UID)
				gpu.UID = intelcrd.NewVFUID
			}

			parentDevice, exists := d.state.allocatable[gpu.ParentUID]
			if !exists {
				return nil, nil, fmt.Errorf("no parent device '%v' for VF %v", gpu.ParentUID, gpu.UID)
			}

			// allocatable devices have no profile field. TODO: add such field.
			// In case the controller allocated existing VF leaving profile blank,
			// and VFs dismantling began before the claim came into preparation,
			// the allocated device profile is effectively lost -> pick up new suitable profile.
			if gpu.Profile == "" {
				_, _, newProfile, err := sriov.PickVFProfile(parentDevice.Model, gpu.Memory, gpu.Millicores, parentDevice.EccOn)
				if err != nil {
					return nil, nil, fmt.Errorf("no suitable VF profile for device %v", gpu.UID)
				}
				klog.V(5).Infof("picked profile %v for device %v", newProfile, gpu.UID)
				gpu.Profile = newProfile
			} else if !sriov.DeviceProfileExists(parentDevice.Model, gpu.Profile) {
				return nil, nil, fmt.Errorf("no profile %v found for device %v (deviceId: %v)", gpu.Profile, gpu.UID, parentDevice.Model)
			}

			if _, parentInPlanned := toProvision[gpu.ParentUID]; !parentInPlanned {
				toProvision[gpu.ParentUID] = []*device.DeviceInfo{}
			}
			newDevice := d.state.DeviceInfoFromAllocated(gpu)
			toProvision[gpu.ParentUID] = append(toProvision[gpu.ParentUID], newDevice)
			claimDevices = append(claimDevices, newDevice)

			continue
		}

		// GPUs
		claimDevices = append(claimDevices, d.state.DeviceInfoFromAllocated(gpu))
	}

	return toProvision, claimDevices, nil
}
