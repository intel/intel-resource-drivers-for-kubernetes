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
	"path"
	"path/filepath"

	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	drav1 "k8s.io/kubelet/pkg/apis/dra/v1alpha3"

	intelcrd "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gpu/v1alpha2/api"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/sriov"
	driverVersion "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/version"
)

// compile-time test for implementation conformance with the interface.
var _ drav1.NodeServer = (*driver)(nil)

type driver struct {
	gas          *intelcrd.GpuAllocationState
	state        *nodeState
	sysfsI915Dir string
	sysfsDRMDir  string
}

const (
	devDriEnvVarName = "DEV_DRI_PATH"
	sysfsEnvVarName  = "SYSFS_ROOT"
	// driver.sysfsI915Dir and driver.sysfsDRMDir are sysfsI915path and sysfsDRMpath
	// respectively prefixed with $SYSFS_ROOT.
	sysfsI915path = "bus/pci/drivers/i915"
	sysfsDRMpath  = "class/drm/"
	pciAddressRE  = `[0-9a-f]{4}:[0-9a-f]{2}:[0-9a-f]{2}\.[0-7]$`
	cardRE        = `^card[0-9]+$`
	renderdIdRE   = `^renderD[0-9]+$`
)

func newDriver(ctx context.Context, config *configType) (*driver, error) {
	var state *nodeState

	driverVersion.PrintDriverVersion()

	sysfsDir := getSysfsDir()
	sysfsI915Dir := filepath.Join(sysfsDir, sysfsI915path)
	sysfsDRMDir := filepath.Join(sysfsDir, sysfsDRMpath)

	gas := intelcrd.NewGpuAllocationState(config.crdconfig, config.clientset.intel)

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

		detectedDevices, err := enumerateAllPossibleDevices(sysfsI915Dir, sysfsDRMDir)
		if err != nil {
			return fmt.Errorf("failed to enumerate supported devices")
		}

		if len(detectedDevices) == 0 {
			klog.Infof("No supported devices detected")
		}

		klog.V(3).Info("Creating new NodeState")
		state, err = newNodeState(gas, detectedDevices, config.cdiRoot)
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
		gas:          gas,
		state:        state,
		sysfsI915Dir: sysfsI915Dir,
		sysfsDRMDir:  sysfsDRMDir,
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
		preparedResources.Claims[claim.Uid] = d.nodePrepareResources(ctx, claim)
	}

	return preparedResources, nil
}

func (d *driver) nodePrepareResources(
	ctx context.Context, claim *drav1.Claim) *drav1.NodePrepareResourceResponse {
	klog.V(5).Infof("NodePrepareResource is called: request: %+v", claim)

	var cdinames []string

	// provide all devices for monitoring claims
	if claim.ResourceHandle == "monitor" {
		cdinames = d.state.getMonitorCDINames(claim.Uid)
		klog.V(3).Infof("Prepared devices for monitor claim '%v': %s", claim.Uid, cdinames)
		return &drav1.NodePrepareResourceResponse{CDIDevices: cdinames}
	}

	prepareErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		err := d.gas.Get(ctx)
		if err != nil {
			return fmt.Errorf("failed to get GpuAllocationState: %v", err)
		}

		// map[string:parent_uid][string:vf_uid]vfInfo:*DeviceInfo
		toProvision, err := d.sanitizedClaimDevicesToBeProvisioned(claim)
		if err != nil {
			return err
		}

		if len(toProvision) != 0 {
			klog.V(5).Infof("Need to provision VFs on %d GPUs", len(toProvision))
			d.pickupMoreClaims(claim.Uid, toProvision)

			// VF validation should be called after all claims that need preparation
			// have been gathered into toProvision
			if err := validateVFsToBeProvisioned(toProvision); err != nil {
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
		}

		// add resource claim to prepared list
		err = d.state.makePreparedClaimAllocation(claim.Uid, d.gas.Spec.AllocatedClaims[claim.Uid])
		if err != nil {
			return fmt.Errorf("failed creating prepared claim allocation: %v", err)
		}

		// GAS needs to be updated even if no VFs were provisioned to have preparedClaims entry
		err = d.gas.Update(ctx, d.state.GetUpdatedSpec(&d.gas.Spec))
		if err != nil {
			klog.V(5).Infof("failed to update GpuAllocationState: %v", err)
			return err
		}

		cdinames = d.state.GetAllocatedCDINames(claim.Uid)
		if len(cdinames) == 0 {
			return fmt.Errorf("could not find CDI device name from CDI registry")
		}

		return nil
	})

	if prepareErr != nil {
		return &drav1.NodePrepareResourceResponse{Error: fmt.Sprintf("error preparing resource: %v", prepareErr)}
	}

	klog.V(3).Infof("Prepared devices for claim '%v': %s", claim.Uid, cdinames)
	return &drav1.NodePrepareResourceResponse{CDIDevices: cdinames}
}

