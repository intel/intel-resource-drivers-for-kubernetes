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
	"encoding/json"
	"fmt"
	"strings"

	resourceapi "k8s.io/api/resource/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"

	intelcrd "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gaudi/v1alpha1/api"
)

func startClaimParametersGenerator(ctx context.Context, config *configType) error {
	// Create a new dynamic client
	dynamicClient, err := dynamic.NewForConfig(config.csconfig)
	if err != nil {
		return fmt.Errorf("error creating dynamic client: %v", err)
	}

	klog.Info("Starting ResourceClaimParamaters generator")

	// Watch GaudiClaimParameters objects
	gaudiClaimParametersInformer := newGaudiClaimParametersInformer(ctx, dynamicClient)
	if _, err := gaudiClaimParametersInformer.AddEventHandler(newGaudiClaimParametersHandler(ctx, config.clientsets.core)); err != nil {
		return fmt.Errorf("error creating GaudiClaimParameters informer: error adding event handler: %v", err)
	}

	// Start informer
	go gaudiClaimParametersInformer.Run(ctx.Done())

	return nil
}

func newGaudiClaimParametersInformer(ctx context.Context, dynamicClient dynamic.Interface) cache.SharedIndexInformer {
	resource := schema.GroupVersionResource{
		Group:    intelcrd.APIGroupName,
		Version:  intelcrd.APIVersion,
		Resource: strings.ToLower(intelcrd.GaudiClaimParametersKind),
	}

	informer := cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return dynamicClient.Resource(resource).List(ctx, metav1.ListOptions{})
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return dynamicClient.Resource(resource).Watch(ctx, metav1.ListOptions{})
			},
		},
		&unstructured.Unstructured{},
		0, // resyncPeriod
		cache.Indexers{},
	)

	return informer
}

func newGaudiClaimParametersHandler(ctx context.Context, clientset kubernetes.Interface) cache.ResourceEventHandler {
	resourceUpdateHandlerFunction := func(oldObj any, newObj any) {
		unstructured, ok := newObj.(*unstructured.Unstructured)
		if !ok {
			klog.Error("error converting argument object into unstructured.Unstructured")
			return
		}

		var gaudiClaimParameters intelcrd.GaudiClaimParameters
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructured.Object, &gaudiClaimParameters)
		if err != nil {
			klog.Errorf("error converting *unstructured.Unstructured to GaudiClaimParameters: %v", err)
			return
		}

		if err := createOrUpdateResourceClaimParameters(ctx, clientset, &gaudiClaimParameters); err != nil {
			klog.Errorf("error updating ResourceClaimParameters: %v", err)
			return
		}
	}

	resourceAddHandlerFunction := func(newObj any) {
		resourceUpdateHandlerFunction(nil, newObj)
	}

	return cache.ResourceEventHandlerFuncs{
		AddFunc:    resourceAddHandlerFunction,
		UpdateFunc: resourceUpdateHandlerFunction,
	}
}

func makeResourceClaimParameters(gaudiClaimParameters *intelcrd.GaudiClaimParameters) (*resourceapi.ResourceClaimParameters, error) {
	rawSpec, err := json.Marshal(gaudiClaimParameters.Spec)
	if err != nil {
		return nil, fmt.Errorf("error marshaling GpuClaimParamaters to JSON: %w", err)
	}

	resourceCount := gaudiClaimParameters.Spec.Count

	shareable := false
	selector := "true"

	var resourceRequests []resourceapi.ResourceRequest
	for i := uint64(0); i < resourceCount; i++ {
		request := resourceapi.ResourceRequest{
			ResourceRequestModel: resourceapi.ResourceRequestModel{
				NamedResources: &resourceapi.NamedResourcesRequest{
					Selector: selector,
				},
			},
		}
		resourceRequests = append(resourceRequests, request)
	}

	resourceClaimParameters := &resourceapi.ResourceClaimParameters{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "resource-claim-parameters-",
			Namespace:    gaudiClaimParameters.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         gaudiClaimParameters.APIVersion,
					Kind:               gaudiClaimParameters.Kind,
					Name:               gaudiClaimParameters.Name,
					UID:                gaudiClaimParameters.UID,
					BlockOwnerDeletion: ptr.To(true),
				},
			},
		},
		GeneratedFrom: &resourceapi.ResourceClaimParametersReference{
			APIGroup: intelcrd.APIGroupName,
			Kind:     gaudiClaimParameters.Kind,
			Name:     gaudiClaimParameters.Name,
		},
		DriverRequests: []resourceapi.DriverRequests{
			{
				DriverName:       intelcrd.APIGroupName,
				VendorParameters: runtime.RawExtension{Raw: rawSpec},
				Requests:         resourceRequests,
			},
		},
		Shareable: shareable,
	}

	return resourceClaimParameters, nil
}

func createOrUpdateResourceClaimParameters(ctx context.Context, clientset kubernetes.Interface, gaudiClaimParameters *intelcrd.GaudiClaimParameters) error {
	namespace := gaudiClaimParameters.Namespace

	// Build a new ResourceClaimParameters object from the incoming GaudiClaimParameters object
	resourceClaimParameters, err := makeResourceClaimParameters(gaudiClaimParameters)
	if err != nil {
		return fmt.Errorf("error building new ResourceClaimParameters object: %w", err)
	}

	// Get a list of existing ResourceClaimParameters in the same namespace as the incoming GaudiClaimParameters
	existing, err := clientset.ResourceV1alpha2().ResourceClaimParameters(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("error listing existing ResourceClaimParameters: %w", err)
	}

	// If there is an existing ResourceClaimParameters generated from the incoming GaudiClaimParameters object, then update it
	for _, item := range existing.Items {
		if (item.GeneratedFrom.APIGroup == intelcrd.APIGroupName) &&
			(item.GeneratedFrom.Kind == gaudiClaimParameters.Kind) &&
			(item.GeneratedFrom.Name == gaudiClaimParameters.Name) {
			klog.Infof("ResourceClaimParameters already exists for GaudiClaimParameters %s/%s, updating it", namespace, gaudiClaimParameters.Name)

			// Copy the matching ResourceClaimParameters metadata into the new ResourceClaimParameters object before updating it
			resourceClaimParameters.ObjectMeta = *item.ObjectMeta.DeepCopy()

			_, err = clientset.ResourceV1alpha2().ResourceClaimParameters(namespace).Update(ctx, resourceClaimParameters, metav1.UpdateOptions{})
			if err != nil {
				return fmt.Errorf("error updating ResourceClaimParameters object: %w", err)
			}

			return nil
		}
	}

	// Create a new ResourceClaimParameters object
	_, err = clientset.ResourceV1alpha2().ResourceClaimParameters(namespace).Create(ctx, resourceClaimParameters, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("error creating ResourceClaimParameters: %w", err)
	}

	klog.Infof("Created ResourceClaimParameters %s/%s", namespace, gaudiClaimParameters.Name)
	return nil
}
