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
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"google.golang.org/grpc/metadata"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	coreclientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/featuregate"
	"k8s.io/component-base/logs"
	logsapi "k8s.io/component-base/logs/api/v1"
	"k8s.io/component-base/term"
	plugin "k8s.io/dynamic-resource-allocation/kubeletplugin"
	"k8s.io/klog/v2"

	intelclientset "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gpu/clientset/versioned"
	intelcrd "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gpu/v1alpha2/api"
)

const (
	pluginRegistrationPath = "/var/lib/kubelet/plugins_registry/" + intelcrd.APIGroupName + ".sock"
	driverPluginPath       = "/var/lib/kubelet/plugins/" + intelcrd.APIGroupName
	driverPluginSocketPath = driverPluginPath + "/plugin.sock"

	cdiRoot   = "/etc/cdi"
	cdiVendor = "intel.com"
	cdiKind   = cdiVendor + "/gpu"

	kubeAPIQps   = 5
	kubeAPIBurst = 10
)

type clientsetType struct {
	core  coreclientset.Interface
	intel intelclientset.Interface
}

type configType struct {
	crdconfig *intelcrd.GpuAllocationStateConfig
	clientset *clientsetType
	cdiRoot   string
}

func main() {
	command := newCommand()
	if err := command.Execute(); err != nil {
		fmt.Printf("Error: %v\n", err)
	}
}

func newCommand() *cobra.Command {
	logsconfig := logsapi.NewLoggingConfiguration()
	fgate := featuregate.NewFeatureGate()
	utilruntime.Must(logsapi.AddFeatureGates(fgate))

	cmd := &cobra.Command{
		Use:   "kubelet-plugin",
		Short: "Intel GPU resource-driver kubelet plugin",
	}

	sharedFlagSets := cliflag.NamedFlagSets{}
	fs := sharedFlagSets.FlagSet("logging")
	logsapi.AddFlags(logsconfig, fs)
	logs.AddFlags(fs, logs.SkipLoggingConfigurationFlags())
	fs = cmd.PersistentFlags()
	for _, f := range sharedFlagSets.FlagSets {
		fs.AddFlagSet(f)
	}

	cols, _, _ := term.TerminalSize(cmd.OutOrStdout())
	cliflag.SetUsageAndHelpFunc(cmd, sharedFlagSets, cols)

	cmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		cmd.SetContext(metadata.AppendToOutgoingContext(context.Background(), "pre", "run"))

		if err := logsapi.ValidateAndApply(logsconfig, fgate); err != nil {
			return fmt.Errorf("failed to validate logs config: %v", err)
		}

		return nil
	}

	cmd.RunE = commandExecutable

	return cmd
}

func commandExecutable(cmd *cobra.Command, args []string) error {
	clientsetconfig, err := getClientSetConfig()
	if err != nil {
		return fmt.Errorf("create client configuration: %v", err)
	}

	coreclient, err := coreclientset.NewForConfig(clientsetconfig)
	if err != nil {
		return fmt.Errorf("create core client: %v", err)
	}

	intelclient, err := intelclientset.NewForConfig(clientsetconfig)
	if err != nil {
		return fmt.Errorf("create Intel client: %v", err)
	}

	nodeName, nodeNameFound := os.LookupEnv("NODE_NAME")
	if !nodeNameFound {
		nodeName = "127.0.0.1"
	}
	podNamespace, podNamespaceFound := os.LookupEnv("POD_NAMESPACE")
	if !podNamespaceFound {
		podNamespace = "default"
	}
	klog.V(3).Infof("node: %v, namespace: %v", nodeName, podNamespace)

	node, err := coreclient.CoreV1().Nodes().Get(cmd.Context(), nodeName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get node object: %v", err)
	}

	config := &configType{
		crdconfig: &intelcrd.GpuAllocationStateConfig{
			Name:      nodeName,
			Namespace: podNamespace,
			Owner: &metav1.OwnerReference{
				APIVersion: "v1",
				Kind:       "Node",
				Name:       nodeName,
				UID:        node.UID,
			},
		},
		clientset: &clientsetType{
			coreclient,
			intelclient,
		},
		cdiRoot: cdiRoot,
	}

	return callPlugin(cmd.Context(), config)
}

func getClientSetConfig() (*rest.Config, error) {
	var csconfig *rest.Config
	kubeconfig := os.Getenv("KUBECONFIG")

	var err error
	if kubeconfig == "" {
		csconfig, err = rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("create in-cluster client configuration: %v", err)
		}
	} else {
		csconfig, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("create out-of-cluster client configuration: %v", err)
		}
	}

	csconfig.QPS = kubeAPIQps
	csconfig.Burst = kubeAPIBurst

	return csconfig, nil
}

func callPlugin(ctx context.Context, config *configType) error {
	err := os.MkdirAll(driverPluginPath, 0750)
	if err != nil {
		return fmt.Errorf("failed to create plugin socket dir: %v", err)
	}

	err = os.MkdirAll(config.cdiRoot, 0750)
	if err != nil {
		return fmt.Errorf("failed to create CDI root dir: %v", err)
	}

	driver, err := newDriver(ctx, config)
	if err != nil {
		return err
	}

	klog.Infof(`Starting DRA resource-driver kubelet-plugin
RegistrarSocketPath: %v
PluginSocketPath: %v
KubeletPluginSocketPath: %v`,
		pluginRegistrationPath,
		driverPluginSocketPath,
		driverPluginSocketPath)

	kubeletPlugin, err := plugin.Start(
		driver,
		plugin.DriverName(intelcrd.APIGroupName),
		plugin.RegistrarSocketPath(pluginRegistrationPath),
		plugin.PluginSocketPath(driverPluginSocketPath),
		plugin.KubeletPluginSocketPath(driverPluginSocketPath))
	if err != nil {
		return fmt.Errorf("failed to start kubelet-plugin: %v", err)
	}

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, syscall.SIGTERM)
	<-sigc
	kubeletPlugin.Stop()

	return nil
}
