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
	"encoding/json"
	"fmt"
	"os"
	"path"
	"sync"
	"time"

	resourcev1 "k8s.io/api/resource/v1beta1"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
	"k8s.io/klog/v2"
	drav1 "k8s.io/kubelet/pkg/apis/dra/v1beta1"
	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"
	cdiparser "tags.cncf.io/container-device-interface/pkg/parser"
	cdiSpecs "tags.cncf.io/container-device-interface/specs-go"

	cdihelpers "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gaudi/cdihelpers"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gaudi/device"
)

type ClaimPreparations map[string][]*drav1.Device

type nodeState struct {
	sync.Mutex
	cdiCache               *cdiapi.Cache
	allocatable            device.DevicesInfo
	prepared               ClaimPreparations
	preparedClaimsFilePath string
	nodeName               string
}

func newNodeState(ctx context.Context, detectedDevices map[string]*device.DeviceInfo, cdiRoot string, preparedClaimsFilePath string, nodeName string) (*nodeState, error) {
	for ddev := range detectedDevices {
		klog.V(3).Infof("new device: %+v", ddev)
	}

	klog.V(5).Info("Refreshing CDI registry")
	if err := cdiapi.Configure(cdiapi.WithSpecDirs(cdiRoot)); err != nil {
		return nil, fmt.Errorf("unable to refresh the CDI registry: %v", err)
	}

	cdiCache := cdiapi.GetDefaultCache()

	// syncDetectedDevicesWithRegistry overrides uid in detecteddevices from existing cdi spec
	if err := cdihelpers.SyncDetectedDevicesWithRegistry(cdiCache, detectedDevices, true); err != nil {
		return nil, fmt.Errorf("unable to sync detected devices to CDI registry: %v", err)
	}

	time.Sleep(250 * time.Millisecond)

	klog.V(5).Info("Allocatable devices after CDI registry refresh:")
	for duid, ddev := range detectedDevices {
		klog.V(5).Infof("CDI device: %v : %+v", duid, ddev)
	}

	// TODO: should be only create prepared claims, discard old preparations. Do we even need the snapshot?
	preparedClaims, err := getOrCreatePreparedClaims(preparedClaimsFilePath)
	if err != nil {
		klog.Errorf("Error getting prepared claims: %v", err)
		return nil, fmt.Errorf("failed to get prepared claims: %v", err)
	}

	klog.V(5).Info("Creating NodeState")
	// TODO: allocatable should include cdi-described
	state := &nodeState{
		cdiCache:               cdiCache,
		allocatable:            detectedDevices,
		prepared:               preparedClaims,
		preparedClaimsFilePath: preparedClaimsFilePath,
		nodeName:               nodeName,
	}

	/*
		klog.V(5).Info("Syncing allocatable devices")
		err = state.syncPreparedDevicesFromFile(clientset, preparedClaims)
		if err != nil {
			return nil, fmt.Errorf("unable to sync allocated devices from GaudiAllocationState: %v", err)
		}
	*/

	klog.V(5).Infof("Synced state with CDI and GaudiAllocationState: %+v", state)
	for duid, ddev := range state.allocatable {
		klog.V(5).Infof("Allocatable device: %v : %+v", duid, ddev)
	}

	return state, nil
}

// FreeClaimDevices cleans up prepared claims records and returns error if it was encountered, otherwise nil.
func (s *nodeState) FreeClaimDevices(claimUID string) error {
	s.Lock()
	defer s.Unlock()

	if s.prepared[claimUID] == nil {
		return nil
	}

	klog.V(5).Infof("Freeing devices from claim %v", claimUID)
	delete(s.prepared, claimUID)

	// write prepared claims to file
	if err := writePreparedClaimsToFile(s.preparedClaimsFilePath, s.prepared); err != nil {
		return fmt.Errorf("failed to write prepared claims to file: %v", err)
	}

	return cdihelpers.DeleteDeviceAndWrite(s.cdiCache, claimUID)
}

