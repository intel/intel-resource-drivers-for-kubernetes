/* Copyright (C) 2024 Intel Corporation
 * SPDX-License-Identifier: Apache-2.0
 */

package device

import (
	"encoding/json"
	"fmt"
	"os"

	"k8s.io/klog/v2"
)

// Map allocation id to VF device.
type savedAllocations map[string][]string

func (q *QATDevices) ReadStateOrCreateEmpty(statefile string) error {
	if statefile == "" {
		return nil
	}

	if _, err := os.Stat(statefile); os.IsNotExist(err) {
		f, err := os.OpenFile(statefile, os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			return fmt.Errorf("failed to create state file '%s': %v", statefile, err)
		}
		defer f.Close()

		if _, err := f.WriteString("{}"); err != nil {
			return fmt.Errorf("failed to write to state file '%s': %v", statefile, err)
		}

		return nil
	}

	return q.readState(statefile)
}

func (q *QATDevices) readState(statefile string) error {
	if statefile == "" {
		return nil
	}

	savedstatebytes, err := os.ReadFile(statefile)
	if err != nil {
		return fmt.Errorf("could not read state file '%s': %v", statefile, err)
	}

	saveddevices := make(savedAllocations, 0)
	if err := json.Unmarshal(savedstatebytes, &saveddevices); err != nil {
		return fmt.Errorf("failed parsing state file '%s': %v", statefile, err)
	}

	for allocatedby, vfdevices := range saveddevices {
		for _, vf := range vfdevices {
			_, _, err := q.Allocate(vf, Unset, allocatedby)

			if err != nil {
				klog.Errorf("Failed to restore VF device '%s' for '%s': %v", vf, allocatedby, err)
				continue
			}

			klog.V(5).Infof("Successfully restored VF device '%s' for '%s'", vf, allocatedby)
		}
	}

	return nil
}

func (q *QATDevices) SaveState(statefile string) error {
	if statefile == "" {
		return nil
	}

	saveddevices := make(savedAllocations, 0)

	for _, pf := range *q {
		for allocatedby, vfdevices := range pf.AllocatedDevices {
			vflist, exists := saveddevices[allocatedby]
			if !exists {
				vflist = make([]string, 0)
			}

			for deviceuid := range vfdevices {
				vflist = append(vflist, deviceuid)
			}
			saveddevices[allocatedby] = vflist
		}
	}

	encodedstate, err := json.MarshalIndent(saveddevices, "", "  ")
	if err != nil {
		return fmt.Errorf("failed save state JSON encoding to file '%s': %v", statefile, err)
	}

	return os.WriteFile(statefile, encodedstate, 0600)
}
