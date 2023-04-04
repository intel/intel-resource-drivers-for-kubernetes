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
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	sriovProfiles "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/sriov"
	"k8s.io/klog/v2"
)

// Loop through VFs of DRM card device and write zeroes into all VFs' attributes.
// Return true if all operations succeeded, false otherwise.
func unConfigureAllVFs(cardIdx int) bool {
	filePath := fmt.Sprintf("%v/card%d/prelim_iov/vf*", sysfsDRMDir, cardIdx)
	files, _ := filepath.Glob(filePath)
	clean := true

	for _, vfdir := range files {
		attrsDir := path.Join(vfdir, "gt")
		err := sriovProfiles.UnConfigureVF(attrsDir)
		if err != nil {
			klog.V(5).Infof("VF cleanup failed, auto_provisioning will not be enabled")
			clean = false // attempt to cleanup the rest nevertheless
		}
	}

	return clean
}

// If drm/cardX/vfio/pf/auto_provisioning is disabled - cleanup VFs' configuration and enable it.
// if numVfs is zero, unconfigure all available VFs regardless if they are enabled / configured.
func cleanupManualConfigurationMaybe(pciDBDF string, cardIdx int, numVfs int) error {
	filename := path.Join(sysfsDRMDir, fmt.Sprintf("card%d", cardIdx), "prelim_iov/pf/auto_provisioning")
	autoProvisioning, err := os.ReadFile(filename)
	if err != nil {
		klog.Warningf("Could not read %v file: %v", filename, err)
	}

	if strings.TrimSpace(string(autoProvisioning)) == "1" {
		klog.V(5).Infof("auto_provisioning is enabled for device %v, skipping unconfiguration of VFs", pciDBDF)
		return nil
	}

	clean := true
	// try cleaning up only requested number of VFs
	for vfIdx := 1; vfIdx <= numVfs; vfIdx++ {
		attrsDir := fmt.Sprintf("%v/card%d/prelim_iov/vf%d/gt/", sysfsDRMDir, cardIdx, vfIdx)
		err = sriovProfiles.UnConfigureVF(attrsDir)
		if err != nil {
			klog.V(5).Infof("VF cleanup failed, auto_provisioning will not be enabled")
			clean = false // attempt to cleanup the rest nevertheless
		}
	}

	// attempt to cleanup all VFs
	if !clean && !unConfigureAllVFs(cardIdx) {
		return fmt.Errorf("PF is dirty, could not enable auto_provisioning")
	}

	// enable auto_provisioning
	fhandle, err := os.OpenFile(filename, os.O_APPEND|os.O_WRONLY, os.ModeAppend)
	if err != nil {
		klog.Errorf("Failed to open %v file for writing", filename)
		return fmt.Errorf("Failed to open %v file for writing", filename)
	}

	_, err = fhandle.WriteString("1")
	if err != nil {
		klog.Error("Could not write to file %v", filename)
		return fmt.Errorf("Could not write to file %v", filename)
	}

	klog.V(5).Infof("Manual VF configuration successfully removed, auto_provisioning enabled")
	return nil
}

// Set custom VF settings from profile for manual provisioning mode, in case fair share is not suitable.
// pf/auto_provisioning will be automatically set to 0 in this case.
func preConfigureVFs(cardIdx int, vfs map[string]*DeviceInfo) error {
	vfAttrsPath := path.Join(sysfsDRMDir, fmt.Sprintf("card%d/prelim_iov", cardIdx))
	for _, vf := range vfs {
		if err := sriovProfiles.PreConfigureVF(vfAttrsPath, vf.drmVFIndex(), vf.vfprofile); err != nil {
			return fmt.Errorf("Failed preconfiguring vf %v: %v", vf.uid, err)
		}
	}

	return nil
}
