/* Copyright (C) 2024 Intel Corporation
 * SPDX-License-Identifier: Apache-2.0
 */

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"k8s.io/dynamic-resource-allocation/kubeletplugin"
)

func main() {
	var (
		err error
		d   *driver
	)

	fmt.Println("DRA kubelet plugin")

	ctx := context.Background()

	if err = os.MkdirAll(driverPluginPath, 0750); err != nil {
		fmt.Printf("Could not create '%s': %v\n", driverPluginPath, err)
		return
	}

	if d, err = newDriver(ctx); err != nil {
		fmt.Printf("failed to create kubelet plugin driver: %v\n", err)
		return
	}

	plugin, err := kubeletplugin.Start(
		ctx,
		d,
		kubeletplugin.KubeClient(d.kubeclient),
		kubeletplugin.NodeName(d.nodename),
		kubeletplugin.DriverName(driverName),
		kubeletplugin.RegistrarSocketPath(pluginRegistrationPath),
		kubeletplugin.PluginSocketPath(driverPluginSocketPath),
		kubeletplugin.KubeletPluginSocketPath(driverPluginSocketPath))
	if err != nil {
		fmt.Printf("failed to start kubelet plugin: %v\n", err)
		return
	}

	d.plugin = plugin

	d.UpdateDeviceResources(ctx)

	fmt.Printf("DRA kubelet plugin for %s running...\n", driverName)

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	<-sigc

	plugin.Stop()

	fmt.Printf("DRA kubelet plugin done\n")
}
