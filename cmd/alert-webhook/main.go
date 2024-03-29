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
	"net/http"
	"os"

	"github.com/spf13/cobra"

	"k8s.io/component-base/cli"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/term"
	"k8s.io/klog/v2"
)

const (
	alertURL = "/alertmanager/api/v1/alerts"
)

// tainter k8s connectivity.
type kubeFlags struct {
	config *string
}

// manual settings.
type cliFlags struct {
	node   *string
	reason *string
}

// alert notification HTTP server settings.
type httpFlags struct {
	address *string
	only    *bool
}

// alert filtering.
type filterFlags struct {
	alerts *string
	groups *string
	values *string
}

type flagsType struct {
	kube   kubeFlags
	cli    cliFlags
	http   httpFlags
	filter filterFlags
}

func main() {
	command := newCommand()
	code := cli.Run(command)
	os.Exit(code)
}

// NewCommand creates a *cobra.Command object with default parameters.
func newCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "alert-webhook",
		Short: "CLI client + Alertmanager webhook for tainting GPUs",
	}
	flags := addFlags(cmd)

	cmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if *flags.cli.node == "" && *flags.http.address == "" {
			return fmt.Errorf("neither (CLI) node name nor (webhook) listen address given")
		}
		if (*flags.filter.groups != "" || *flags.filter.values != "") &&
			(*flags.filter.groups == "" || *flags.filter.values == "") {
			return fmt.Errorf("both of groups and (their accepted) values should be given, not just one of them")
		}
		return nil
	}

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		var err error
		var tainter *tainter

		if !*flags.http.only {
			ctx := context.Background()
			// tainter encapsulates all k8s access
			tainter, err = newTainter(ctx, *flags.kube.config)
			if err != nil {
				return err
			}
			if err = tainter.setTaintsFromFlags(&flags.cli); err != nil {
				return err
			}
		}

		if *flags.http.address == "" {
			return nil
		}

		var alerter *alerter
		if *flags.http.only {
			alerter, err = newAlerter(&flags.filter, nil)
		} else {
			alerter, err = newAlerter(&flags.filter, tainter)
		}
		if err != nil {
			return err
		}

		klog.V(3).Infof("Listening for Alertmanager webhook alerts on %s%s",
			*flags.http.address, alertURL)
		http.HandleFunc(alertURL, alerter.parseRequests)
		return http.ListenAndServe(*flags.http.address, nil)
	}

	return cmd
}

func addFlags(cmd *cobra.Command) *flagsType {
	flags := &flagsType{}
	sharedFlagSets := cliflag.NamedFlagSets{}

	fs := sharedFlagSets.FlagSet("Kubernetes client")
	flags.kube.config = fs.String("kubeconfig", "", "Absolute path to the kube.config file")

	fs = sharedFlagSets.FlagSet("Manual GPU taint settings")
	flags.cli.node = fs.String("node", "", "Cluster node from which GPUs taint should be removed")
	flags.cli.reason = fs.String("reason", "", "Taint given node GPUs with given reason, instead of removing taints")

	fs = sharedFlagSets.FlagSet("Alertmanager webhook")
	flags.http.address = fs.String("address", "", "Address to listen for Alertmanager calls")
	flags.http.only = fs.Bool("only-http", false, "Test just HTTP server functionality without k8s / node CRD updates")

	fs = sharedFlagSets.FlagSet("Alert filtering rules")
	flags.filter.alerts = fs.String("alerts", "", "Comma separate list of alerts (names) setting GPU tainted")
	flags.filter.groups = fs.String("groups", "", "Comma separate list of group labels to check against values list")
	flags.filter.values = fs.String("values", "", "Comma separate list of accepted values for specified group labels")

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
