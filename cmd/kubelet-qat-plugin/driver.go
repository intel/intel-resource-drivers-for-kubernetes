/* Copyright (C) 2024 Intel Corporation
 * SPDX-License-Identifier: Apache-2.0
 */

package main

import (
	"context"
	"fmt"
	"os"
	"sync"

	resourceapi "k8s.io/api/resource/v1alpha3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
	drav1 "k8s.io/kubelet/pkg/apis/dra/v1alpha4"

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

var _ drav1.NodeServer = &driver{}

type driver struct {
	sync.Mutex
	kubeclient      KubeClient
	nodename        string
	cdi             *cdi.CDI
	devices         device.QATDevices
	resourcedevices *[]resourceapi.Device
	plugin          kubeletplugin.DRAPlugin
	statefile       string
}

func (d *driver) getResourceClaim(ctx context.Context, claim *drav1.Claim) (*resourceapi.ResourceClaim, error) {
	resourceclaim, err := d.kubeclient.ResourceV1alpha3().ResourceClaims(claim.Namespace).Get(ctx, claim.Name, metav1.GetOptions{})
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
		fmt.Printf("NodePrepareResources: claim %s\n", claim.GetUID())
		preparedResourcesResponse.Claims[claim.GetUID()] = d.allocateResource(ctx, claim)
	}

	return preparedResourcesResponse, nil
}

func (d *driver) allocateResource(ctx context.Context, claim *drav1.Claim) *drav1.NodePrepareResourceResponse {
	resourceclaim, err := d.getResourceClaim(ctx, claim)
	if err != nil {

		fmt.Printf("Error: %s", err.Error())

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
			fmt.Printf("Driver/pool '%s/%s' not handled by driver (%s/%s)\n",
				deviceallocationresult.Driver, deviceallocationresult.Pool,
				driverName, d.nodename)

			continue
		}

		requestedDeviceUID := deviceallocationresult.Device

		fmt.Printf("requested device UID '%s'\n", requestedDeviceUID)

		// allocate specified QAT VF device which can have any service configured
		vfDevice, deviceConfigurationChanged, err = d.devices.Allocate(requestedDeviceUID, device.Unset, claim.GetUID())
		if err != nil {

			fmt.Printf("Error: %s", err.Error())

			for _, vf := range allocatedvfs {
				_, _ = vf.Free(claim.GetUID())
			}
			return &drav1.NodePrepareResourceResponse{
				Error: err.Error(),
			}
		}
		allocatedvfs = append(allocatedvfs, vfDevice)

		cdidevicename := cdi.CDIKind + "=" + vfDevice.UID()
		fmt.Printf("Allocated CDI devices '%s' and '%s' for claim '%s'\n", cdidevicename, controldevicename, claim.GetUID())

		// add device
		response.Devices = append(response.Devices, &drav1.Device{
			RequestNames: []string{deviceallocationresult.Request},
			PoolName:     deviceallocationresult.Pool,
			DeviceName:   deviceallocationresult.Device,
			CDIDeviceIDs: []string{cdidevicename, controldevicename},
		})

		fmt.Printf("%v\n", response.Devices)
	}

	if err := d.devices.SaveState(d.statefile); err != nil {
		return &drav1.NodePrepareResourceResponse{
			Error: err.Error(),
		}
	}

	if deviceConfigurationChanged {
		d.UpdateDeviceResources(ctx)
	}

	return response
}

func (d *driver) NodeUnprepareResources(ctx context.Context, req *drav1.NodeUnprepareResourcesRequest) (*drav1.NodeUnprepareResourcesResponse, error) {

	unpreparedResourcesResponse := &drav1.NodeUnprepareResourcesResponse{
		Claims: map[string]*drav1.NodeUnprepareResourceResponse{},
	}

	for _, claim := range req.Claims {
		fmt.Printf("NodeUnprepareResources: claim %s\n", claim.GetUID())

		unpreparedResourcesResponse.Claims[claim.GetUID()] = d.freeDevice(ctx, claim)
	}

	return unpreparedResourcesResponse, nil
}

func (d *driver) freeDevice(ctx context.Context, claim *drav1.Claim) *drav1.NodeUnprepareResourceResponse {
	savestate := false

	resourceclaim, err := d.getResourceClaim(ctx, claim)
	if err != nil {
		return &drav1.NodeUnprepareResourceResponse{
			Error: err.Error(),
		}
	}

	d.Lock()
	defer d.Unlock()

	for _, deviceallocationresult := range resourceclaim.Status.Allocation.Devices.Results {

		requestedDeviceUID := deviceallocationresult.Device

		if updated, err := d.devices.Free(requestedDeviceUID, claim.GetUID()); err != nil {
			fmt.Printf("Could not free claim uid '%s', ignoring: %v\n", claim.GetUID(), err)
		} else {
			savestate = true
			fmt.Printf("Claim with uid '%s' freed\n", claim.GetUID())
			if updated {
				d.UpdateDeviceResources(ctx)
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

func (d *driver) UpdateDeviceResources(ctx context.Context) {
	if d.plugin == nil {
		return
	}

	resources := kubeletplugin.Resources{
		Devices: *d.resourcedevices,
	}
	d.plugin.PublishResources(ctx, resources)
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
		fmt.Printf("cannot apply default configuration: %vn", err)
	}

	detectedcdidevices := device.GetCDIDevices(pfdevices)

	if err := cdi.SyncDevices(detectedcdidevices); err != nil {
		return nil, fmt.Errorf("cannot sync CDI devices: %v", err)
	}

	detectedresourcedevices := device.GetResourceDevices(pfdevices)
	resourcedevices := deviceResources(detectedresourcedevices)

	fmt.Printf("configured\n")
	d := &driver{
		kubeclient:      kubeclient,
		nodename:        nodename,
		cdi:             cdi,
		devices:         pfdevices,
		resourcedevices: resourcedevices,
		statefile:       stateFileName,
	}

	if err := d.devices.SetupSaveStateFile(d.statefile); err != nil {
		return nil, fmt.Errorf("could not set up save state file '%s': %v", d.statefile, err)
	}

	return d, nil
}
