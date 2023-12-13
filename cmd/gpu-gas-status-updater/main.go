package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	coreclientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/retry"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/term"
	"k8s.io/klog/v2"

	intelclientset "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gpu/clientset/versioned"
	intelcrd "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gpu/v1alpha2/api"
)

// Flags holds input parameter flags.
type Flags struct {
	kubeconfig *string
	status     *string
}

// Config holds Flags and CRD with clientset.
type Config struct {
	flags       *Flags
	intelcrd    *intelcrd.GpuAllocationState
	intelclient intelclientset.Interface
}

func main() {
	command := newCommand()
	err := command.Execute()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	}
}

func newCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "Status Updater",
		Short: "Intel GPU Allocation State (GAS) Status Updater",
	}

	flags := addFlags(cmd)
	ctx := context.Background()

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		err := validateFlags(flags)
		if err != nil {
			return fmt.Errorf("validate flags: %v", err)
		}

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
			return fmt.Errorf("create intel client: %v", err)
		}

		nodeName := os.Getenv("NODE_NAME")
		podNamespace := os.Getenv("POD_NAMESPACE")

		node, err := coreclient.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("get node object: %v", err)
		}

		crdconfig := &intelcrd.GpuAllocationStateConfig{
			Name:      nodeName,
			Namespace: podNamespace,
			Owner: &metav1.OwnerReference{
				APIVersion: "v1",
				Kind:       "Node",
				Name:       nodeName,
				UID:        node.UID,
			},
		}
		intelcrd := intelcrd.NewGpuAllocationState(crdconfig, intelclient)

		config := &Config{
			flags:       flags,
			intelcrd:    intelcrd,
			intelclient: intelclient,
		}

		errorSetStatus := setStatus(ctx, config, nodeName, podNamespace)
		if errorSetStatus != nil {
			return fmt.Errorf("set status: %v", errorSetStatus)
		}
		return errorSetStatus
	}

	return cmd
}

func addFlags(cmd *cobra.Command) *Flags {
	flags := &Flags{}
	sharedFlagSets := cliflag.NamedFlagSets{}

	fs := sharedFlagSets.FlagSet("Kubernetes client")
	flags.kubeconfig = fs.String("kubeconfig", "", "Absolute path to the kube.config file.")
	flags.status = fs.String("status", "", "The status to set [Ready | NotReady].")

	fs = cmd.PersistentFlags()
	for _, f := range sharedFlagSets.FlagSets {
		fs.AddFlagSet(f)
	}

	cols, _, _ := term.TerminalSize(cmd.OutOrStdout())
	cliflag.SetUsageAndHelpFunc(cmd, sharedFlagSets, cols)

	return flags
}

func validateFlags(f *Flags) error {
	switch strings.ToLower(*f.status) {
	case strings.ToLower(intelcrd.GpuAllocationStateStatusReady):
		*f.status = intelcrd.GpuAllocationStateStatusReady
	case strings.ToLower(intelcrd.GpuAllocationStateStatusNotReady):
		*f.status = intelcrd.GpuAllocationStateStatusNotReady
	default:
		return fmt.Errorf("unknown status: %v", *f.status)
	}
	return nil
}

func getClientsetConfig(f *Flags) (*rest.Config, error) {
	var csconfig *rest.Config

	kubeconfigEnv := os.Getenv("KUBECONFIG")
	if kubeconfigEnv != "" {
		klog.Infof("Found KUBECONFIG environment variable set, using that..")
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

	return csconfig, nil
}

func setStatus(ctx context.Context, config *Config, nodeName string, podNamespace string) error {

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {

		klog.V(5).Infof("fetching GAS %v", nodeName)
		gas := config.intelcrd
		err := gas.GetOrCreate(ctx)
		if err != nil {
			return fmt.Errorf("error retrieving GAS CRD for node %v: %v", nodeName, err)
		}

		if gas.Status == *config.flags.status {
			return nil
		}

		return gas.UpdateStatus(ctx, *config.flags.status)
	})

	if err != nil {
		return err
	}

	klog.V(5).Infof("GAS status updated")

	return nil
}
