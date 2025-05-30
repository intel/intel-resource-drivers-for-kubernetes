package gpu

import (
	"context"
	"path/filepath"
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
	gpuNamespace                  = "intel-gpu-resource-driver"
	gpuDeviceClassYaml            = "deployments/gpu/base/device-class.yaml"
	gpuNamespaceYaml              = "deployments/gpu/base/namespace.yaml"
	gpuDriverYaml                 = "deployments/gpu/base/resource-driver.yaml"
	gpuResourceClaimTemplateYaml  = "deployments/gpu/examples/resource-claim-template.yaml"
	gpuSampleAppKustomizationYaml = "deployments/gpu/tests/gpu-sample-app/kustomization.yaml"
)

var (
	gpuDeviceClassYamlPath           string
	gpuNamespaceYamlPath             string
	gpuDriverYamlPath                string
	gpuResourceClaimTemplateYamlPath string
)

func init() {
	ginkgo.Describe("GPU DRA Driver", describeGpuDraDriver)
}

func describeGpuDraDriver() {
	f := framework.NewDefaultFramework("gpudra")
	f.NamespacePodSecurityEnforceLevel = admissionapi.LevelPrivileged

	filePaths := map[string]*string{
		gpuDeviceClassYaml:            &gpuDeviceClassYamlPath,
		gpuNamespaceYaml:              &gpuNamespaceYamlPath,
		gpuDriverYaml:                 &gpuDriverYamlPath,
		gpuResourceClaimTemplateYaml:  &gpuResourceClaimTemplateYamlPath,
	}
	for file, pathVar := range filePaths {
		locatedPath, err := utils.LocateRepoFile(file)
		if err != nil {
			framework.Failf("unable to locate %q: %v", file, err)
		}
		*pathVar = locatedPath
	}

	ginkgo.BeforeEach(func(ctx context.Context) {
		ginkgo.By("deploying GPU plugin")
		e2ekubectl.RunKubectlOrDie(gpuNamespace, "apply", "-f", gpuNamespaceYamlPath)
		e2ekubectl.RunKubectlOrDie(gpuNamespace, "apply", "-f", gpuDriverYamlPath)
		_, _ = e2epod.WaitForPodsWithLabelRunningReady(ctx, f.ClientSet, gpuNamespace,
			labels.Set{"app": "intel-gpu-resource-driver-kubelet-plugin"}.AsSelector(), 1 /* one replica */, 100*time.Second)
		e2ekubectl.RunKubectlOrDie(gpuNamespace, "apply", "-f", gpuDeviceClassYamlPath)
		e2ekubectl.RunKubectlOrDie(gpuNamespace, "apply", "-f", gpuResourceClaimTemplateYamlPath)
		time.Sleep(10 * time.Second)
	})

	ginkgo.AfterEach(func(ctx context.Context) {
		ginkgo.By("undeploying all in the GPU namespace")
		e2ekubectl.RunKubectlOrDie(gpuNamespace, "delete", "-f", gpuNamespaceYamlPath)
	})

	ginkgo.Context("When GPU DRA driver is running", func() {
		ginkgo.It("deploys a GPU sample application pod", func(ctx context.Context) {
			gpuSampleAppKustomizeDir, err := utils.LocateRepoFile(gpuSampleAppKustomizationYaml)
			if err != nil {
    			framework.Failf("unable to locate %q: %v", gpuSampleAppKustomizationYaml, err)
			}
			e2ekubectl.RunKubectlOrDie(gpuNamespace, "apply", "-k", filepath.Dir(gpuSampleAppKustomizeDir))

			ginkgo.By("waiting the GPU sample app pod to finish successfully")
			err = e2epod.WaitForPodSuccessInNamespaceTimeout(ctx, f.ClientSet, "gpu-sample-app", gpuNamespace, 300*time.Second)
			gomega.Expect(err).To(gomega.BeNil(), utils.GetPodLogs(ctx, f, "gpu-sample-app", "gpu-sample-app"))
		})
	})
}
