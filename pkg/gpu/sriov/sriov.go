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

package sriov

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"
)

// PreConfigureVF sets custom VF settings from profile for manual provisioning
// mode for cases when fair share is not suitable. pf/auto_provisioning will be
// automatically set to 0 by KMD in this case.
func PreConfigureVF(sysfsVFsDir string, drmVFIndex uint64, vfProfile string, eccOn bool) error {

	vfDir := path.Join(sysfsVFsDir, fmt.Sprintf("vf%v/", drmVFIndex))
	vfTilePaths := getGTdirs(vfDir)
	if len(vfTilePaths) == 0 {
		return fmt.Errorf("could not find VF %v tiles in %v", drmVFIndex, sysfsVFsDir)
	}

	for _, attrName := range VfAttributeFiles {
		attrValue := Profiles[vfProfile][attrName]
		if attrName == "lmem_quota" && eccOn {
			attrValue = Profiles[vfProfile]["lmem_quota_ecc_on"]
		}

		// these values need to be split evenly between tiles
		if attrName == "doorbells_quota" || attrName == "ggtt_quota" || attrName == "lmem_quote" {
			attrValue /= uint64(len(vfTilePaths))
		}

		// for now VFs on multi-tile devices are provisioned on all tiles
		for _, vfTilePath := range vfTilePaths {
			vfAttrFile := path.Join(vfTilePath, attrName)
			klog.V(3).Infof("setting %v", attrName)
			fhandle, err := os.OpenFile(vfAttrFile, os.O_APPEND|os.O_WRONLY, os.ModeAppend)
			if err != nil {
				klog.Errorf("failed to open %v file: %v", vfAttrFile, err)
				return fmt.Errorf("failed to open %v file: %v", vfAttrFile, err)
			}

			_, err = fhandle.WriteString(fmt.Sprint(attrValue))
			if err != nil {
				klog.Errorf("could not write to file %v: %v", vfAttrFile, err)
				return fmt.Errorf("could not write to file %v: %v", vfAttrFile, err)
			}
			time.Sleep(250 * time.Millisecond)
		}
	}
	return nil
}

// CleanupManualConfigurationMaybe checks if "pf/auto_provisioning" (under given sysfsVFsDir)
// is disabled before unconfiguring requested VFs. If VFs were successfully unconfigured,
// auto_provisioning is enabled.
func CleanupManualConfigurationMaybe(sysfsVFsDir string, numVFs uint64) error {
	autoProvisioningFile := path.Join(sysfsVFsDir, "pf/auto_provisioning")
	autoProvisioning, err := os.ReadFile(autoProvisioningFile)
	if err != nil {
		klog.Warningf("Could not read %v file: %v", autoProvisioningFile, err)
	}

	if strings.TrimSpace(string(autoProvisioning)) == "1" {
		klog.V(5).Info("auto_provisioning is enabled, skipping unconfiguration of VFs")
		return nil
	}

	if !unConfigureVFs(sysfsVFsDir, numVFs) {
		// if unconfiguration fails, there is nothing we can do
		return fmt.Errorf("could not unconfigure requested VFs. Not enabling auto_provisioning")
	}

	// enable auto_provisioning
	fhandle, err := os.OpenFile(autoProvisioningFile, os.O_APPEND|os.O_WRONLY, os.ModeAppend)
	if err != nil {
		return fmt.Errorf("failed to open %v file for writing", autoProvisioningFile)
	}

	if _, err := fhandle.WriteString("1"); err != nil {
		klog.V(3).Info("failed to enable auto_provisioning, trying to unconfigure all possible VFs")
		if !unConfigureVFs(sysfsVFsDir, 0) {
			return fmt.Errorf("could not unconfigure some VFs. Not enabling auto_provisioning")
		}
		// try enabling auto_provisioning again
		if _, err := fhandle.WriteString("1"); err != nil {
			return fmt.Errorf("could not enable auto_provisioning, all VFs successfully unconfigured")
		}
	}

	klog.V(5).Info("Manual VF configuration successfully removed, auto_provisioning enabled")
	return nil
}

