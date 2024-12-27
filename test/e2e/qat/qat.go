package qat

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/kubernetes/test/e2e/framework"
	e2ekubectl "k8s.io/kubernetes/test/e2e/framework/kubectl"
	e2epod "k8s.io/kubernetes/test/e2e/framework/pod"
	admissionapi "k8s.io/pod-security-admission/api"

	"github.com/intel/intel-resource-drivers-for-kubernetes/test/e2e/utils"
)

const (
	qatNamespace                 = "intel-qat-resource-driver"
	qatDeviceClassYaml           = "deployments/qat/device-class.yaml"
	qatConfigMapYaml             = "deployments/qat/examples/intel-qat-resource-driver-configuration.yaml"
	qatNamespaceYaml             = "deployments/qat/resource-driver-namespace.yaml"
	qatDriverYaml                = "deployments/qat/resource-driver.yaml"
	qatResourceClaimTemplateYaml = "deployments/qat/tests/resource-claim-template.yaml"
)

const (
	opensslEngineKustomizationYaml    = "deployments/qat/tests/openssl-qat-engine/kustomization.yaml"
	dpdkTestKustomizationYaml         = "deployments/qat/tests/qat-dpdk-test/kustomization.yaml"
	qatlibSampleCodeKustomizationYaml = "deployments/qat/tests/qatlib-sample-code/kustomization.yaml"
)

func init() {
	ginkgo.Describe("QAT DRA Driver", describeQatDraDriver)
}

