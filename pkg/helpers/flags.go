/*
 * Copyright (c) 2025, Intel Corporation.  All Rights Reserved.
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

package helpers

import (
	"fmt"
	"strings"

	coreclientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/component-base/featuregate"
	logsapi "k8s.io/component-base/logs/api/v1"

	_ "k8s.io/component-base/logs/json/register" // for JSON log output support

	"github.com/spf13/pflag"
	"github.com/urfave/cli/v2"
)

type KubeClientConfig struct {
	KubeConfig   string
	KubeAPIQPS   float64
	KubeAPIBurst int
}

type ClientSets struct {
	Core coreclientset.Interface
}

func (k *KubeClientConfig) Flags() []cli.Flag {
	flags := []cli.Flag{
		&cli.StringFlag{
			Category:    "Kubernetes client:",
			Name:        "kubeconfig",
			Usage:       "Absolute path to the `KUBECONFIG` file. Either this flag or the KUBECONFIG env variable need to be set if the driver is being run out of cluster.",
			Destination: &k.KubeConfig,
			EnvVars:     []string{"KUBECONFIG"},
		},
		&cli.Float64Flag{
			Category:    "Kubernetes client:",
			Name:        "kube-api-qps",
			Usage:       "`QPS` to use while communicating with the Kubernetes apiserver.",
			Value:       5,
			Destination: &k.KubeAPIQPS,
			EnvVars:     []string{"KUBE_API_QPS"},
		},
		&cli.IntFlag{
			Category:    "Kubernetes client:",
			Name:        "kube-api-burst",
			Usage:       "`Burst` to use while communicating with the Kubernetes apiserver.",
			Value:       10,
			Destination: &k.KubeAPIBurst,
			EnvVars:     []string{"KUBE_API_BURST"},
		},
	}

	return flags
}

func (k *KubeClientConfig) NewClientSetConfig() (*rest.Config, error) {
	var csconfig *rest.Config

	var err error
	if k.KubeConfig == "" {
		csconfig, err = rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("create in-cluster client configuration: %v", err)
		}
	} else {
		csconfig, err = clientcmd.BuildConfigFromFlags("", k.KubeConfig)
		if err != nil {
			return nil, fmt.Errorf("create out-of-cluster client configuration: %v", err)
		}
	}

	csconfig.QPS = float32(k.KubeAPIQPS)
	csconfig.Burst = k.KubeAPIBurst

	return csconfig, nil
}

func (k *KubeClientConfig) NewClientSets() (ClientSets, error) {
	csconfig, err := k.NewClientSetConfig()
	if err != nil {
		return ClientSets{}, fmt.Errorf("create client configuration: %v", err)
	}

	coreclient, err := coreclientset.NewForConfig(csconfig)
	if err != nil {
		return ClientSets{}, fmt.Errorf("create core client: %v", err)
	}

	return ClientSets{
		Core: coreclient,
	}, nil
}

type LoggingConfig struct {
	featureGate featuregate.MutableFeatureGate
	config      *logsapi.LoggingConfiguration
}

func NewLoggingConfig() *LoggingConfig {
	fg := featuregate.NewFeatureGate()
	var _ pflag.Value = fg // compile-time check for the type conversion below
	l := &LoggingConfig{
		featureGate: fg,
		config:      logsapi.NewLoggingConfiguration(),
	}
	utilruntime.Must(logsapi.AddFeatureGates(fg))
	utilruntime.Must(l.featureGate.SetFromMap(map[string]bool{string(logsapi.ContextualLogging): true}))
	return l
}

// Apply should be called in a cli.App.Before directly after parsing command
// line flags and before running any code which emits log entries.
func (l *LoggingConfig) Apply() error {
	return logsapi.ValidateAndApply(l.config, l.featureGate)
}

// Flags returns the flags for the configuration.
func (l *LoggingConfig) Flags() []cli.Flag {
	var fs pflag.FlagSet
	logsapi.AddFlags(l.config, &fs)

	// Adding the feature gates flag to fs means that its going to be added
	// with "logging" as category. In practice, the logging code is the
	// only code which uses the flag, therefore that seems like a good
	// place to report it.
	fs.AddFlag(&pflag.Flag{
		Name: "feature-gates",
		Usage: "A set of key=value pairs that describe feature gates for alpha/experimental features. " +
			"Options are:\n     " + strings.Join(l.featureGate.KnownFeatures(), "\n     "),
		Value: l.featureGate.(pflag.Value), //nolint:forcetypeassert // No need for type check: l.featureGate is a *featuregate.featureGate, which implements pflag.Value.
	})

	var flags []cli.Flag
	fs.VisitAll(func(flag *pflag.Flag) {
		flags = append(flags, pflagToCLI(flag, "Logging:"))
	})
	return flags
}

func pflagToCLI(flag *pflag.Flag, category string) cli.Flag {
	return &cli.GenericFlag{
		Name:        flag.Name,
		Category:    category,
		Usage:       flag.Usage,
		Value:       flag.Value,
		Destination: flag.Value,
		EnvVars:     []string{strings.ToUpper(strings.ReplaceAll(flag.Name, "-", "_"))},
	}
}
