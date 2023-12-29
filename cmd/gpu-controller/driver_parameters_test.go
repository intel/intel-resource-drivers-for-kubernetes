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
	"reflect"
	"strings"
	"testing"

	fakeclient "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gpu/clientset/versioned/fake"
	intelcrd "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gpu/v1alpha2/api"
	resourcev1 "k8s.io/api/resource/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
)

const (
	claimParamsName = "test-claim-params"
	classParamsName = "test-class-params"
	testNamespace   = "namespace1"
)

func TestGetClassParameters(t *testing.T) {
	type testCase struct {
		name               string
		class              *resourcev1.ResourceClass
		expectedParamsSpec interface{}
		expectedErrPattern string
	}

	testCases := []testCase{
		{
			name:               "successful default params",
			class:              &resourcev1.ResourceClass{},
			expectedParamsSpec: intelcrd.DefaultGpuClassParametersSpec(),
			expectedErrPattern: "",
		},
		{
			name: "wrong parametersRef.APIGroup",
			class: &resourcev1.ResourceClass{
				ParametersRef: &resourcev1.ResourceClassParametersReference{
					APIGroup: "unsupported",
					Kind:     "GpuClassParameters",
				},
			},
			expectedParamsSpec: nil,
			expectedErrPattern: "incorrect resource-class API group",
		},
		{
			name: "wrong parametersRef.Kind",
			class: &resourcev1.ResourceClass{
				ParametersRef: &resourcev1.ResourceClassParametersReference{
					APIGroup: "gpu.resource.intel.com/v1alpha2",
					Kind:     "unsupported",
				},
			},
			expectedParamsSpec: nil,
			expectedErrPattern: "unsupported ResourceClass.ParametersRef.Kind",
		},
		{
			name: "missing params object",
			class: &resourcev1.ResourceClass{
				ParametersRef: &resourcev1.ResourceClassParametersReference{
					APIGroup: "gpu.resource.intel.com/v1alpha2",
					Kind:     "GpuClassParameters",
					Name:     "missing",
				},
			},
			expectedParamsSpec: nil,
			expectedErrPattern: "not found",
		},
		{
			name: "successful custom params",
			class: &resourcev1.ResourceClass{
				ParametersRef: &resourcev1.ResourceClassParametersReference{
					APIGroup: "gpu.resource.intel.com/v1alpha2",
					Kind:     "GpuClassParameters",
					Name:     classParamsName,
				},
			},
			expectedParamsSpec: &intelcrd.GpuClassParametersSpec{
				Shared: true,
			},
			expectedErrPattern: "",
		},
	}

	fakeParams := &intelcrd.GpuClassParameters{
		TypeMeta: metav1.TypeMeta{
			Kind:       "GpuClassParameters",
			APIVersion: "gpu.resource.intel.com/v1alpha2",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: classParamsName,
		},
		Spec: intelcrd.GpuClassParametersSpec{
			Shared: true,
		},
	}

	fakeDRAClient := fakeclient.NewSimpleClientset()
	driver := createFakeDriver(t, kubefake.NewSimpleClientset(), fakeDRAClient)

	_, err := fakeDRAClient.GpuV1alpha2().GpuClassParameters().Create(context.TODO(), fakeParams, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Could not create GpuClassParameters for test: %v", err)
	}

	for _, testcase := range testCases {

		t.Log(testcase.name)

		classParamsSpec, err := driver.GetClassParameters(context.TODO(), testcase.class)

		if (err != nil && testcase.expectedErrPattern == "") ||
			(err != nil && !strings.Contains(err.Error(), testcase.expectedErrPattern)) ||
			(err == nil && testcase.expectedErrPattern != "") {
			t.Errorf("bad result, expected err pattern: %s, got: %s", testcase.expectedErrPattern, err)
		}

		if !reflect.DeepEqual(testcase.expectedParamsSpec, classParamsSpec) {
			t.Errorf("bad result, expected params: %+v, got: %+v", testcase.expectedParamsSpec, classParamsSpec)
		}
	}
}

