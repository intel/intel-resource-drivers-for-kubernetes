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

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	coreclientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/featuregate"
	"k8s.io/component-base/logs"
	logsapi "k8s.io/component-base/logs/api/v1"
	"k8s.io/component-base/term"
	"k8s.io/klog/v2"

	"github.com/intel/intel-resource-drivers-for-kubernetes/pkg/gpu/device"
)

const (
	DefaultCDIRoot                   = "/etc/cdi"
	DefaultKubeletPath               = "/var/lib/kubelet/"
	DefaultKubeletPluginDir          = DefaultKubeletPath + "plugins/" + device.DriverName
	DefaultKubeletPluginsRegistryDir = DefaultKubeletPath + "plugins_registry/"
)

type flagsType struct {
	kubeconfig   *string
	kubeAPIQPS   *float32
	kubeAPIBurst *int
}

type configType struct {
	clientset                 coreclientset.Interface
	cdiRoot                   string
	kubeletPluginDir          string
	kubeletPluginsRegistryDir string
	nodeName                  string
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

	flags := addFlags(cmd, logsconfig)

	cmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		cmd.SetContext(metadata.AppendToOutgoingContext(context.Background(), "pre", "run"))

		if err := logsapi.ValidateAndApply(logsconfig, fgate); err != nil {
			return fmt.Errorf("failed to validate logs config: %v", err)
		}

		return nil
	}

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		csconfig, err := getClientSetConfig(flags)
		if err != nil {
			return fmt.Errorf("create client configuration: %v", err)
		}

		coreclient, err := coreclientset.NewForConfig(csconfig)
		if err != nil {
			return fmt.Errorf("create core client: %v", err)
		}

		nodeName, nodeNameFound := os.LookupEnv("NODE_NAME")
		if !nodeNameFound {
			nodeName = "127.0.0.1"
		}

		config := &configType{
			nodeName:                  nodeName,
			clientset:                 coreclient,
			cdiRoot:                   DefaultCDIRoot,
			kubeletPluginDir:          DefaultKubeletPluginDir,
			kubeletPluginsRegistryDir: DefaultKubeletPluginsRegistryDir,
		}

		return callPlugin(cmd.Context(), config)
	}

	return cmd
}

func addFlags(cmd *cobra.Command, logsconfig *logsapi.LoggingConfiguration) *flagsType {
	flags := &flagsType{}

	sharedFlagSets := cliflag.NamedFlagSets{}
	fs := sharedFlagSets.FlagSet("logging")
	logsapi.AddFlags(logsconfig, fs)
	logs.AddFlags(fs, logs.SkipLoggingConfigurationFlags())

	fs = sharedFlagSets.FlagSet("Kubernetes client")
	flags.kubeconfig = fs.String("kubeconfig", "", "Absolute path to the kube.config file")
	flags.kubeAPIQPS = fs.Float32("kube-api-qps", 15, "QPS to use while communicating with the kubernetes apiserver.")
	flags.kubeAPIBurst = fs.Int("kube-api-burst", 45, "Burst to use while communicating with the kubernetes apiserver.")

	fs = cmd.PersistentFlags()
	for _, f := range sharedFlagSets.FlagSets {
		fs.AddFlagSet(f)
	}

	cols, _, _ := term.TerminalSize(cmd.OutOrStdout())
	cliflag.SetUsageAndHelpFunc(cmd, sharedFlagSets, cols)

	return flags
}

func getClientSetConfig(flags *flagsType) (*rest.Config, error) {
	var csconfig *rest.Config
	kubeconfigEnv := os.Getenv("KUBECONFIG")

	if kubeconfigEnv != "" {
		klog.V(5).Info("Found KUBECONFIG environment variable set, using that..")
		*flags.kubeconfig = kubeconfigEnv
	}

	var err error
	if *flags.kubeconfig == "" {
		csconfig, err = rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("create in-cluster client configuration: %v", err)
		}
	} else {
		csconfig, err = clientcmd.BuildConfigFromFlags("", *flags.kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("create out-of-cluster client configuration: %v", err)
		}
	}

	csconfig.QPS = *flags.kubeAPIQPS
	csconfig.Burst = *flags.kubeAPIBurst

	return csconfig, nil
}

func callPlugin(ctx context.Context, config *configType) error {
	if err := os.MkdirAll(config.kubeletPluginDir, 0750); err != nil {
		return fmt.Errorf("failed to create plugin socket dir: %v", err)
	}

	if err := os.MkdirAll(config.kubeletPluginDir, 0750); err != nil {
		return fmt.Errorf("failed to create plugin registrar socket dir: %v", err)
	}

	if err := os.MkdirAll(config.cdiRoot, 0750); err != nil {
		return fmt.Errorf("failed to create CDI root dir: %v", err)
	}

	driver, err := newDriver(ctx, config)
	if err != nil {
		return err
	}

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, syscall.SIGTERM)
	<-sigc

	klog.Info("Received stop stignal, exiting.")
	if err := driver.Shutdown(ctx); err != nil {
		klog.FromContext(ctx).Error(err, "could not stop DRA driver gracefully: %v", err)
		return err
	}

	return nil
}
