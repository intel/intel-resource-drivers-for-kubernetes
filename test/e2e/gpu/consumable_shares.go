//
// Copyright (C) 2026 Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0
//

package gpu

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/dynamic-resource-allocation/deviceattribute"
	"k8s.io/kubernetes/test/e2e/framework"
	e2ekubectl "k8s.io/kubernetes/test/e2e/framework/kubectl"
	e2epod "k8s.io/kubernetes/test/e2e/framework/pod"
	"k8s.io/utils/ptr"
)

const (
	gpuDriverName        = "gpu.intel.com"
	gpuDaemonSet         = "daemonset/intel-gpu-resource-driver-kubelet-plugin"
	sharedGPUClaimName   = "shared-gpu-claim"
	sharedGPUPodPrefix   = "shared-gpu-test-"
	sharesPerGPU         = 2
	sharedGPUPodDuration = "600"
)

// describeConsumableShares tests GPU sharing between ResourceClaims with --consumable-shares.
// It runs inside describeGpuDraDriver, so the driver is already deployed. Any GPU works, real or
// from the device faker: the claims are pinned to one GPU by its PCI bus ID.
func describeConsumableShares(f *framework.Framework) {
	var sharedDevice resourcev1.Device
	var sharedDeviceNode string

	ginkgo.BeforeEach(func(ctx context.Context) {
		ginkgo.By(fmt.Sprintf("restarting the GPU plugin with --consumable-shares=%d", sharesPerGPU))
		e2ekubectl.RunKubectlOrDie(gpuNamespace, "set", "env", gpuDaemonSet, "-c", "kubelet-plugin",
			fmt.Sprintf("CONSUMABLE_SHARES=%d", sharesPerGPU))
		e2ekubectl.RunKubectlOrDie(gpuNamespace, "rollout", "status", gpuDaemonSet, "--timeout=120s")

		ginkgo.By("waiting for a shareable GPU in the ResourceSlice")
		device, node, devices, err := waitForShareableGPU(ctx, f)
		if err != nil && devices > 0 {
			// The plugin published GPUs but the apiserver dropped the sharing fields.
			ginkgo.Skip("no shareable GPU published: DRAConsumableCapacity feature gate is likely disabled")
		}
		gomega.Expect(err).To(gomega.BeNil(), "waiting for a shareable GPU in the ResourceSlice")
		sharedDevice, sharedDeviceNode = *device, node

		pciBusID := sharedDevice.Attributes[resourcev1.QualifiedName(deviceattribute.StandardDeviceAttributePCIBusID)].StringValue
		gomega.Expect(pciBusID).ToNot(gomega.BeNil(), "shareable GPU has no PCI bus ID attribute")

		ginkgo.By(fmt.Sprintf("creating a ResourceClaimTemplate pinned to GPU %s on node %s", sharedDevice.Name, sharedDeviceNode))
		template := &resourcev1.ResourceClaimTemplate{
			ObjectMeta: metav1.ObjectMeta{Name: sharedGPUClaimName, Namespace: gpuNamespace},
			Spec: resourcev1.ResourceClaimTemplateSpec{
				Spec: resourcev1.ResourceClaimSpec{
					Devices: resourcev1.DeviceClaim{
						Requests: []resourcev1.DeviceRequest{{
							Name: "gpu",
							Exactly: &resourcev1.ExactDeviceRequest{
								DeviceClassName: gpuDriverName,
								Selectors: []resourcev1.DeviceSelector{{
									CEL: &resourcev1.CELDeviceSelector{
										Expression: fmt.Sprintf(`device.attributes["resource.kubernetes.io"].pciBusID == %q`, *pciBusID),
									},
								}},
							},
						}},
					},
				},
			},
		}
		_, err = f.ClientSet.ResourceV1().ResourceClaimTemplates(gpuNamespace).Create(ctx, template, metav1.CreateOptions{})
		gomega.Expect(err).To(gomega.BeNil(), "creating ResourceClaimTemplate")
	})

	// The test pods run until deleted. They must be gone before the outer AfterEach removes the
	// namespace, and with it the plugin: without the plugin the kubelet cannot unprepare their
	// claims, and the pods and the namespace would stay in Terminating forever.
	ginkgo.AfterEach(func(ctx context.Context) {
		ginkgo.By("deleting the shared GPU pods while the plugin is still running")
		pods, err := f.ClientSet.CoreV1().Pods(gpuNamespace).List(ctx, metav1.ListOptions{})
		gomega.Expect(err).To(gomega.BeNil(), "listing pods")
		for _, pod := range pods.Items {
			if !strings.HasPrefix(pod.Name, sharedGPUPodPrefix) {
				continue
			}
			err := f.ClientSet.CoreV1().Pods(gpuNamespace).Delete(ctx, pod.Name, *metav1.NewDeleteOptions(0))
			gomega.Expect(err).To(gomega.Or(gomega.BeNil(), gomega.WithTransform(apierrors.IsNotFound, gomega.BeTrue())), "deleting pod %s", pod.Name)
			err = e2epod.WaitForPodNotFoundInNamespace(ctx, f.ClientSet, pod.Name, gpuNamespace, 120*time.Second)
			gomega.Expect(err).To(gomega.BeNil(), "pod %s was not deleted", pod.Name)
		}
	})

	ginkgo.It(fmt.Sprintf("runs %d pods on one GPU and leaves the next one pending", sharesPerGPU), func(ctx context.Context) {
		podNames := make([]string, sharesPerGPU+1)
		for i := range podNames {
			podNames[i] = fmt.Sprintf("%s%d", sharedGPUPodPrefix, i)
		}

		ginkgo.By(fmt.Sprintf("deploying %d pods each requesting one share of the GPU", len(podNames)))
		for _, name := range podNames {
			_, err := f.ClientSet.CoreV1().Pods(gpuNamespace).Create(ctx, sharedGPUPod(name, sharedDeviceNode), metav1.CreateOptions{})
			gomega.Expect(err).To(gomega.BeNil(), "creating pod "+name)
		}

		ginkgo.By(fmt.Sprintf("waiting for the first %d pods to run", sharesPerGPU))
		for _, name := range podNames[:sharesPerGPU] {
			err := e2epod.WaitTimeoutForPodRunningInNamespace(ctx, f.ClientSet, name, gpuNamespace, 120*time.Second)
			gomega.Expect(err).To(gomega.BeNil(), "pod %s did not start", name)
		}

		ginkgo.By("checking that the running pods hold distinct shares of the same GPU")
		claims, err := f.ClientSet.ResourceV1().ResourceClaims(gpuNamespace).List(ctx, metav1.ListOptions{})
		gomega.Expect(err).To(gomega.BeNil(), "listing ResourceClaims")
		shareIDs := map[types.UID]string{}
		for _, name := range podNames[:sharesPerGPU] {
			result := allocationForPod(claims, name)
			gomega.Expect(result).ToNot(gomega.BeNil(), "no allocated ResourceClaim for pod %s", name)
			gomega.Expect(result.Pool).To(gomega.Equal(sharedDeviceNode), "pod %s got a GPU on a different node", name)
			gomega.Expect(result.Device).To(gomega.Equal(sharedDevice.Name), "pod %s got a different GPU", name)
			gomega.Expect(result.ShareID).ToNot(gomega.BeNil(), "pod %s has no ShareID", name)
			gomega.Expect(shareIDs).ToNot(gomega.HaveKey(*result.ShareID), "pods %s and %s share one ShareID", name, shareIDs[*result.ShareID])
			shareIDs[*result.ShareID] = name
		}

		pendingPod := podNames[sharesPerGPU]
		ginkgo.By("checking that pod " + pendingPod + " stays pending: the GPU has no shares left")
		err = e2epod.WaitForPodNameUnschedulableInNamespace(ctx, f.ClientSet, pendingPod, gpuNamespace)
		gomega.Expect(err).To(gomega.BeNil(), "pod %s should be unschedulable", pendingPod)

		ginkgo.By("deleting pod " + podNames[0] + " to free a share")
		err = f.ClientSet.CoreV1().Pods(gpuNamespace).Delete(ctx, podNames[0], *metav1.NewDeleteOptions(0))
		gomega.Expect(err).To(gomega.BeNil(), "deleting pod %s", podNames[0])
		err = e2epod.WaitForPodNotFoundInNamespace(ctx, f.ClientSet, podNames[0], gpuNamespace, 120*time.Second)
		gomega.Expect(err).To(gomega.BeNil(), "pod %s was not deleted", podNames[0])

		ginkgo.By("waiting for pod " + pendingPod + " to take the freed share")
		err = e2epod.WaitTimeoutForPodRunningInNamespace(ctx, f.ClientSet, pendingPod, gpuNamespace, 120*time.Second)
		gomega.Expect(err).To(gomega.BeNil(), "pod %s did not start after a share was freed", pendingPod)

		ginkgo.By("checking that pod " + podNames[1] + " kept running")
		pod, err := f.ClientSet.CoreV1().Pods(gpuNamespace).Get(ctx, podNames[1], metav1.GetOptions{})
		gomega.Expect(err).To(gomega.BeNil(), "getting pod %s", podNames[1])
		gomega.Expect(pod.Status.Phase).To(gomega.Equal(v1.PodRunning), "pod %s stopped running", podNames[1])
	})
}

