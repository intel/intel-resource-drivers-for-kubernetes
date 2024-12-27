/* Copyright (C) 2024 Intel Corporation
 * SPDX-License-Identifier: Apache-2.0
 */

package main

import (
	"fmt"
	"os"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

type ClientSet struct {
	csconfig *rest.Config
}

type KubeClient kubernetes.Interface

// Create a new client config. Use KUBECONFIG environment variable if set,
// othewise resort to in-cluster config.
func (c *ClientSet) newClientSetConfig() error {
	var err error

	if c.csconfig != nil {
		return nil
	}

	kubeconfenv := os.Getenv("KUBECONFIG")
	if kubeconfenv == "" {
		klog.V(5).Info("In-cluster config")

		c.csconfig, err = rest.InClusterConfig()
		if err != nil {
			return fmt.Errorf("creating in-cluster client configuration: %v", err)
		}
	} else {
		klog.V(5).Infof("Using env variable KUBECONFIG=%s", kubeconfenv)

		c.csconfig, err = clientcmd.BuildConfigFromFlags("", kubeconfenv)
		if err != nil {
			return fmt.Errorf("creating out-of-cluster client configuration: %v", err)
		}

	}

	return nil
}

func (c *ClientSet) NewKubeClient() (KubeClient, error) {
	if err := c.newClientSetConfig(); err != nil {
		return nil, err
	}

	kubeclient, err := kubernetes.NewForConfig(c.csconfig)
	if err != nil {
		return nil, fmt.Errorf("creating kubernetes client: %v", err)
	}

	return kubeclient, nil
}
