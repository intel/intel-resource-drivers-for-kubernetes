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
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
	"k8s.io/klog/v2"
	drav1 "k8s.io/kubelet/pkg/apis/dra/v1beta1"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/qat/cdi"
	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/qat/device"
)

const (
	driverName             = "qat.intel.com"
	pluginRegistrationPath = "/var/lib/kubelet/plugins_registry/" + driverName + ".sock"
	driverPluginPath       = "/var/lib/kubelet/plugins/" + driverName
	driverPluginSocketPath = driverPluginPath + "/plugin.sock"
	stateFileName          = driverPluginPath + ".state"
)

var _ drav1.DRAPluginServer = &driver{}

type driver struct {
	sync.Mutex
	kubeclient KubeClient
	nodename   string
	cdi        *cdi.CDI
	devices    device.QATDevices
	plugin     kubeletplugin.DRAPlugin
	statefile  string
}

func (d *driver) getResourceClaim(ctx context.Context, claim *drav1.Claim) (*resourceapi.ResourceClaim, error) {
	resourceclaim, err := d.kubeclient.ResourceV1beta1().ResourceClaims(claim.Namespace).Get(ctx, claim.Name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to find ResourceClaim %s in namespace %s", claim.Name, claim.Namespace)
	}

	if resourceclaim.Status.Allocation == nil {
		return nil, fmt.Errorf("ResourceClaim %s not yet allocated", claim.Name)
	}

	return resourceclaim, nil
}

func (d *driver) NodePrepareResources(ctx context.Context, req *drav1.NodePrepareResourcesRequest) (*drav1.NodePrepareResourcesResponse, error) {

	preparedResourcesResponse := &drav1.NodePrepareResourcesResponse{
		Claims: map[string]*drav1.NodePrepareResourceResponse{},
	}

	for _, claim := range req.Claims {
		klog.V(5).Infof("NodePrepareResources: claim %s", claim.GetUID())
		preparedResourcesResponse.Claims[claim.GetUID()] = d.allocateResource(ctx, claim)
	}

	return preparedResourcesResponse, nil
}

func (d *driver) allocateResource(ctx context.Context, claim *drav1.Claim) *drav1.NodePrepareResourceResponse {
	resourceclaim, err := d.getResourceClaim(ctx, claim)
	if err != nil {
		klog.Errorf("Error fetching ResourceClaim for %s: %v", claim.GetUID(), err)
		return &drav1.NodePrepareResourceResponse{
			Error: err.Error(),
		}
	}

	response := &drav1.NodePrepareResourceResponse{}
	deviceConfigurationChanged := false

	d.Lock()
	defer d.Unlock()

	// control device
	controldevicenode, _ := device.GetControlNode()
	controldevicename := cdi.CDIKind + "=" + controldevicenode.UID()

	var allocatedvfs []*device.VFDevice

	for _, deviceallocationresult := range resourceclaim.Status.Allocation.Devices.Results {
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
		vfDevice, deviceConfigurationChanged, err = d.devices.Allocate(requestedDeviceUID, device.Unset, claim.GetUID())
		if err != nil {

			klog.Errorf("Error allocating device %s for %s: %v", requestedDeviceUID, claim.GetUID(), err)

			for _, vf := range allocatedvfs {
				_, _ = d.devices.Free(vf.UID(), claim.GetUID())
			}
			return &drav1.NodePrepareResourceResponse{
				Error: err.Error(),
			}
		}
		allocatedvfs = append(allocatedvfs, vfDevice)

		cdidevicename := cdi.CDIKind + "=" + vfDevice.UID()
		klog.V(5).Infof("Allocated CDI devices '%s' and '%s' for claim '%s'", cdidevicename, controldevicename, claim.GetUID())

		// add device
		response.Devices = append(response.Devices, &drav1.Device{
			RequestNames: []string{deviceallocationresult.Request},
			PoolName:     deviceallocationresult.Pool,
			DeviceName:   deviceallocationresult.Device,
			CDIDeviceIDs: []string{cdidevicename, controldevicename},
		})
	}

	// FIXME: deallocate devices if state couldn't be saved for some reason ?
	if err := d.devices.SaveState(d.statefile); err != nil {
		return &drav1.NodePrepareResourceResponse{
			Error: err.Error(),
		}
	}

	// FIXME: deallocate devices if couldn't publish resources ?
	if deviceConfigurationChanged {
		if err := d.UpdateDeviceResources(ctx); err != nil {
			return &drav1.NodePrepareResourceResponse{
				Error: fmt.Sprintf("error publishing resources: %v", err),
			}
		}
	}

	return response
}

func (d *driver) NodeUnprepareResources(ctx context.Context, req *drav1.NodeUnprepareResourcesRequest) (*drav1.NodeUnprepareResourcesResponse, error) {

	unpreparedResourcesResponse := &drav1.NodeUnprepareResourcesResponse{
		Claims: map[string]*drav1.NodeUnprepareResourceResponse{},
	}

	for _, claim := range req.Claims {
		klog.V(5).Infof("NodeUnprepareResources: claim %s", claim.GetUID())

		unpreparedResourcesResponse.Claims[claim.GetUID()] = d.freeDevice(ctx, claim)
	}

	return unpreparedResourcesResponse, nil
}

func (d *driver) freeDevice(ctx context.Context, claim *drav1.Claim) *drav1.NodeUnprepareResourceResponse {
	savestate := false

	resourceclaim, err := d.getResourceClaim(ctx, claim)
	if err != nil {
		klog.Errorf("Error fetching ResourceClaim for %s: %v", claim.GetUID(), err)
		return &drav1.NodeUnprepareResourceResponse{
			Error: err.Error(),
		}
	}

	d.Lock()
	defer d.Unlock()

	for _, deviceallocationresult := range resourceclaim.Status.Allocation.Devices.Results {

		requestedDeviceUID := deviceallocationresult.Device

		if updated, err := d.devices.Free(requestedDeviceUID, claim.GetUID()); err != nil {
			klog.Warningf("Could not free device %s claim '%s': %v", requestedDeviceUID, claim.GetUID(), err)
		} else {
			// FIXME: why savestate only once below, but publish resources is inside the loop?
			savestate = true
			klog.V(5).Infof("Claim with uid '%s' freed", claim.GetUID())
			if updated {
				if err := d.UpdateDeviceResources(ctx); err != nil {
					return &drav1.NodeUnprepareResourceResponse{
						Error: fmt.Sprintf("error publishing resources: %v", err),
					}
				}
			}
		}
	}

	if savestate {
		if err := d.devices.SaveState(d.statefile); err != nil {
			return &drav1.NodeUnprepareResourceResponse{
				Error: err.Error(),
			}
		}
	}
	return &drav1.NodeUnprepareResourceResponse{}
}

func (d *driver) UpdateDeviceResources(ctx context.Context) error {
	if d.plugin == nil {
		return nil
	}

	resources := kubeletplugin.Resources{
		Devices: *deviceResources(device.GetResourceDevices(d.devices)),
	}

	return d.plugin.PublishResources(ctx, resources)
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
