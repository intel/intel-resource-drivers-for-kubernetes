/* Copyright (C) 2024 Intel Corporation
 * SPDX-License-Identifier: Apache-2.0
 */

package main

import (
	"context"
	"fmt"
	"os"
	"sync"

	resourceapi "k8s.io/api/resource/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
	"k8s.io/dynamic-resource-allocation/resourceslice"
	"k8s.io/klog/v2"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/qat/cdi"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/qat/device"
)

const (
	driverName                = "qat.intel.com"
	pluginRegistrationDirPath = "/var/lib/kubelet/plugins_registry/"
	kubeletPluginDataDirPath  = "/var/lib/kubelet/plugins/"
	stateFileName             = kubeletPluginDataDirPath + ".state"
)

type driver struct {
	sync.Mutex
	kubeclient KubeClient
	nodename   string
	cdi        *cdi.CDI
	devices    device.QATDevices
	helper     *kubeletplugin.Helper
	statefile  string
}

func (d *driver) PrepareResourceClaims(ctx context.Context, claims []*resourceapi.ResourceClaim) (map[types.UID]kubeletplugin.PrepareResult, error) {

	response := map[types.UID]kubeletplugin.PrepareResult{}

	for _, claim := range claims {
		klog.V(5).Infof("NodePrepareResources: claim %s", claim.UID)
		response[claim.UID] = d.allocateResource(ctx, claim)
	}

	return response, nil
}

func (d *driver) allocateResource(ctx context.Context, claim *resourceapi.ResourceClaim) kubeletplugin.PrepareResult {
	response := kubeletplugin.PrepareResult{}
	deviceConfigurationChanged := false

	d.Lock()
	defer d.Unlock()

	// control device
	controldevicenode, _ := device.GetControlNode()
	controldevicename := cdi.CDIKind + "=" + controldevicenode.UID()

	var allocatedvfs []*device.VFDevice

	for _, deviceallocationresult := range claim.Status.Allocation.Devices.Results {
		var err error
		var vfDevice *device.VFDevice

		if deviceallocationresult.Driver != driverName || deviceallocationresult.Pool != d.nodename {
			klog.V(5).Infof("Driver/pool '%s/%s' not handled by driver (%s/%s)",
				deviceallocationresult.Driver, deviceallocationresult.Pool,
				driverName, d.nodename)

			continue
		}

		requestedDeviceUID := deviceallocationresult.Device

		klog.V(5).Infof("Requested device UID '%s'", requestedDeviceUID)

		// allocate specified QAT VF device which can have any service configured
		vfDevice, deviceConfigurationChanged, err = d.devices.Allocate(requestedDeviceUID, device.Unset, string(claim.UID))
		if err != nil {

			klog.Errorf("Error allocating device %s for %s: %v", requestedDeviceUID, claim.GetUID(), err)

			for _, vf := range allocatedvfs {
				_, _ = d.devices.Free(vf.UID(), string(claim.UID))
			}
			return kubeletplugin.PrepareResult{Err: err}
		}
		allocatedvfs = append(allocatedvfs, vfDevice)

		cdidevicename := cdi.CDIKind + "=" + vfDevice.UID()
		klog.V(5).Infof("Allocated CDI devices '%s' and '%s' for claim '%s'", cdidevicename, controldevicename, claim.GetUID())

		// add device
		response.Devices = append(response.Devices, kubeletplugin.Device{
			Requests:     []string{deviceallocationresult.Request},
			PoolName:     deviceallocationresult.Pool,
			DeviceName:   deviceallocationresult.Device,
			CDIDeviceIDs: []string{cdidevicename, controldevicename},
		})
	}

	// FIXME: deallocate devices if state couldn't be saved for some reason ?
	if err := d.devices.SaveState(d.statefile); err != nil {
		return kubeletplugin.PrepareResult{Err: err}
	}

	// FIXME: deallocate devices if couldn't publish resources ?
	if deviceConfigurationChanged {
		if err := d.UpdateDeviceResources(ctx); err != nil {
			return kubeletplugin.PrepareResult{Err: fmt.Errorf("error publishing resources: %v", err)}
		}
	}

	return response
}