func TestGetClaimParameters(t *testing.T) {
	type testCase struct {
		name               string
		claim              *resourcev1.ResourceClaim
		classParams        interface{}
		expectedParamsSpec interface{}
		expectedErrPattern string
	}

	// used for ClaimParameters validation testcases where Claim does not matter
	goodClaim := &resourcev1.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
		},
		Spec: resourcev1.ResourceClaimSpec{
			ParametersRef: &resourcev1.ResourceClaimParametersReference{
				APIGroup: "gpu.resource.intel.com/v1alpha2",
				Kind:     "GpuClaimParameters",
				Name:     claimParamsName,
			},
		},
	}

	testCases := []testCase{
		{
			name:               "successful default params",
			claim:              &resourcev1.ResourceClaim{},
			classParams:        &intelcrd.GpuClassParametersSpec{},
			expectedParamsSpec: intelcrd.DefaultGpuClaimParametersSpec(),
			expectedErrPattern: "",
		},
		{
			name: "wrong parametersRef.APIGroup",
			claim: &resourcev1.ResourceClaim{
				Spec: resourcev1.ResourceClaimSpec{
					ParametersRef: &resourcev1.ResourceClaimParametersReference{
						APIGroup: "unsupported",
						Kind:     "GpuClaimParameters",
					},
				},
			},
			classParams:        &intelcrd.GpuClassParametersSpec{},
			expectedParamsSpec: nil,
			expectedErrPattern: "incorrect claim spec parameter API group and version",
		},
		{
			name: "wrong parametersRef.Kind",
			claim: &resourcev1.ResourceClaim{
				Spec: resourcev1.ResourceClaimSpec{
					ParametersRef: &resourcev1.ResourceClaimParametersReference{
						APIGroup: "gpu.resource.intel.com/v1alpha2",
						Kind:     "unsupported",
					},
				},
			},
			classParams:        &intelcrd.GpuClassParametersSpec{},
			expectedParamsSpec: nil,
			expectedErrPattern: "unsupported ResourceClaimParametersRef Kind",
		},
		{
			name: "missing params object",
			claim: &resourcev1.ResourceClaim{
				Spec: resourcev1.ResourceClaimSpec{
					ParametersRef: &resourcev1.ResourceClaimParametersReference{
						APIGroup: "gpu.resource.intel.com/v1alpha2",
						Kind:     "GpuClaimParameters",
						Name:     "missing",
					},
				},
			},
			classParams:        &intelcrd.GpuClassParametersSpec{},
			expectedParamsSpec: nil,
			expectedErrPattern: "could not get GpuClaimParameters",
		},
		{
			name:        "successful custom params",
			claim:       goodClaim,
			classParams: &intelcrd.GpuClassParametersSpec{},
			expectedParamsSpec: &intelcrd.GpuClaimParametersSpec{
				Count: 2,
				Type:  "vf",
			},
			expectedErrPattern: "",
		},
	}

	fakeParams := &intelcrd.GpuClaimParameters{
		TypeMeta: metav1.TypeMeta{
			Kind:       "GpuClaimParameters",
			APIVersion: "gpu.resource.intel.com/v1alpha2",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      claimParamsName,
		},
		Spec: intelcrd.GpuClaimParametersSpec{
			Count: 2,
			Type:  "vf",
		},
	}

	fakeDRAClient := fakeclient.NewSimpleClientset()
	driver := createFakeDriver(t, kubefake.NewSimpleClientset(), fakeDRAClient)
	_, err := fakeDRAClient.GpuV1alpha2().GpuClaimParameters(testNamespace).Create(context.TODO(), fakeParams, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Could not create GpuClaimParameters for test: %v", err)
	}

	for _, testcase := range testCases {

		t.Log(testcase.name)

		claimParamsSpec, err := driver.GetClaimParameters(context.TODO(), testcase.claim, &resourcev1.ResourceClass{}, testcase.classParams)

		if (err != nil && testcase.expectedErrPattern == "") ||
			(err != nil && !strings.Contains(err.Error(), testcase.expectedErrPattern)) ||
			(err == nil && testcase.expectedErrPattern != "") {
			t.Errorf("bad result, expected err pattern: %s, got: %s", testcase.expectedErrPattern, err)
		}

		if !reflect.DeepEqual(testcase.expectedParamsSpec, claimParamsSpec) {
			t.Errorf("bad result, expected params: %+v, got: %+v", testcase.expectedParamsSpec, claimParamsSpec)
		}
	}
}

func TestValidateClaimParameters(t *testing.T) {
	type testCase struct {
		name               string
		claimParams        *intelcrd.GpuClaimParametersSpec
		classParams        interface{}
		expectedErrPattern string
	}

	testCases := []testCase{
		{
			name:               "validation err class parameters",
			claimParams:        &intelcrd.GpuClaimParametersSpec{},
			classParams:        &intelcrd.GpuClaimParametersSpec{Count: 2},
			expectedErrPattern: "unsupported ParametersRef type",
		},
		{
			name:               "validation err monitor vf",
			claimParams:        &intelcrd.GpuClaimParametersSpec{Type: "vf"},
			classParams:        &intelcrd.GpuClassParametersSpec{Monitor: true},
			expectedErrPattern: "unsupported monitor type",
		},
		{
			name:               "validation err monitor any",
			claimParams:        &intelcrd.GpuClaimParametersSpec{Type: "any"},
			classParams:        &intelcrd.GpuClassParametersSpec{Monitor: true},
			expectedErrPattern: "unsupported monitor type",
		},
		{
			name:               "validation err millicores any device type",
			claimParams:        &intelcrd.GpuClaimParametersSpec{Type: "any", Millicores: 100},
			classParams:        &intelcrd.GpuClassParametersSpec{Shared: true},
			expectedErrPattern: "millicores can be used either with 'gpu' type and shared ResourceClass or with 'vf' type and non-shared ResourceClass",
		},
		{
			name:               "validation err non-shared millicores",
			claimParams:        &intelcrd.GpuClaimParametersSpec{Type: "gpu", Millicores: 100},
			classParams:        &intelcrd.GpuClassParametersSpec{Shared: false},
			expectedErrPattern: "millicores can be used either with 'gpu' type and shared ResourceClass or with 'vf' type and non-shared ResourceClass",
		},
	}

	for _, testcase := range testCases {

		t.Log(testcase.name)

		err := validateGpuClaimParameters(testcase.claimParams, testcase.classParams)

		if (err != nil && testcase.expectedErrPattern == "") ||
			(err != nil && !strings.Contains(err.Error(), testcase.expectedErrPattern)) ||
			(err == nil && testcase.expectedErrPattern != "") {
			t.Errorf("bad result, expected err pattern: %s, got: %s", testcase.expectedErrPattern, err)
		}
	}
}