// waitForShareableGPU polls the driver's ResourceSlices until one device allows multiple
// allocations with sharesPerGPU shares. It returns that device and the node that published it,
// or an error with the number of published devices when none became shareable in time.
func waitForShareableGPU(ctx context.Context, f *framework.Framework) (*resourcev1.Device, string, int, error) {
	var shareable *resourcev1.Device
	var node string
	var devices int
	err := wait.PollUntilContextTimeout(ctx, 2*time.Second, 60*time.Second, true, func(ctx context.Context) (bool, error) {
		slices, err := f.ClientSet.ResourceV1().ResourceSlices().List(ctx, metav1.ListOptions{
			FieldSelector: "spec.driver=" + gpuDriverName,
		})
		if err != nil {
			return false, err
		}
		devices = 0
		for _, slice := range slices.Items {
			for _, device := range slice.Spec.Devices {
				devices++
				shares, hasShares := device.Capacity["shares"]
				if ptr.Deref(device.AllowMultipleAllocations, false) && hasShares && shares.Value.Value() == sharesPerGPU {
					shareable, node = &device, ptr.Deref(slice.Spec.NodeName, "")
					return true, nil
				}
			}
		}
		return false, nil
	})
	return shareable, node, devices, err
}

// sharedGPUPod returns a long-running pod requesting one share of the GPU through the
// sharedGPUClaimName template. It is pinned to the node so that the same PCI bus ID on another
// node does not match. It fails instead of running if the GPU is not in the container.
func sharedGPUPod(name, node string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: gpuNamespace},
		Spec: v1.PodSpec{
			RestartPolicy: v1.RestartPolicyNever,
			NodeSelector:  map[string]string{v1.LabelHostname: node},
			Containers: []v1.Container{{
				Name:    "with-resource",
				Image:   "registry.k8s.io/e2e-test-images/busybox:1.29-2",
				Command: []string{"sh", "-c", "ls -la /dev/dri/ && sleep " + sharedGPUPodDuration},
				Resources: v1.ResourceRequirements{
					Claims: []v1.ResourceClaim{{Name: "gpu"}},
				},
			}},
			ResourceClaims: []v1.PodResourceClaim{{
				Name:                      "gpu",
				ResourceClaimTemplateName: ptr.To(sharedGPUClaimName),
			}},
		},
	}
}

// allocationForPod returns the first device allocation of the ResourceClaim reserved for the pod,
// or nil if the pod has no allocated claim.
func allocationForPod(claims *resourcev1.ResourceClaimList, podName string) *resourcev1.DeviceRequestAllocationResult {
	for _, claim := range claims.Items {
		for _, reservedFor := range claim.Status.ReservedFor {
			if reservedFor.Resource == "pods" && reservedFor.Name == podName && claim.Status.Allocation != nil &&
				len(claim.Status.Allocation.Devices.Results) > 0 {
				return &claim.Status.Allocation.Devices.Results[0]
			}
		}
	}
	return nil
}
