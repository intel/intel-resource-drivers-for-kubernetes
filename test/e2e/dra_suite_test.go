package e2e_test

import (
	"context"
	"flag"
	"os"
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/component-base/logs"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"
	e2epod "k8s.io/kubernetes/test/e2e/framework/pod"

	_ "github.com/intel/intel-resource-drivers-for-kubernetes/test/e2e/qat"
)

func init() {
	ginkgo.SynchronizedBeforeSuite(setupFirstNode, func(data []byte) {})
}

func setupFirstNode(ctx context.Context) []byte {
	c, err := framework.LoadClientset()
	if err != nil {
		framework.Failf("Error loading client: %v", err)
	}

	// Delete any namespaces except those created by the system. This ensures no
	// lingering resources are left over from a previous test run.
	if framework.TestContext.CleanStart {
		deleted, err2 := framework.DeleteNamespaces(ctx, c, nil, /* deleteFilter */
			[]string{
				metav1.NamespaceSystem,
				metav1.NamespaceDefault,
				metav1.NamespacePublic,
				v1.NamespaceNodeLease,
				"cert-manager",
			})
		if err2 != nil {
			framework.Failf("Error deleting orphaned namespaces: %v", err2)
		}

		framework.Logf("Waiting for deletion of the following namespaces: %v", deleted)

		if err2 = framework.WaitForNamespacesDeleted(ctx, c, deleted, e2epod.DefaultPodDeletionTimeout); err2 != nil {
			framework.Failf("Failed to delete orphaned namespaces %v: %v", deleted, err2)
		}
	}

	return []byte{}
}
func TestDra(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "E2E DRA Drivers Suite")
}

func TestMain(m *testing.M) {
	klog.SetOutput(ginkgo.GinkgoWriter)

	logs.InitLogs()
	config.CopyFlags(config.Flags, flag.CommandLine)
	framework.RegisterCommonFlags(flag.CommandLine)
	framework.RegisterClusterFlags(flag.CommandLine)
	flag.Parse()

	// Register framework flags, then handle flags.
	framework.AfterReadingAllFlags(&framework.TestContext)

	// Now run the test suite.
	os.Exit(m.Run())
}
