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

// UnconfigureAllVfs is taking full path to the driver's DRM VFs dir
// and loops through found VFs to write zeroes into all VFs' attributes.
// Returns true if all operations succeeded, false otherwise.
func UnConfigureAllVFs(vfsDir string, model string) bool {
	filePath := path.Join(vfsDir, "vf*")
	files, _ := filepath.Glob(filePath)
	clean := true

	for _, vfDir := range files {
		attrsDir := path.Join(vfDir, "gt")
		err := UnConfigureVF(attrsDir)
		if err != nil {
			klog.V(5).Info("VF cleanup failed, auto_provisioning will not be enabled")
			clean = false // attempt to cleanup the rest nevertheless
		}
	}

	return clean
}

// PreConfigureVF sets custom VF settings from profile for manual provisioning
// mode for cases when fair share is not suitable. pf/auto_provisioning will be
// automatically set to 0 by KMD in this case.
func PreConfigureVF(vfAttrsDir string, drmVfIndex uint64, vfProfile string, eccOn bool) error {
	for _, attrName := range VfAttributeFiles {
		attrValue := Profiles[vfProfile][attrName]
		if attrName == "lmem_quota" && eccOn {
			attrValue = Profiles[vfProfile]["lmem_quota_ecc_on"]
		}

		vfAttrFile := path.Join(vfAttrsDir, fmt.Sprintf("vf%v/%v/%v", drmVfIndex, GtDirFromProfile(vfProfile), attrName))
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

	return nil
}

// UnConfigureVF unsets configuration of single VF to auto mode, writing 0 to
// all configuration files. It is important to manually set pf/auto_provisioning
// to 1 after all VFs are unconfigured.
func UnConfigureVF(attrsDir string) error {
	for _, vfAttrName := range VfAttributeFiles {
		vfAttrFile := path.Join(attrsDir, vfAttrName)

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
	return nil
}

// CleanupManualConfigurationMaybe checks whether "pf/auto_provisioning" (under given
// sysfsVFsDir) is disabled before enabling it back. If numVfs is zero, it unconfigures
// all available VFs regardless whether they are enabled or configured.
func CleanupManualConfigurationMaybe(sysfsVFsDir string, numVFs uint64, model string) error {
	filename := path.Join(sysfsVFsDir, "pf/auto_provisioning")
	autoProvisioning, err := os.ReadFile(filename)
	if err != nil {
		klog.Warningf("Could not read %v file: %v", filename, err)
	}

	if strings.TrimSpace(string(autoProvisioning)) == "1" {
		klog.V(5).Info("auto_provisioning is enabled, skipping unconfiguration of VFs")
		return nil
	}

	clean := true

	// try cleaning up only requested number of VFs
	for vfIdx := uint64(1); vfIdx <= numVFs; vfIdx++ {
		attrsDir := fmt.Sprintf("%v/vf%d/%v/", sysfsVFsDir, vfIdx, GtDirFromModel(model))
		err = UnConfigureVF(attrsDir)
		if err != nil {
			klog.V(5).Info("VF cleanup failed, auto_provisioning will not be enabled")
			clean = false // attempt to cleanup the rest nevertheless
		}
	}

	// attempt to cleanup all VFs
	if !clean && UnConfigureAllVFs(sysfsVFsDir, model) {
		return fmt.Errorf("PF is dirty, could not enable auto_provisioning")
	}

	// enable auto_provisioning
	fhandle, err := os.OpenFile(filename, os.O_APPEND|os.O_WRONLY, os.ModeAppend)
	if err != nil {
		klog.Errorf("failed to open %v file for writing", filename)
		return fmt.Errorf("failed to open %v file for writing", filename)
	}

	_, err = fhandle.WriteString("1")
	if err != nil {
		klog.Errorf("could not write to file %v", filename)
		return fmt.Errorf("could not write to file %v", filename)
	}

	klog.V(5).Info("Manual VF configuration successfully removed, auto_provisioning enabled")
	return nil
}

// GtDirFromProfile returns gt or gt0 for /sys/class/drm/cardXX/prelim_iov/vfYY/
// depending on the GPU family. Family is determined based on profile prefix.
func GtDirFromProfile(profileName string) string {
	if len(profileName) > 3 && profileName[:3] == "max" {
		return "gt0"
	}
	return "gt"
}

// GtDirFromModel returns gt or gt0 for /sys/class/drm/cardXX/prelim_iov/vfYY/
// depending on the GPU family. Family is determined based on the default VF profile prefix of the given GPU model.
func GtDirFromModel(model string) string {
	return GtDirFromProfile(PerDeviceIdDefaultProfiles[model])
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