func describeQatDraDriver() {
	f := framework.NewDefaultFramework("qatdra")
	f.NamespacePodSecurityEnforceLevel = admissionapi.LevelPrivileged

	qatDeviceClassYamlPath, errFailedToLocateRepoFile := utils.LocateRepoFile(qatDeviceClassYaml)
	if errFailedToLocateRepoFile != nil {
		framework.Failf("unable to locate %q: %v", qatDeviceClassYaml, errFailedToLocateRepoFile)
	}

	qatNamespaceYamlPath, errFailedToLocateRepoFile := utils.LocateRepoFile(qatNamespaceYaml)
	if errFailedToLocateRepoFile != nil {
		framework.Failf("unable to locate %q: %v", qatNamespaceYaml, errFailedToLocateRepoFile)
	}

	qatConfigMapYamlPath, errFailedToLocateRepoFile := utils.LocateRepoFile(qatConfigMapYaml)
	if errFailedToLocateRepoFile != nil {
		framework.Failf("unable to locate %q: %v", qatConfigMapYaml, errFailedToLocateRepoFile)
	}

	qatDriverYamlPath, errFailedToLocateRepoFile := utils.LocateRepoFile(qatDriverYaml)
	if errFailedToLocateRepoFile != nil {
		framework.Failf("unable to locate %q: %v", qatDriverYaml, errFailedToLocateRepoFile)
	}

	qatResourceClaimTemplateYamlPath, errFailedToLocateRepoFile := utils.LocateRepoFile(qatResourceClaimTemplateYaml)
	if errFailedToLocateRepoFile != nil {
		framework.Failf("unable to locate %q: %v", qatResourceClaimTemplateYaml, errFailedToLocateRepoFile)
	}

	opensslEngineKustomizationYamlPath, errFailedToLocateRepoFile := utils.LocateRepoFile(opensslEngineKustomizationYaml)
	if errFailedToLocateRepoFile != nil {
		framework.Failf("unable to locate %q: %v", opensslEngineKustomizationYaml, errFailedToLocateRepoFile)
	}

	dpdkTestKustomizationYamlPath, errFailedToLocateRepoFile := utils.LocateRepoFile(dpdkTestKustomizationYaml)
	if errFailedToLocateRepoFile != nil {
		framework.Failf("unable to locate %q: %v", dpdkTestKustomizationYaml, errFailedToLocateRepoFile)
	}

	qatlibSampleCodeKustomizationYamlPath, errFailedToLocateRepoFile := utils.LocateRepoFile(qatlibSampleCodeKustomizationYaml)
	if errFailedToLocateRepoFile != nil {
		framework.Failf("unable to locate %q: %v", qatlibSampleCodeKustomizationYaml, errFailedToLocateRepoFile)
	}

	ginkgo.BeforeEach(func(ctx context.Context) {
		ginkgo.By("deploying QAT plugin in DPDK mode")
		e2ekubectl.RunKubectlOrDie(qatNamespace, "apply", "-f", qatNamespaceYamlPath)
		if err := modifyConfigMapYAML(qatConfigMapYamlPath); err != nil {
			framework.Failf("unable to modify ConfigMap yaml file for configuring QAT: %v", err)
		}
		e2ekubectl.RunKubectlOrDie(qatNamespace, "apply", "-f", qatConfigMapYamlPath)
		e2ekubectl.RunKubectlOrDie(qatNamespace, "apply", "-f", qatDriverYamlPath)
		_, _ = e2epod.WaitForPodsWithLabelRunningReady(ctx, f.ClientSet, qatNamespace,
			labels.Set{"app": "intel-qat-resource-driver-kubelet-plugin"}.AsSelector(), 1 /* one replica */, 100*time.Second)
		e2ekubectl.RunKubectlOrDie(qatNamespace, "apply", "-f", qatDeviceClassYamlPath)
		e2ekubectl.RunKubectlOrDie(qatNamespace, "apply", "-f", qatResourceClaimTemplateYamlPath)
		time.Sleep(10 * time.Second)
	})

	ginkgo.AfterEach(func(ctx context.Context) {
		ginkgo.By("undeploying all in the QAT namespace")
		e2ekubectl.RunKubectlOrDie(qatNamespace, "delete", "-f", qatNamespaceYamlPath)
	})

	ginkgo.Context("When QAT DRA driver is running", func() {
		ginkgo.It("deploys an openssl-qat-engine pod", func(ctx context.Context) {
			e2ekubectl.RunKubectlOrDie(qatNamespace, "apply", "-k", filepath.Dir(opensslEngineKustomizationYamlPath))

			ginkgo.By("waiting the openssl-qat-engine pod to finish successfully")
			err := e2epod.WaitForPodSuccessInNamespaceTimeout(ctx, f.ClientSet, "openssl-qat-engine-sym", qatNamespace, 300*time.Second)
			gomega.Expect(err).To(gomega.BeNil(), utils.GetPodLogs(ctx, f, "openssl-qat-engine-sym", "openssl-qat-engine-sym"))
		})

		ginkgo.It("deploys a qat-dpdk-test pod", func(ctx context.Context) {
			e2ekubectl.RunKubectlOrDie(qatNamespace, "apply", "-k", filepath.Dir(dpdkTestKustomizationYamlPath))

			ginkgo.By("waiting the qat-dpdk-test pod to finish successfully")
			err := e2epod.WaitForPodSuccessInNamespaceTimeout(ctx, f.ClientSet, "qat-dpdk-test-crypto-perf", qatNamespace, 300*time.Second)
			gomega.Expect(err).To(gomega.BeNil(), utils.GetPodLogs(ctx, f, "qat-dpdk-test-crypto-perf", "crypto-perf"))
			err = e2epod.WaitForPodSuccessInNamespaceTimeout(ctx, f.ClientSet, "qat-dpdk-test-compress-perf", qatNamespace, 300*time.Second)
			gomega.Expect(err).To(gomega.BeNil(), utils.GetPodLogs(ctx, f, "qat-dpdk-test-compress-perf", "compress-perf"))
		})

		ginkgo.It("deploys a qatlib-sample-code pod", func(ctx context.Context) {
			e2ekubectl.RunKubectlOrDie(qatNamespace, "apply", "-k", filepath.Dir(qatlibSampleCodeKustomizationYamlPath))

			ginkgo.By("waiting the qatlib-sample-code to finish successfully")
			err := e2epod.WaitForPodSuccessInNamespaceTimeout(ctx, f.ClientSet, "qatlib-sample-code-sym", qatNamespace, 300*time.Second)
			gomega.Expect(err).To(gomega.BeNil(), utils.GetPodLogs(ctx, f, "qatlib-sample-code-sym", "qatlib-sample-code-sym"))
			err = e2epod.WaitForPodSuccessInNamespaceTimeout(ctx, f.ClientSet, "qatlib-sample-code-dc", qatNamespace, 300*time.Second)
			gomega.Expect(err).To(gomega.BeNil(), utils.GetPodLogs(ctx, f, "qatlib-sample-code-dc", "qatlib-sample-code-dc"))
		})
	})

}

// ConfigMap will not be used after partitionable device is impelmented.
// This is a temporary way to configure QAT devices.
func modifyConfigMapYAML(yamlPath string) error {
	content, err := os.ReadFile(yamlPath)
	if err != nil {
		return err
	}

	newHostName, err := e2ekubectl.RunKubectl("", "get", "nodes", "-o", "jsonpath={.items[0].metadata.name}")
	if err != nil {
		return err
	}

	modifiedContent := strings.ReplaceAll(string(content), "host-name-here", strings.TrimSpace(newHostName))
	modifiedContent = strings.ReplaceAll(modifiedContent, "0000:aa:00.0", "0000:6b:00.0")
	modifiedContent = strings.ReplaceAll(modifiedContent, "0000:bb:00.0", "0000:e8:00.0")

	err = os.WriteFile(yamlPath, []byte(modifiedContent), 0644)
	if err != nil {
		return err
	}

	return nil
}
