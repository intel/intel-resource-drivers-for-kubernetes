/*
 * Copyright (c) 2026, Intel Corporation.  All Rights Reserved.
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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
	"k8s.io/klog/v2"
)

const (
	CheckpointAPIGroup   = "checkpoint.gpu.intel.com"
	CheckpointKind       = "PreparedClaimsCheckpoint"
	CheckpointAPIVersion = CheckpointAPIGroup + "/v1"
)

// This struct is only used to write and read the checkpoint file
// TODO: add checksum and use it.
type PreparedClaimsCheckpoint struct {
	metav1.TypeMeta
	PreparedClaims ClaimPreparations
}

type ClaimPreparations map[types.UID]ClaimPreparation
type ClaimPreparation struct {
	PreparedDevices []PreparedDevice
}

type PreparedDevices []PreparedDevice
type PreparedDevice struct {
	AdminAccess         bool
	KubeletpluginDevice kubeletplugin.Device
}

func (cp ClaimPreparation) PrepareResult() kubeletplugin.PrepareResult {
	result := kubeletplugin.PrepareResult{}

	for _, device := range cp.PreparedDevices {
		result.Devices = append(result.Devices, device.KubeletpluginDevice)
	}

	return result
}

func UnmarshalClaimPreparations(data []byte) (ClaimPreparations, error) {
	var err error
	cp := PreparedClaimsCheckpoint{PreparedClaims: ClaimPreparations{}}
	// unmarshalling might lose unknown fields, test if  TypeMeta version was filled.
	// If it wasn't -> it's the oldest map[string]kubeletplugin.PrepareResult format.
	if err = json.Unmarshal(data, &cp); err != nil {
		klog.Errorf("Failed to unmarshal prepared claims file as latest generation. Err: %v", err)
	} else if cp.Kind == CheckpointKind { // check if TypeMeta was filled.
		// TODO: handle newer versions when there are any.
		// For now, it's enough that TypeMeta was present -> new generation.
		return cp.PreparedClaims, nil
	} // else - attempt to parse the oldest format below.

	klog.V(5).Info("Falling back to parsing prepared claims file as unversioned.")

	var oldPreparedClaims map[string]kubeletplugin.PrepareResult
	if err = json.Unmarshal(data, &oldPreparedClaims); err != nil {
		return ClaimPreparations{}, fmt.Errorf("failed to unmarshal prepared claims file as unversioned: %v", err)
	}

	for claimUIDstring, prepareResult := range oldPreparedClaims {
		claimUID := types.UID(claimUIDstring)
		preparedDevices := []PreparedDevice{}
		for _, device := range prepareResult.Devices {
			preparedDevices = append(preparedDevices, PreparedDevice{
				KubeletpluginDevice: device,
			})
		}
		cp.PreparedClaims[claimUID] = ClaimPreparation{PreparedDevices: preparedDevices}
	}

	return cp.PreparedClaims, nil
}

// GetOrCreatePreparedClaims reads a PreparedClaim from a file and deserializes it or creates the file.
func GetOrCreatePreparedClaims(preparedClaimFilePath string) (ClaimPreparations, error) {
	if _, err := os.Stat(preparedClaimFilePath); os.IsNotExist(err) {
		klog.V(5).Infof("could not find file %v. Creating file", preparedClaimFilePath)
		return ClaimPreparations{}, WritePreparedClaimsToFile(preparedClaimFilePath, ClaimPreparations{})
	}

	return readPreparedClaimsFromFile(preparedClaimFilePath)
}

// readPreparedClaimsFromFile returns unmarshaled content for given prepared claims JSON file.
func readPreparedClaimsFromFile(preparedClaimFilePath string) (ClaimPreparations, error) {
	var err error
	var cp ClaimPreparations

	preparedClaimsBytes, err := os.ReadFile(preparedClaimFilePath)
	if err != nil {
		klog.V(5).Infof("could not read prepared claims checkpoint from file %v. Err: %v", preparedClaimFilePath, err)
		return nil, fmt.Errorf("failed reading file %v. Err: %v", preparedClaimFilePath, err)
	}

	if cp, err = UnmarshalClaimPreparations(preparedClaimsBytes); err != nil {
		klog.V(5).Infof("Could not parse prepared claims checkpoint from file %v. Err: %v", preparedClaimFilePath, err)
		return nil, fmt.Errorf("failed parsing file %v. Err: %v", preparedClaimFilePath, err)
	}

	return cp, nil
}

// WritePreparedClaimsToFile wraps PreparedClaims into versioned struct, serializes it
// and writes it to a file.
func WritePreparedClaimsToFile(preparedClaimFilePath string, preparedClaims ClaimPreparations) error {
	if preparedClaims == nil {
		preparedClaims = ClaimPreparations{}
	}
	newCheckpoint := PreparedClaimsCheckpoint{
		TypeMeta: metav1.TypeMeta{
			Kind:       CheckpointKind,
			APIVersion: CheckpointAPIVersion,
		},
		PreparedClaims: preparedClaims,
	}

	encodedPreparedClaims, err := json.Marshal(newCheckpoint)
	if err != nil {
		return fmt.Errorf("prepared claims JSON encoding failed. Err: %v", err)
	}
	return os.WriteFile(preparedClaimFilePath, encodedPreparedClaims, 0600)
}
