/*
 * Copyright (c) 2025, Intel Corporation.  All Rights Reserved.
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

package helpers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"k8s.io/dynamic-resource-allocation/kubeletplugin"
	"k8s.io/klog/v2"
	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"
)

type ClaimPreparations map[string]kubeletplugin.PrepareResult

type NodeState struct {
	sync.Mutex
	CdiCache               *cdiapi.Cache
	Allocatable            interface{}
	Prepared               ClaimPreparations
	PreparedClaimsFilePath string
	NodeName               string
	SysfsRoot              string
}

func (s *NodeState) Unprepare(ctx context.Context, claimUID string) error {
	s.Lock()
	defer s.Unlock()

	if _, found := s.Prepared[claimUID]; !found {
		return nil
	}

	klog.V(5).Infof("Freeing devices from claim %v", claimUID)
	delete(s.Prepared, claimUID)

	// write prepared claims to file
	if err := WritePreparedClaimsToFile(s.PreparedClaimsFilePath, s.Prepared); err != nil {
		return fmt.Errorf("failed to write prepared claims to file: %v", err)
	}

	return nil
}

// GetOrCreatePreparedClaims reads a PreparedClaim from a file and deserializes it or creates the file.
func GetOrCreatePreparedClaims(preparedClaimFilePath string) (ClaimPreparations, error) {
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

	return ReadPreparedClaimsFromFile(preparedClaimFilePath)
}

// ReadPreparedClaimToFile returns unmarshaled content for given prepared claims JSON file.
func ReadPreparedClaimsFromFile(preparedClaimFilePath string) (ClaimPreparations, error) {

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

// WritePreparedClaimsToFile serializes PreparedClaims and writes it to a file.
func WritePreparedClaimsToFile(preparedClaimFilePath string, preparedClaims ClaimPreparations) error {
	if preparedClaims == nil {
		preparedClaims = ClaimPreparations{}
	}
	encodedPreparedClaims, err := json.MarshalIndent(preparedClaims, "", "  ")
	if err != nil {
		return fmt.Errorf("prepared claims JSON encoding failed. Err: %v", err)
	}
	return os.WriteFile(preparedClaimFilePath, encodedPreparedClaims, 0600)
}
