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
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	resourcev1 "k8s.io/api/resource/v1alpha2"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"

	cdihelpers "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gaudi/cdihelpers"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gaudi/device"
	intelcrd "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gaudi/v1alpha1/api"
	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"
)

type ClaimPreparations map[string][]*device.DeviceInfo

type nodeState struct {
	sync.Mutex
	cdiCache               *cdiapi.Cache
	allocatable            device.DevicesInfo
	prepared               ClaimPreparations
	preparedClaimsFilePath string
}

func newNodeState(gas *intelcrd.GaudiAllocationState, detectedDevices map[string]*device.DeviceInfo, cdiRoot string, preparedClaimsFilePath string) (*nodeState, error) {
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

	klog.V(5).Info("Creating NodeState")
	// TODO: allocatable should include cdi-described
	state := &nodeState{
		cdiCache:               cdiCache,
		allocatable:            detectedDevices,
		prepared:               make(ClaimPreparations),
		preparedClaimsFilePath: preparedClaimsFilePath,
	}

	preparedClaims, err := getOrCreatePreparedClaims(preparedClaimsFilePath)
	if err != nil {
		klog.Errorf("Error getting prepared claims: %v", err)
		return nil, fmt.Errorf("failed to get prepared claims: %v", err)
	}

	klog.V(5).Info("Syncing allocatable devices")
	err = state.syncPreparedDevicesFromFile(preparedClaims)
	if err != nil {
		return nil, fmt.Errorf("unable to sync allocated devices from GaudiAllocationState: %v", err)
	}
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

	return nil
}

func (s *nodeState) GetUpdatedSpec(inspec *intelcrd.GaudiAllocationStateSpec) *intelcrd.GaudiAllocationStateSpec {
	s.Lock()
	defer s.Unlock()

	outspec := inspec.DeepCopy()
	s.syncAllocatableDevicesToGASSpec(outspec)
	return outspec
}

func (s *nodeState) GetAllocatedCDINames(claimUID string) []string {
	devs := []string{}
	klog.V(5).Info("getAllocatedCDINames is called")

	for _, device := range s.prepared[claimUID] {
		cdidev := s.cdiCache.GetDevice(device.CDIName())
		if cdidev == nil {
			klog.Errorf("CDI Device %v from claim %v not found in CDI DB", device.CDIName(), claimUID)
			return []string{}
		}
		klog.V(5).Infof("Found CDI device %v", cdidev.GetQualifiedName())
		devs = append(devs, cdidev.GetQualifiedName())
	}
	return devs
}

func (s *nodeState) getMonitorCDINames(claimUID string) []string {
	klog.V(5).Info("getMonitorCDINames is called")

	klog.V(5).Info("Refreshing CDI registry")
	err := s.cdiCache.Refresh()
	if err != nil {
		klog.Errorf("Unable to refresh the CDI registry: %v", err)
		return []string{}
	}

	devs := []string{}
	for _, device := range s.allocatable {
		cdidev := s.cdiCache.GetDevice(device.CDIName())
		if cdidev == nil {
			klog.Errorf("CDI Device %v for monitor claim %v not found in CDI DB", device.CDIName(), claimUID)
			return []string{}
		}
		klog.V(5).Infof("Found CDI device %v", cdidev.GetQualifiedName())
		devs = append(devs, cdidev.GetQualifiedName())
	}
	return devs
}

func (s *nodeState) syncAllocatableDevicesToGASSpec(spec *intelcrd.GaudiAllocationStateSpec) {
	devices := make(map[string]intelcrd.AllocatableDevice)
	for _, device := range s.allocatable {
		devices[device.UID] = intelcrd.AllocatableDevice{
			Model: device.Model,
			UID:   device.UID,
		}
	}

	spec.AllocatableDevices = devices
}

// On startup read what was previously prepared where we left off.
func (s *nodeState) syncPreparedDevicesFromFile(preparedClaims map[string][]*device.DeviceInfo) error {
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

func (s *nodeState) makePreparedClaimAllocation(claimUID string, claimDevices []*device.DeviceInfo) error {
	s.prepared[claimUID] = claimDevices
	err := writePreparedClaimsToFile(s.preparedClaimsFilePath, s.prepared)
	if err != nil {
		klog.Errorf("Error writing prepared claims to file: %v", err)
		return fmt.Errorf("failed to write prepared claims to file: %v", err)
	}

	klog.V(5).Infof("Created prepared claim %v allocation", claimUID)
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

func (s *nodeState) getResourceModel() resourcev1.ResourceModel {
	var devices []resourcev1.NamedResourcesInstance

	for _, device := range s.allocatable {
		instance := resourcev1.NamedResourcesInstance{
			Name: strings.ToLower(device.UID),
			Attributes: []resourcev1.NamedResourcesAttribute{
				{
					Name: "uid",
					NamedResourcesAttributeValue: resourcev1.NamedResourcesAttributeValue{
						StringValue: &device.UID,
					},
				},
				{
					Name: "model",
					NamedResourcesAttributeValue: resourcev1.NamedResourcesAttributeValue{
						StringValue: ptr.To(device.ModelName()),
					},
				},
			},
		}
		devices = append(devices, instance)
	}

	model := resourcev1.ResourceModel{
		NamedResources: &resourcev1.NamedResourcesResources{Instances: devices},
	}

	return model
}