func (d *driver) UnprepareResourceClaims(ctx context.Context, claims []kubeletplugin.NamespacedObject) (map[types.UID]error, error) {

	response := map[types.UID]error{}

	for _, claim := range claims {
		klog.V(5).Infof("NodeUnprepareResources: claim %s", claim.UID)

		response[claim.UID] = d.freeDevice(ctx, claim)
	}

	return response, nil
}

func (d *driver) freeDevice(ctx context.Context, claimDetails kubeletplugin.NamespacedObject) error {
	savestate := false

	claim, err := d.kubeclient.ResourceV1beta1().ResourceClaims(claimDetails.Namespace).Get(ctx, claimDetails.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to find ResourceClaim %s in namespace %s", claimDetails.Name, claimDetails.Namespace)
	}

	if claim.Status.Allocation == nil {
		return fmt.Errorf("ResourceClaim %s is not allocated", claimDetails.Name)
	}

	d.Lock()
	defer d.Unlock()

	for _, deviceallocationresult := range claim.Status.Allocation.Devices.Results {

		requestedDeviceUID := deviceallocationresult.Device

		if updated, err := d.devices.Free(requestedDeviceUID, string(claim.UID)); err != nil {
			klog.Warningf("Could not free device %s claim '%s': %v", requestedDeviceUID, claim.UID, err)
		} else {
			// FIXME: why savestate only once below, but publish resources is inside the loop?
			savestate = true
			klog.V(5).Infof("Claim with uid '%s' freed", claim.GetUID())
			if updated {
				if err := d.UpdateDeviceResources(ctx); err != nil {
					return fmt.Errorf("error publishing resources: %v", err)
				}
			}
		}
	}

	if savestate {
		return d.devices.SaveState(d.statefile)
	}
	return nil
}

func (d *driver) UpdateDeviceResources(ctx context.Context) error {
	resources := resourceslice.DriverResources{
		Pools: map[string]resourceslice.Pool{
			d.nodename: {
				Slices: []resourceslice.Slice{{
					Devices: *deviceResources(device.GetResourceDevices(d.devices)),
				}}}},
	}

	return d.helper.PublishResources(ctx, resources)
}

func newDriver(ctx context.Context) (*driver, error) {
	var (
		clientset  ClientSet
		err        error
		kubeclient KubeClient
	)

	nodename := os.Getenv("NODE_NAME")

	if kubeclient, err = clientset.NewKubeClient(); err != nil {
		return nil, fmt.Errorf("could not create kube client: %v", err)
	}

	cdi, err := cdi.New(cdi.CDIRoot)
	if err != nil {
		return nil, err
	}

	pfdevices, err := device.New()
	if err != nil {
		return nil, fmt.Errorf("could not find PF devices: %v", err)
	}

	for _, pf := range pfdevices {
		if err := pf.EnableVFs(); err != nil {
			return nil, fmt.Errorf("cannot enable PF device '%s': %v", pf.Device, err)
		}
	}

	if err := getDefaultConfiguration(nodename, pfdevices); err != nil {
		klog.Warningf("Cannot apply default configuration: %vn", err)
	}

	detectedcdidevices := device.GetCDIDevices(pfdevices)

	if err := cdi.SyncDevices(detectedcdidevices); err != nil {
		return nil, fmt.Errorf("cannot sync CDI devices: %v", err)
	}

	d := &driver{
		kubeclient: kubeclient,
		nodename:   nodename,
		cdi:        cdi,
		devices:    pfdevices,
		statefile:  stateFileName,
	}

	if err := d.devices.ReadStateOrCreateEmpty(d.statefile); err != nil {
		return nil, fmt.Errorf("could not set up save state file '%s': %v", d.statefile, err)
	}

	return d, nil
}
