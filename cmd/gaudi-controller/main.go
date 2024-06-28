/*
 * Copyright (c) 2024, Intel Corporation.  All Rights Reserved.
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
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"path"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/spf13/cobra"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/informers"
	coreclientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/component-base/cli"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/featuregate"
	"k8s.io/component-base/logs"
	logsapi "k8s.io/component-base/logs/api/v1"
	_ "k8s.io/component-base/logs/json/register"
	"k8s.io/component-base/metrics/legacyregistry"
	_ "k8s.io/component-base/metrics/prometheus/clientgo/leaderelection"
	_ "k8s.io/component-base/metrics/prometheus/restclient"
	_ "k8s.io/component-base/metrics/prometheus/version"
	_ "k8s.io/component-base/metrics/prometheus/workqueue"
	"k8s.io/component-base/term"
	"k8s.io/dynamic-resource-allocation/controller"
	"k8s.io/klog/v2"

	intelclientset "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gaudi/clientset/versioned"
	intelcrd "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gaudi/v1alpha1/api"
)

type flagsType struct {
	kubeconfig   *string
	kubeAPIQPS   *float32
	kubeAPIBurst *int
	workers      *int

	httpEndpoint *string
	metricsPath  *string
	profilePath  *string
}

type clientsetType struct {
	core  coreclientset.Interface
	intel intelclientset.Interface
}

type configType struct {
	namespace  string
	flags      *flagsType
	csconfig   *rest.Config
	clientsets *clientsetType
	ctx        context.Context
	mux        *http.ServeMux
}

func main() {
	command := newCommand()
	code := cli.Run(command)
	os.Exit(code)
}

// NewCommand creates a *cobra.Command object with default parameters.
func newCommand() *cobra.Command {
	logsconfig := logsapi.NewLoggingConfiguration()
	fgate := featuregate.NewFeatureGate()
	utilruntime.Must(logsapi.AddFeatureGates(fgate))

	cmd := &cobra.Command{
		Use:   "controller",
		Short: "Intel Gaudi resource-driver controller",
	}

	flags := addFlags(cmd, logsconfig, fgate)

	cmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		// Activate logging as soon as possible, after that
		// show flags with the final logging configuration.
		if err := logsapi.ValidateAndApply(logsconfig, fgate); err != nil {
			return fmt.Errorf("logsapi failed: %v", err)
		}

		return nil
	}

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		mux := http.NewServeMux()

		csconfig, err := getClientsetConfig(flags)
		if err != nil {
			return fmt.Errorf("create client configuration: %v", err)
		}

		coreclient, err := coreclientset.NewForConfig(csconfig)
		if err != nil {
			return fmt.Errorf("create core client: %v", err)
		}

		intelclient, err := intelclientset.NewForConfig(csconfig)
		if err != nil {
			return fmt.Errorf("create Intel client: %v", err)
		}

		nsname, nsnamefound := os.LookupEnv("POD_NAMESPACE")
		if !nsnamefound {
			nsname = "default"
		}

		config := &configType{
			ctx:       ctx,
			mux:       mux,
			flags:     flags,
			csconfig:  csconfig,
			namespace: nsname,
			clientsets: &clientsetType{
				coreclient,
				intelclient,
			},
		}

		if *flags.httpEndpoint != "" {
			err = setupHTTPEndpoint(config)
			if err != nil {
				return fmt.Errorf("create http endpoint: %v", err)
			}
		}

		if err := startClaimParametersGenerator(ctx, config); err != nil {
			return fmt.Errorf("failed to start ResourceClaimParamaters generator: %v", err)
		}

		startController(config)
		return nil
	}

	return cmd
}

func addFlags(cmd *cobra.Command,
	logsconfig *logsapi.LoggingConfiguration,
	fgate featuregate.MutableFeatureGate) *flagsType {
	flags := &flagsType{}

	sharedFlagSets := cliflag.NamedFlagSets{}
	fs := sharedFlagSets.FlagSet("logging")
	logsapi.AddFlags(logsconfig, fs)
	logs.AddFlags(fs, logs.SkipLoggingConfigurationFlags())

	fs = sharedFlagSets.FlagSet("Kubernetes client")
	flags.kubeconfig = fs.String("kubeconfig", "", "Absolute path to the kube.config file")
	flags.kubeAPIQPS = fs.Float32("kube-api-qps", 15, "QPS to use while communicating with the kubernetes apiserver.")
	flags.kubeAPIBurst = fs.Int("kube-api-burst", 45, "Burst to use while communicating with the kubernetes apiserver.")
	flags.workers = fs.Int("workers", 10, "Concurrency to process multiple claims")

	fs = sharedFlagSets.FlagSet("http server")
	flags.httpEndpoint = fs.String("http-endpoint", "",
		"TCP address to listen for diagnostics HTTP server (i.e.: `:8080`). Empty string = disabled (default)")
	flags.metricsPath = fs.String("metrics-path", "/metrics",
		"HTTP path to expose for Prometheus metrics, Empty string = disabled (default is /metrics).")
	flags.profilePath = fs.String("pprof-path", "",
		"HTTP path for pprof profiling. Empty string = disabled.")

	fs = sharedFlagSets.FlagSet("other")
	fgate.AddFlag(fs)

	fs = cmd.PersistentFlags()
	for _, f := range sharedFlagSets.FlagSets {
		fs.AddFlagSet(f)
	}

	// SetUsageAndHelpFunc takes care of flag grouping. However,
	// it doesn't support listing child commands. We add those
	// to cmd.Use.
	cols, _, _ := term.TerminalSize(cmd.OutOrStdout())
	cliflag.SetUsageAndHelpFunc(cmd, sharedFlagSets, cols)

	return flags
}

func getClientsetConfig(f *flagsType) (*rest.Config, error) {
	var csconfig *rest.Config

	klog.V(5).Info("Getting client config")

	kubeconfigEnv := os.Getenv("KUBECONFIG")
	if kubeconfigEnv != "" {
		klog.V(5).Info("Found KUBECONFIG environment variable set, using that..")
		*f.kubeconfig = kubeconfigEnv
	}

	var err error
	if *f.kubeconfig == "" {
		csconfig, err = rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("create in-cluster client configuration: %v", err)
		}
	} else {
		csconfig, err = clientcmd.BuildConfigFromFlags("", *f.kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("create out-of-cluster client configuration: %v", err)
		}
	}

	csconfig.QPS = *f.kubeAPIQPS
	csconfig.Burst = *f.kubeAPIBurst

	return csconfig, nil
}

func setupHTTPEndpoint(config *configType) error {
	klog.V(5).Info("Setting up HTTP endpoint")

	if *config.flags.metricsPath != "" {
		// To collect metrics data from the metric handler itself, we
		// let it register itself and then collect from that registry.
		reg := prometheus.NewRegistry()
		gatherers := prometheus.Gatherers{
			// Include Go runtime and process metrics:
			// https://github.com/kubernetes/kubernetes/blob/9780d88cb6a4b5b067256ecb4abf56892093ee87/staging/
			// src/k8s.io/component-base/metrics/legacyregistry/registry.go#L46-L49
			legacyregistry.DefaultGatherer,
		}
		gatherers = append(gatherers, reg)

		actualPath := path.Join("/", *config.flags.metricsPath)
		klog.V(3).InfoS("Starting metrics", "path", actualPath)
		// This is similar to k8s.io/component-base/metrics HandlerWithReset
		// except that we gather from multiple sources.
		config.mux.Handle(actualPath,
			promhttp.InstrumentMetricHandler(
				reg,
				promhttp.HandlerFor(gatherers, promhttp.HandlerOpts{})))
	}

	if *config.flags.profilePath != "" {
		actualPath := path.Join("/", *config.flags.profilePath)
		klog.V(3).InfoS("Starting profiling", "path", actualPath)
		config.mux.HandleFunc(path.Join("/", *config.flags.profilePath), pprof.Index)
		config.mux.HandleFunc(path.Join("/", *config.flags.profilePath, "cmdline"), pprof.Cmdline)
		config.mux.HandleFunc(path.Join("/", *config.flags.profilePath, "profile"), pprof.Profile)
		config.mux.HandleFunc(path.Join("/", *config.flags.profilePath, "symbol"), pprof.Symbol)
		config.mux.HandleFunc(path.Join("/", *config.flags.profilePath, "trace"), pprof.Trace)
	}

	listener, err := net.Listen("tcp", *config.flags.httpEndpoint)
	if err != nil {
		return fmt.Errorf("listen on HTTP endpoint: %v", err)
	}

	go func() {
		klog.V(3).InfoS("Starting HTTP server", "endpoint", *config.flags.httpEndpoint)
		err := http.Serve(listener, config.mux)
		if err != nil {
			klog.ErrorS(err, "HTTP server failed")
			klog.FlushAndExit(klog.ExitFlushTimeout, 1)
		}
	}()

	return nil
}

func startController(config *configType) {
	klog.V(3).Info("Starting controller without leader election")
	driver := newDriver(config)
	informerFactory := informers.NewSharedInformerFactory(config.clientsets.core, 0 /* resync period */)
	ctrl := controller.New(config.ctx, intelcrd.APIGroupName, driver, config.clientsets.core, informerFactory)
	informerFactory.Start(config.ctx.Done())
	ctrl.Run(*config.flags.workers)
}
