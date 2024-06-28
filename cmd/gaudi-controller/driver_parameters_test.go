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
	"reflect"
	"strings"
	"testing"

	fakeclient "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gaudi/clientset/versioned/fake"
	intelcrd "github.com/intel/intel-resource-drivers-for-kubernetes/pkg/intel.com/resource/gaudi/v1alpha1/api"
	resourcev1 "k8s.io/api/resource/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
)

const (
	claimParamsName = "test-claim-params"
	classParamsName = "test-class-params"
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
			expectedParamsSpec: intelcrd.DefaultGaudiClassParametersSpec(),
			expectedErrPattern: "",
		},
		{
			name: "wrong parametersRef.APIGroup",
			class: &resourcev1.ResourceClass{
				ParametersRef: &resourcev1.ResourceClassParametersReference{
					APIGroup: "unsupported",
					Kind:     "GaudiClassParameters",
				},
			},
			expectedParamsSpec: nil,
			expectedErrPattern: "incorrect resource-class API group",
		},
		{
			name: "wrong parametersRef.Kind",
			class: &resourcev1.ResourceClass{
				ParametersRef: &resourcev1.ResourceClassParametersReference{
					APIGroup: "gaudi.resource.intel.com/v1alpha1",
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
					APIGroup: "gaudi.resource.intel.com/v1alpha1",
					Kind:     "GaudiClassParameters",
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
					APIGroup: "gaudi.resource.intel.com/v1alpha1",
					Kind:     "GaudiClassParameters",
					Name:     classParamsName,
				},
			},
			expectedParamsSpec: &intelcrd.GaudiClassParametersSpec{},
			expectedErrPattern: "",
		},
	}

	fakeParams := &intelcrd.GaudiClassParameters{
		TypeMeta: metav1.TypeMeta{
			Kind:       "GaudiClassParameters",
			APIVersion: "gaudi.resource.intel.com/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: classParamsName,
		},
		Spec: intelcrd.GaudiClassParametersSpec{},
	}

	fakeDRAClient := fakeclient.NewSimpleClientset()
	driver := createFakeDriver(t, kubefake.NewSimpleClientset(), fakeDRAClient)

	_, err := fakeDRAClient.GaudiV1alpha1().GaudiClassParameters().Create(context.TODO(), fakeParams, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Could not create GaudiClassParameters for test: %v", err)
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
				APIGroup: "gaudi.resource.intel.com/v1alpha1",
				Kind:     "GaudiClaimParameters",
				Name:     claimParamsName,
			},
		},
	}

	testCases := []testCase{
		{
			name:               "successful default params",
			claim:              &resourcev1.ResourceClaim{},
			classParams:        &intelcrd.GaudiClassParametersSpec{},
			expectedParamsSpec: intelcrd.DefaultGaudiClaimParametersSpec(),
			expectedErrPattern: "",
		},
		{
			name: "wrong parametersRef.APIGroup",
			claim: &resourcev1.ResourceClaim{
				Spec: resourcev1.ResourceClaimSpec{
					ParametersRef: &resourcev1.ResourceClaimParametersReference{
						APIGroup: "unsupported",
						Kind:     "GaudiClaimParameters",
					},
				},
			},
			classParams:        &intelcrd.GaudiClassParametersSpec{},
			expectedParamsSpec: nil,
			expectedErrPattern: "incorrect claim spec parameter API group and version",
		},
		{
			name: "wrong parametersRef.Kind",
			claim: &resourcev1.ResourceClaim{
				Spec: resourcev1.ResourceClaimSpec{
					ParametersRef: &resourcev1.ResourceClaimParametersReference{
						APIGroup: "gaudi.resource.intel.com/v1alpha1",
						Kind:     "unsupported",
					},
				},
			},
			classParams:        &intelcrd.GaudiClassParametersSpec{},
			expectedParamsSpec: nil,
			expectedErrPattern: "unsupported ResourceClaimParametersRef Kind",
		},
		{
			name: "missing params object",
			claim: &resourcev1.ResourceClaim{
				Spec: resourcev1.ResourceClaimSpec{
					ParametersRef: &resourcev1.ResourceClaimParametersReference{
						APIGroup: "gaudi.resource.intel.com/v1alpha1",
						Kind:     "GaudiClaimParameters",
						Name:     "missing",
					},
				},
			},
			classParams:        &intelcrd.GaudiClassParametersSpec{},
			expectedParamsSpec: nil,
			expectedErrPattern: "could not get GaudiClaimParameters",
		},
		{
			name:               "successful custom params",
			claim:              goodClaim,
			classParams:        &intelcrd.GaudiClassParametersSpec{},
			expectedParamsSpec: &intelcrd.GaudiClaimParametersSpec{Count: 2},
			expectedErrPattern: "",
		},
	}

	fakeParams := &intelcrd.GaudiClaimParameters{
		TypeMeta: metav1.TypeMeta{
			Kind:       "GaudiClaimParameters",
			APIVersion: "gpu.resource.intel.com/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      claimParamsName,
		},
		Spec: intelcrd.GaudiClaimParametersSpec{Count: 2},
	}

	fakeDRAClient := fakeclient.NewSimpleClientset()
	driver := createFakeDriver(t, kubefake.NewSimpleClientset(), fakeDRAClient)
	_, err := fakeDRAClient.GaudiV1alpha1().GaudiClaimParameters(testNamespace).Create(context.TODO(), fakeParams, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Could not create GaudiClaimParameters for test: %v", err)
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
