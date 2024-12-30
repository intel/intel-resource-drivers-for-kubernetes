/* Copyright (C) 2024 Intel Corporation
 * SPDX-License-Identifier: Apache-2.0
 */

package main

import (
	"encoding/json"
	"fmt"
	"os"

	"k8s.io/klog/v2"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/qat/device"
)

const defaultConfigFile = "/defaults/qatdefaults.config"

func readConfigFile(hostname string) (map[string]string, error) {
	configBytes, err := os.ReadFile(defaultConfigFile)
	if err != nil {
		return nil, err
	}

	var configFile map[string]map[string]string
	if err := json.Unmarshal(configBytes, &configFile); err != nil {
		return nil, err
	}

	hostConfig, exists := configFile[hostname]
	if !exists {
		return nil, fmt.Errorf("no config for host '%s' found", hostname)
	}

	return hostConfig, nil
}

func getDefaultConfiguration(hostname string, q device.QATDevices) error {
	serviceconfig, err := readConfigFile(hostname)
	if err != nil {
		klog.Infof("Could not read default config file - leaving unconfigured: %v", err)
		return nil
	}

	klog.V(5).Infof("Default config for host '%s':", hostname)
	for _, pf := range q {
		if servicestr, exists := serviceconfig[pf.Device]; exists {
			var services device.Services
			var err error

			if services, err = device.StringToServices(servicestr); err != nil {
				klog.Warningf("Error parsing default config services for PF device '%s': %v", pf.Device, err)
				continue
			}

			if err := pf.SetServices([]device.Services{services}); err != nil {
				klog.Warningf("Error configuring services '%s' for PF device '%s': %v", services.String(), pf.Device, err)
				continue
			}

			klog.V(5).Infof("PF device '%s' configured with services %s'", pf.Device, services.String())
		}
	}

	return nil
}