// unconfigureVFs is taking full path to the driver's DRM VFs dir and loops
// through requested number of VFs. If the requested numVFs is zero, it loops
// through all found VFs to call UnConfigureVF which writes zeroes into all VF's
// attributes on all tiles.
// Returns true if all operations succeeded, false otherwise.
func unConfigureVFs(vfsDir string, numVFs uint64) bool {
	clean := true

	if numVFs == 0 {
		filePath := path.Join(vfsDir, "vf*")
		files, _ := filepath.Glob(filePath)

		for _, vfDir := range files {
			if err := unConfigureVF(vfDir); err != nil {
				klog.V(5).Infof("VF cleanup failed, auto_provisioning will not be enabled. Err: %v", err)
				clean = false // attempt to cleanup the rest nevertheless
			}
		}
	} else {
		// try cleaning up only requested number of VFs
		for drmVFIndex := uint64(1); drmVFIndex <= numVFs; drmVFIndex++ {
			vfIOVPath := fmt.Sprintf("%v/vf%d/", vfsDir, drmVFIndex)
			if err := unConfigureVF(vfIOVPath); err != nil {
				klog.V(3).Infof("VF %v cleanup failed, auto_provisioning will not be enabled", vfIOVPath)
				clean = false
				// attempt to cleanup the rest nevertheless
			}
		}
	}
	return clean
}

// unConfigureVF unsets configuration of single VF to auto mode, writing 0 to
// all configuration files on all tiles. It is important to manually set
// pf/auto_provisioning to 1 after all VFs are unconfigured.
func unConfigureVF(vfDir string) error {
	vfTilePaths := getGTdirs(vfDir)
	if len(vfTilePaths) == 0 {
		return fmt.Errorf("could not find VF tiles in %v", vfDir)
	}

	for _, vfTilePath := range vfTilePaths {
		for _, vfAttrName := range VfAttributeFiles {
			vfAttrFile := path.Join(vfTilePath, vfAttrName)

			klog.V(3).Infof("Resetting VF file %v", vfAttrFile)
			fhandle, err := os.OpenFile(vfAttrFile, os.O_APPEND|os.O_WRONLY, os.ModeAppend)
			if err != nil {
				if errors.IsNotFound(err) {
					klog.V(5).Infof("Ignoring missing VF attribute file %v", vfAttrName)
					continue
				}
				klog.Errorf("failed to open %v file: %v", vfAttrFile, err)
				return fmt.Errorf("failed to open %v file: %v", vfAttrFile, err)
			}

			_, err = fhandle.WriteString("0")
			if err != nil {
				klog.Errorf("could not write to file %v: %v", vfAttrFile, err)
				return fmt.Errorf("could not write to file %v: %v", vfAttrFile, err)
			}

			if err = fhandle.Close(); err != nil {
				klog.Errorf("could not close file %v: %v", vfAttrFile, err)
				// Do not fail here, main job is done by now.
			}
			time.Sleep(250 * time.Millisecond)
		}
	}

	return nil
}

// getGTdirs returns directories named gt* in vfTilesDir.
func getGTdirs(vfDir string) []string {
	filePath := filepath.Join(vfDir, "gt*")
	gts, err := filepath.Glob(filePath)
	if err != nil {
		klog.V(5).Infof("could not find gt* dirs in %v. Err: %v", filePath, err)
		return []string{}
	}

	return gts
}

// DeduceVFMillicores returns relative amount of millicores based on either of:
// - number of provisioned VFs, if auto_provisioning is enabled;
// - memory allocated to the VF, if auto_provisioning is disabled.
func DeduceVFMillicores(parentI915Dir string, cardIdx uint64, pciVFIndex uint64, vfMemMiB uint64, deviceID string) (uint64, error) {
	parentCardDir := path.Join(parentI915Dir, "drm", fmt.Sprintf("card%d", cardIdx))
	filename := path.Join(parentCardDir, "prelim_iov", "pf/auto_provisioning")
	autoProvisioning, err := os.ReadFile(filename)
	if err != nil {

		return 0, fmt.Errorf("could not read %v file: %v", filename, err)
	}

	if strings.TrimSpace(string(autoProvisioning)) == "1" {
		// auto provisioning in place, need to check how many VFs are enabled
		filePath := filepath.Join(parentI915Dir, "virtfn*")
		files, _ := filepath.Glob(filePath)
		millicores := uint64(1000 / len(files))

		return millicores, nil
	}

	// trust that VFs are configured according to profiles, find most suitable profile
	_, millicores, _, err := PickVFProfile(deviceID, vfMemMiB, 0, true)
	if err != nil {
		return 0, fmt.Errorf("picking profile based on memory: %v", err)
	}

	return millicores, nil
}
