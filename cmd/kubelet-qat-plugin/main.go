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

	"github.com/spf13/cobra"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/featuregate"
	"k8s.io/component-base/logs"
	logsapi "k8s.io/component-base/logs/api/v1"
	"k8s.io/component-base/term"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
	"k8s.io/klog/v2"

	driverVersion "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/version"
)

func cmdRun(cmd *cobra.Command, args []string) error {
	var (
		d   *driver
		err error
	)

	klog.Info("DRA QAT kubelet plugin")
	driverVersion.PrintDriverVersion(driverName)

	ctx := context.Background()

	if err := os.MkdirAll(kubeletPluginDataDirPath, 0750); err != nil {
		return fmt.Errorf("could not create '%s': %v", kubeletPluginDataDirPath, err)
	}

	if err := os.MkdirAll(pluginRegistrationDirPath, 0750); err != nil {
		return fmt.Errorf("could not create '%s': %v", pluginRegistrationDirPath, err)
	}

	if d, err = newDriver(ctx); err != nil {
		return fmt.Errorf("failed to create kubelet plugin driver: %v", err)
	}

	cmd.Flags()
	helper, err := kubeletplugin.Start(
		ctx,
		d,
		kubeletplugin.KubeClient(d.kubeclient),
		kubeletplugin.NodeName(d.nodename),
		kubeletplugin.DriverName(driverName),
		kubeletplugin.RegistrarDirectoryPath(pluginRegistrationDirPath),
		kubeletplugin.PluginDataDirectoryPath(kubeletPluginDataDirPath))
	if err != nil {
		return fmt.Errorf("failed to start kubelet plugin: %v", err)
	}

	d.helper = helper

	if err := d.UpdateDeviceResources(ctx); err != nil {
		return fmt.Errorf("failed to publish resources: %v", err)
	}

	klog.Infof("DRA kubelet plugin %s running...", driverName)

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	<-sigc

	helper.Stop()

	klog.Infof("DRA kubelet plugin %s done", driverName)

	return nil
}

func setupCmd() (*cobra.Command, error) {
	cmd := &cobra.Command{
		Use:   "kubelet-plugin",
		Short: "Intel QAT resource driver kubelet plugin",
		RunE:  cmdRun,
	}

	logsconfig := logsapi.NewLoggingConfiguration()
	fgate := featuregate.NewFeatureGate()
	utilruntime.Must(logsapi.AddFeatureGates(fgate))
	if err := logsapi.ValidateAndApply(logsconfig, fgate); err != nil {
		return nil, err
	}

	loggingFlags := cliflag.NamedFlagSets{}
	fs := loggingFlags.FlagSet("logging")
	logsapi.AddFlags(logsconfig, fs)
	logs.AddFlags(fs, logs.SkipLoggingConfigurationFlags())

	cmd.PersistentFlags().AddFlagSet(fs)

	cols, _, _ := term.TerminalSize(cmd.OutOrStdout())
	cliflag.SetUsageAndHelpFunc(cmd, loggingFlags, cols)

	return cmd, nil
}

func main() {
	cmd, err := setupCmd()
	if err != nil {
		fmt.Printf("Error: failed to start: %v", err)
		return
	}

	// Execute() already prints out the error.
	_ = cmd.Execute()
}