func (s *nodeState) GetResources() kubeletplugin.Resources {
	devices := []resourcev1.Device{}

	for gaudiUID, gaudi := range s.allocatable {
		newDevice := resourcev1.Device{
			Name: gaudiUID,
			Basic: &resourcev1.BasicDevice{
				Attributes: map[resourcev1.QualifiedName]resourcev1.DeviceAttribute{
					"model": {
						StringValue: &gaudi.ModelName,
					},
					"pciRoot": {
						StringValue: &gaudi.PCIRoot,
					},
				},
			},
		}

		devices = append(devices, newDevice)
	}

	return kubeletplugin.Resources{Devices: devices}
}

// cdiHabanaEnvVar ensures there is a CDI device with name == claimUID, that has
// only env vars for Habana Runtime, without device nodes.
func (s *nodeState) cdiHabanaEnvVar(claimUID string, visibleDevices string) error {
	cdidev := s.cdiCache.GetDevice(claimUID)
	if cdidev != nil { // overwrite the contents
		cdidev.Device.ContainerEdits = cdiSpecs.ContainerEdits{
			Env: []string{visibleDevices},
		}

		// Save into the same spec where the device was found.
		deviceSpec := cdidev.GetSpec()
		specName := path.Base(deviceSpec.GetPath())
		if err := s.cdiCache.WriteSpec(deviceSpec.Spec, specName); err != nil {
			return err
		}

		return nil
	}

	// Create new CDI device and save into first vendor spec.
	newDevice := cdiSpecs.Device{
		Name: claimUID,
		ContainerEdits: cdiSpecs.ContainerEdits{
			Env: []string{visibleDevices},
		},
	}

	if err := cdihelpers.AddDeviceToAnySpec(s.cdiCache, device.CDIVendor, newDevice); err != nil {
		return fmt.Errorf("could not add CDI device into CDI registry: %v", err)
	}

	return nil
}

/*
func (s *nodeState) syncPreparedDevicesFromFile(preparedClaims ClaimPreparations) error {
	klog.V(5).Infof("Syncing %d Prepared allocations from GaudiAllocationState to internal state", len(preparedClaims))

	if s.prepared == nil {
		s.prepared = make(ClaimPreparations)
	}

	for claimuid, preparedDevices := range preparedClaims {
		skipPreparedClaim := false
		prepared := []*device.DeviceInfo{}
		for _, preparedDevice := range preparedDevices {
			klog.V(5).Infof("claim %v had device %+v", claimuid, preparedDevice)

			if _, exists := s.allocatable[preparedDevice.UID]; !exists {
				klog.Errorf("prepared device %v no longer available for claim %v, dropping claim preparation", preparedDevice.UID, claimuid)
				skipPreparedClaim = true
				break
			}

			newdevice := s.allocatable[preparedDevice.UID].DeepCopy()
			prepared = append(prepared, newdevice)
		}

		if !skipPreparedClaim {
			s.prepared[claimuid] = prepared
		}
	}

	return nil
}
*/

