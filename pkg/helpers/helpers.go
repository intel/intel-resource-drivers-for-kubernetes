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
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/urfave/cli/v2"
	coreclientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"sigs.k8s.io/dra-example-driver/pkg/flags"
)

const (
	DefaultCDIRoot                   = "/etc/cdi"
	DefaultKubeletPath               = "/var/lib/kubelet/"
	DefaultKubeletPluginDir          = DefaultKubeletPath + "plugins/"
	DefaultKubeletPluginsRegistryDir = DefaultKubeletPath + "plugins_registry/"
)

var (
	TestSysfsRoot = AddRandomString("/tmp/sysfsroot")
	TestDevfsRoot = AddRandomString("/tmp/devfsroot")
)

type Flags struct {
	kubeClientConfig flags.KubeClientConfig
	loggingConfig    *flags.LoggingConfig

	NodeName                  string
	KubeletPluginDir          string
	KubeletPluginsRegistryDir string

	CdiRoot string
}

type Config struct {
	CommonFlags *Flags
	Coreclient  coreclientset.Interface
	DriverFlags interface{}
}

func NewApp(driverName string, newDriver func(ctx context.Context, config *Config) (Driver, error), driverCliFlags []cli.Flag, driverConfigFlags interface{}) *cli.App {
	nodeName, nodeNameFound := os.LookupEnv("NODE_NAME")
	if !nodeNameFound {
		nodeName = "127.0.0.1"
	}

	flags := &Flags{
		loggingConfig:             flags.NewLoggingConfig(),
		NodeName:                  nodeName,
		CdiRoot:                   DefaultCDIRoot,
		KubeletPluginDir:          filepath.Join(DefaultKubeletPluginDir, driverName),
		KubeletPluginsRegistryDir: DefaultKubeletPluginsRegistryDir,
	}
	cliFlags := []cli.Flag{
		&cli.StringFlag{
			Name:        "node-name",
			Usage:       "The name of the node to be worked on.",
			Required:    true,
			Destination: &flags.NodeName,
			EnvVars:     []string{"NODE_NAME"},
		},
		&cli.StringFlag{
			Name:        "cdi-root",
			Usage:       "Absolute path to the directory where CDI files will be generated.",
			Value:       DefaultCDIRoot,
			Destination: &flags.CdiRoot,
			EnvVars:     []string{"CDI_ROOT"},
		},
	}
	cliFlags = append(cliFlags, driverCliFlags...)
	cliFlags = append(cliFlags, flags.kubeClientConfig.Flags()...)
	cliFlags = append(cliFlags, flags.loggingConfig.Flags()...)

	app := &cli.App{
		Name:            "Intel " + driverName + " resource-driver kubelet plugin",
		Usage:           "kubelet-plugin",
		ArgsUsage:       " ",
		HideHelpCommand: true,
		Flags:           cliFlags,
		Before: func(c *cli.Context) error {
			if c.Args().Len() > 0 {
				return fmt.Errorf("arguments not supported: %v", c.Args().Slice())
			}
			return flags.loggingConfig.Apply()
		},
		Action: func(c *cli.Context) error {
			ctx := c.Context
			clientSets, err := flags.kubeClientConfig.NewClientSets()
			if err != nil {
				return fmt.Errorf("create client: %v", err)
			}

			config := &Config{
				CommonFlags: flags,
				Coreclient:  clientSets.Core,
				DriverFlags: driverConfigFlags,
			}

			return StartPlugin(ctx, config, newDriver)
		},
	}

	return app
}

func StartPlugin(ctx context.Context, config *Config, newDriver func(ctx context.Context, config *Config) (Driver, error)) error {
	err := os.MkdirAll(config.CommonFlags.KubeletPluginDir, 0750)
	if err != nil {
		return err
	}

	info, err := os.Stat(config.CommonFlags.CdiRoot)
	switch {
	case err != nil && os.IsNotExist(err):
		err := os.MkdirAll(config.CommonFlags.CdiRoot, 0750)
		if err != nil {
			return err
		}
	case err != nil:
		return err
	case !info.IsDir():
		return fmt.Errorf("path for CDI file generation is not a directory: '%v'", err)
	}

	driver, err := newDriver(ctx, config)
	if err != nil {
		return err
	}

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	signum := <-sigc

	klog.Infof("Received signal %d, exiting.", signum)
	err = driver.Shutdown(ctx)
	if err != nil {
		klog.FromContext(ctx).Error(err, "Unable to cleanly shutdown driver")
	}

	return nil
}

func WriteFile(filePath string, fileContents string) error {
	fhandle, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("could not create file %v: %v", filePath, err)
	}

	if _, err = fhandle.WriteString(fileContents); err != nil {
		return fmt.Errorf("could not write to file %v: %v", filePath, err)
	}

	if err := fhandle.Close(); err != nil {
		return fmt.Errorf("could not close file %v: %v", filePath, err)
	}

	return nil
}

func AddRandomString(str string) string {
	b := make([]byte, 4)
	_, err := rand.Read(b)
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf(str+"_%s", hex.EncodeToString(b))
}