func (d *driver) NodeUnprepareResources(ctx context.Context, req *drav1.NodeUnprepareResourcesRequest) (*drav1.NodeUnprepareResourcesResponse, error) {
	klog.Infof("NodeUnprepareResource is called: number of claims: %d", len(req.Claims))
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
	if claim.ResourceHandle == "monitor" {
		klog.V(3).Infof("Freed devices for monitor claim '%v'", claim.Uid)
		return &drav1.NodeUnprepareResourceResponse{}
	}

	unprepareErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		err := d.gas.Get(ctx)
		if err != nil {
			return fmt.Errorf("error freeing devices for claim '%v': %v", claim.Uid, err)
		}

		parentsToCleanup, err := d.state.FreeClaimDevices(claim.Uid, &d.gas.Spec)
		if err != nil {
			return fmt.Errorf("error freeing devices for claim '%v': %v", claim.Uid, err)
		}

		// If there are no VFs used in prepared, remove VFs from this Gpu.
		// uid is pci DBDF with device pci id, e.g. 0000:00:02.0-0x56c0
		if err := d.removeAllVFsFromParents(parentsToCleanup); err != nil {
			klog.Errorf("failed to remove VFs: %v", err)
			return fmt.Errorf("failed to remove VFs: %v", err)
		}

		err = d.gas.Update(ctx, d.state.GetUpdatedSpec(&d.gas.Spec))
		if err != nil {
			klog.V(5).Infof("failed to update GpuAllocationState: %v", err)
			return err
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
func (d *driver) sanitizedClaimDevicesToBeProvisioned(claim *drav1.Claim) (map[string]map[string]*DeviceInfo, error) {
	toProvision := map[string]map[string]*DeviceInfo{}

	// map VFs in req context that need provisioning against parent uids
	for _, device := range d.gas.Spec.AllocatedClaims[claim.Uid].Gpus {
		if device.Type == intelcrd.VfDeviceType {
			existingVF, exists := d.state.allocatable[device.UID]
			if !exists {
				parentDevice, exists := d.state.allocatable[device.ParentUID]
				if !exists {
					return nil, fmt.Errorf("no parent device '%v' for VF %v", device.ParentUID, device.UID)
				}

				// allocatable devices have no profile field. TODO: add such field.
				// In case the controller allocated existing VF leaving profile blank,
				// and VFs dismantling began before the claim came into preparation,
				// the allocated device profile is effectively lost -> pick up new suitable profile.
				if device.Profile == "" {
					_, newProfile, err := sriov.PickVFProfile(parentDevice.model, device.Memory, parentDevice.eccOn)
					if err != nil {
						return nil, fmt.Errorf("no suitable VF profile for device %v", device.UID)
					}
					klog.V(5).Infof("picked profile %v for device %v", newProfile, device.UID)
					device.Profile = newProfile
				} else if !sriov.DeviceProfileExists(parentDevice.model, device.Profile) {
					return nil, fmt.Errorf("no profile %v found for device %v (deviceId: %v)", device.Profile, device.UID, parentDevice.model)
				}
				klog.Infof("VF %v is not provisioned", device.UID)
				if _, parentInPlanned := toProvision[device.ParentUID]; !parentInPlanned {
					toProvision[device.ParentUID] = map[string]*DeviceInfo{}
				}
				toProvision[device.ParentUID][device.UID] = d.state.DeviceInfoFromAllocated(device)
			} else {
				klog.V(5).Infof("VF %v is already provisioned", device.UID)
				// verify profile and parent fields
				if existingVF.parentuid != device.ParentUID || existingVF.memoryMiB != device.Memory || (device.Profile != "" && existingVF.vfprofile != device.Profile) {
					return nil, fmt.Errorf("malformed allocated device %v: fields mismatch existing allocatable device", device.UID)
				}
			}
		}
	}

	return toProvision, nil
}

// getSysfsPath tries to get path where sysfs is mounted from
// env var, or fallback to hardcoded path.
func getSysfsDir() string {
	sysfsPath, found := os.LookupEnv(sysfsEnvVarName)

	if found {
		if _, err := os.Stat(path.Join(sysfsPath, sysfsDRMpath)); err == nil {
			klog.Infof("using custom sysfs location: %v", sysfsPath)
			return sysfsPath
		}
	}

	klog.Infof("using default sysfs location: /sys")
	// If /sys is not available, devices discovery will fail gracefully.
	return "/sys"
}

func getDevfsDriDir() string {
	devfsDriDir, found := os.LookupEnv(devDriEnvVarName)

	if found {
		klog.Infof("using custom devfs dri location: %v", devfsDriDir)
		return devfsDriDir
	}

	klog.Infof("using default devfs dri location: /dev/dri")
	return "/dev/dri"
}