func (s *nodeState) Prepare(ctx context.Context, claim *resourcev1.ResourceClaim) error {
	if claim.Status.Allocation == nil {
		return fmt.Errorf("no allocation found in claim %v/%v status", claim.Namespace, claim.Name)
	}

	allocatedDevices := []*drav1.Device{}
	visibleDevices := device.VisibleDevicesEnvVarName + "="
	devs := 0

	for _, allocatedDevice := range claim.Status.Allocation.Devices.Results {
		// ATM the only pool is cluster node's pool: all devices on current node.
		if allocatedDevice.Driver != device.DriverName || allocatedDevice.Pool != s.nodeName {
			klog.FromContext(ctx).Info("ignoring claim allocation device", allocatedDevice)
			continue
		}

		allocatableDevice, found := s.allocatable[allocatedDevice.Device]
		if !found {
			return fmt.Errorf("could not find allocatable device %v (pool %v)", allocatedDevice.Device, allocatedDevice.Pool)
		}

		newDevice := drav1.Device{
			RequestNames: []string{allocatedDevice.Request},
			PoolName:     allocatedDevice.Pool,
			DeviceName:   allocatedDevice.Device,
			CDIDeviceIDs: []string{allocatableDevice.CDIName()},
		}
		allocatedDevices = append(allocatedDevices, &newDevice)

		devs++
		if devs > 1 {
			visibleDevices += ","
		}
		visibleDevices += fmt.Sprintf("%v", allocatableDevice.DeviceIdx)
	}

	if devs > 0 {
		if err := s.cdiHabanaEnvVar(string(claim.UID), visibleDevices); err != nil {
			return fmt.Errorf("failed ensuring Habana Runtime specific CDI device: %v", err)
		}

		cdiName := cdiparser.QualifiedName(device.CDIVendor, device.CDIClass, string(claim.UID))
		allocatedDevices[0].CDIDeviceIDs = append(allocatedDevices[0].CDIDeviceIDs, cdiName)
	}

	s.prepared[string(claim.UID)] = allocatedDevices

	err := writePreparedClaimsToFile(s.preparedClaimsFilePath, s.prepared)
	if err != nil {
		klog.Errorf("Error writing prepared claims to file: %v", err)
		return fmt.Errorf("failed to write prepared claims to file: %v", err)
	}

	klog.V(5).Infof("Created prepared claim %v allocation", claim.UID)
	return nil
}

// getOrCreatePreparedClaims reads a PreparedClaim from a file and deserializes it or creates the file.
func getOrCreatePreparedClaims(preparedClaimsFilePath string) (ClaimPreparations, error) {
	if _, err := os.Stat(preparedClaimsFilePath); os.IsNotExist(err) {
		klog.V(5).Infof("could not find file %v. Creating file", preparedClaimsFilePath)
		f, err := os.OpenFile(preparedClaimsFilePath, os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			return nil, fmt.Errorf("failed creating file %v. Err: %v", preparedClaimsFilePath, err)
		}
		defer f.Close()

		if _, err := f.WriteString("{}"); err != nil {
			return nil, fmt.Errorf("failed writing to file %v. Err: %v", preparedClaimsFilePath, err)
		}

		klog.V(5).Infof("empty prepared claims file created %v", preparedClaimsFilePath)

		return ClaimPreparations{}, nil
	}

	return readPreparedClaimsFromFile(preparedClaimsFilePath)
}

// readPreparedClaimToFile returns unmarshaled content for given prepared claims JSON file.
func readPreparedClaimsFromFile(preparedClaimsFilePath string) (ClaimPreparations, error) {

	preparedClaims := make(ClaimPreparations)

	preparedClaimsConfigBytes, err := os.ReadFile(preparedClaimsFilePath)
	if err != nil {
		klog.V(5).Infof("could not read prepared claims configuration from file %v. Err: %v", preparedClaimsFilePath, err)
		return nil, fmt.Errorf("failed reading file %v. Err: %v", preparedClaimsFilePath, err)
	}

	if err := json.Unmarshal(preparedClaimsConfigBytes, &preparedClaims); err != nil {
		klog.V(5).Infof("Could not parse default prepared claims configuration from file %v. Err: %v", preparedClaimsFilePath, err)
		return nil, fmt.Errorf("failed parsing file %v. Err: %v", preparedClaimsFilePath, err)
	}

	return preparedClaims, nil
}

// writePreparedClaimsToFile serializes PreparedClaims and writes it to a file.
func writePreparedClaimsToFile(preparedClaimsFilePath string, preparedClaims ClaimPreparations) error {
	if preparedClaims == nil {
		preparedClaims = ClaimPreparations{}
	}
	encodedPreparedClaims, err := json.MarshalIndent(preparedClaims, "", "  ")
	if err != nil {
		return fmt.Errorf("failed encoding json. Err: %v", err)
	}
	return os.WriteFile(preparedClaimsFilePath, encodedPreparedClaims, 0600)
}
