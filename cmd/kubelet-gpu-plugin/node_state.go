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
	"sync"
	"time"

	inf "gopkg.in/inf.v0"
	resourcev1 "k8s.io/api/resource/v1alpha3"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
	"k8s.io/klog/v2"
	drav1 "k8s.io/kubelet/pkg/apis/dra/v1alpha4"
	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"

	cdihelpers "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/cdihelpers"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
)

type ClaimPreparations map[string][]*drav1.Device

type nodeState struct {
	sync.Mutex
	cdiCache               *cdiapi.Cache
	allocatable            device.DevicesInfo
	prepared               ClaimPreparations
	preparedClaimsFilePath string
	nodeName               string
	sysfsRoot              string
}

func newNodeState(detectedDevices map[string]*device.DeviceInfo, cdiRoot string, preparedClaimFilePath string, sysfsRoot string, nodeName string) (*nodeState, error) {
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

	// hack for tests on slow machines
	time.Sleep(250 * time.Millisecond)

	klog.V(5).Info("Allocatable devices after CDI registry refresh:")
	for duid, ddev := range detectedDevices {
		klog.V(5).Infof("CDI device: %v : %+v", duid, ddev)
	}

	preparedClaims, err := getOrCreatePreparedClaims(preparedClaimFilePath)
	if err != nil {
		klog.Errorf("Error getting prepared claims: %v", err)
		return nil, fmt.Errorf("failed to get prepared claims: %v", err)
	}

	klog.V(5).Info("Creating NodeState")
	state := &nodeState{
		cdiCache:               cdiCache,
		allocatable:            detectedDevices,
		prepared:               preparedClaims,
		preparedClaimsFilePath: preparedClaimFilePath,
		sysfsRoot:              sysfsRoot,
		nodeName:               nodeName,
	}

	for duid, ddev := range state.allocatable {
		klog.V(5).Infof("Allocatable device: %v : %+v", duid, ddev)
	}

	return state, nil
}

func (s *nodeState) GetResources() kubeletplugin.Resources {
	devices := []resourcev1.Device{}

	for gpuUID, gpu := range s.allocatable {
		newDevice := resourcev1.Device{
			Name: gpuUID,
			Basic: &resourcev1.BasicDevice{
				Attributes: map[resourcev1.QualifiedName]resourcev1.DeviceAttribute{
					"model": {
						StringValue: &gpu.ModelName,
					},
					"family": {
						StringValue: &gpu.FamilyName,
					},
				},
				Capacity: map[resourcev1.QualifiedName]resource.Quantity{
					"memory":     resource.MustParse(fmt.Sprintf("%vMi", gpu.MemoryMiB)),
					"millicores": *resource.NewDecimalQuantity(*inf.NewDec(int64(1000), inf.Scale(0)), resource.DecimalSI),
				},
			},
		}

		devices = append(devices, newDevice)
	}

	return kubeletplugin.Resources{Devices: devices}
}

func (s *nodeState) Prepare(ctx context.Context, claim *resourcev1.ResourceClaim) error {
	if claim.Status.Allocation == nil {
		return fmt.Errorf("no allocation found in claim %v/%v status", claim.Namespace, claim.Name)
	}

	allocatedDevices := []*drav1.Device{}

	for _, allocatedDevice := range claim.Status.Allocation.Devices.Results {
		// ATM the only pool is cluster node's pool: all devices on current node.
		if allocatedDevice.Driver != device.DriverName || allocatedDevice.Pool != s.nodeName {
			klog.FromContext(ctx).Info("ignoring claim allocation device", "device pool", allocatedDevice.Pool, "device driver", allocatedDevice.Driver,
				"expected pool", s.nodeName, "expected driver", device.DriverName)
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

func (s *nodeState) Unprepare(ctx context.Context, claimUID string) error {
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

// getOrCreatePreparedClaims reads a PreparedClaim from a file and deserializes it or creates the file.
func getOrCreatePreparedClaims(preparedClaimFilePath string) (ClaimPreparations, error) {
	if _, err := os.Stat(preparedClaimFilePath); os.IsNotExist(err) {
		klog.V(5).Infof("could not find file %v. Creating file", preparedClaimFilePath)
		f, err := os.OpenFile(preparedClaimFilePath, os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			return nil, fmt.Errorf("failed creating file %v. Err: %v", preparedClaimFilePath, err)
		}
		defer f.Close()

		if _, err := f.WriteString("{}"); err != nil {
			return nil, fmt.Errorf("failed writing to file %v. Err: %v", preparedClaimFilePath, err)
		}

		klog.V(5).Infof("empty prepared claims file created %v", preparedClaimFilePath)

		return make(ClaimPreparations), nil
	}

	return readPreparedClaimsFromFile(preparedClaimFilePath)
}

// readPreparedClaimToFile returns unmarshaled content for given prepared claims JSON file.
func readPreparedClaimsFromFile(preparedClaimFilePath string) (ClaimPreparations, error) {

	preparedClaims := make(ClaimPreparations)

	preparedClaimsBytes, err := os.ReadFile(preparedClaimFilePath)
	if err != nil {
		klog.V(5).Infof("could not read prepared claims configuration from file %v. Err: %v", preparedClaimFilePath, err)
		return nil, fmt.Errorf("failed reading file %v. Err: %v", preparedClaimFilePath, err)
	}

	if err := json.Unmarshal(preparedClaimsBytes, &preparedClaims); err != nil {
		klog.V(5).Infof("Could not parse default prepared claims configuration from file %v. Err: %v", preparedClaimFilePath, err)
		return nil, fmt.Errorf("failed parsing file %v. Err: %v", preparedClaimFilePath, err)
	}

	return preparedClaims, nil
}

// writePreparedClaimsToFile serializes PreparedClaims and writes it to a file.
func writePreparedClaimsToFile(preparedClaimFilePath string, preparedClaims ClaimPreparations) error {
	if preparedClaims == nil {
		preparedClaims = ClaimPreparations{}
	}
	encodedPreparedClaims, err := json.MarshalIndent(preparedClaims, "", "  ")
	if err != nil {
		return fmt.Errorf("prepared claims JSON encoding failed. Err: %v", err)
	}
	return os.WriteFile(preparedClaimFilePath, encodedPreparedClaims, 0600)
}
