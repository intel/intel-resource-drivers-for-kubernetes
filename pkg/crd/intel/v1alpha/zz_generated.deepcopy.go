//go:build !ignore_autogenerated
// +build !ignore_autogenerated

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

// Code generated by deepcopy-gen. DO NOT EDIT.

package v1alpha

import (
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AllocatableGpu) DeepCopyInto(out *AllocatableGpu) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new AllocatableGpu.
func (in *AllocatableGpu) DeepCopy() *AllocatableGpu {
	if in == nil {
		return nil
	}
	out := new(AllocatableGpu)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AllocatedGpu) DeepCopyInto(out *AllocatedGpu) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new AllocatedGpu.
func (in *AllocatedGpu) DeepCopy() *AllocatedGpu {
	if in == nil {
		return nil
	}
	out := new(AllocatedGpu)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in AllocatedGpus) DeepCopyInto(out *AllocatedGpus) {
	{
		in := &in
		*out = make(AllocatedGpus, len(*in))
		copy(*out, *in)
		return
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new AllocatedGpus.
func (in AllocatedGpus) DeepCopy() AllocatedGpus {
	if in == nil {
		return nil
	}
	out := new(AllocatedGpus)
	in.DeepCopyInto(out)
	return *out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DeviceClassParameters) DeepCopyInto(out *DeviceClassParameters) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DeviceClassParameters.
func (in *DeviceClassParameters) DeepCopy() *DeviceClassParameters {
	if in == nil {
		return nil
	}
	out := new(DeviceClassParameters)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *DeviceClassParameters) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DeviceClassParametersList) DeepCopyInto(out *DeviceClassParametersList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]DeviceClassParameters, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DeviceClassParametersList.
func (in *DeviceClassParametersList) DeepCopy() *DeviceClassParametersList {
	if in == nil {
		return nil
	}
	out := new(DeviceClassParametersList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *DeviceClassParametersList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DeviceClassParametersSpec) DeepCopyInto(out *DeviceClassParametersSpec) {
	*out = *in
	if in.DeviceSelector != nil {
		in, out := &in.DeviceSelector, &out.DeviceSelector
		*out = make([]DeviceSelector, len(*in))
		copy(*out, *in)
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DeviceClassParametersSpec.
func (in *DeviceClassParametersSpec) DeepCopy() *DeviceClassParametersSpec {
	if in == nil {
		return nil
	}
	out := new(DeviceClassParametersSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DeviceSelector) DeepCopyInto(out *DeviceSelector) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DeviceSelector.
func (in *DeviceSelector) DeepCopy() *DeviceSelector {
	if in == nil {
		return nil
	}
	out := new(DeviceSelector)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GpuAllocationState) DeepCopyInto(out *GpuAllocationState) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GpuAllocationState.
func (in *GpuAllocationState) DeepCopy() *GpuAllocationState {
	if in == nil {
		return nil
	}
	out := new(GpuAllocationState)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *GpuAllocationState) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GpuAllocationStateList) DeepCopyInto(out *GpuAllocationStateList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]GpuAllocationState, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GpuAllocationStateList.
func (in *GpuAllocationStateList) DeepCopy() *GpuAllocationStateList {
	if in == nil {
		return nil
	}
	out := new(GpuAllocationStateList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *GpuAllocationStateList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GpuAllocationStateSpec) DeepCopyInto(out *GpuAllocationStateSpec) {
	*out = *in
	if in.Allocatable != nil {
		in, out := &in.Allocatable, &out.Allocatable
		*out = make(map[string]AllocatableGpu, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.Prepared != nil {
		in, out := &in.Prepared, &out.Prepared
		*out = make(map[string]AllocatedGpus, len(*in))
		for key, val := range *in {
			var outVal []AllocatedGpu
			if val == nil {
				(*out)[key] = nil
			} else {
				in, out := &val, &outVal
				*out = make(AllocatedGpus, len(*in))
				copy(*out, *in)
			}
			(*out)[key] = outVal
		}
	}
	if in.ResourceClaimAllocations != nil {
		in, out := &in.ResourceClaimAllocations, &out.ResourceClaimAllocations
		*out = make(map[string]ResourceClaimAllocation, len(*in))
		for key, val := range *in {
			(*out)[key] = *val.DeepCopy()
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GpuAllocationStateSpec.
func (in *GpuAllocationStateSpec) DeepCopy() *GpuAllocationStateSpec {
	if in == nil {
		return nil
	}
	out := new(GpuAllocationStateSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GpuClaimParameters) DeepCopyInto(out *GpuClaimParameters) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	out.Spec = in.Spec
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GpuClaimParameters.
func (in *GpuClaimParameters) DeepCopy() *GpuClaimParameters {
	if in == nil {
		return nil
	}
	out := new(GpuClaimParameters)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *GpuClaimParameters) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GpuClaimParametersList) DeepCopyInto(out *GpuClaimParametersList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]GpuClaimParameters, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GpuClaimParametersList.
func (in *GpuClaimParametersList) DeepCopy() *GpuClaimParametersList {
	if in == nil {
		return nil
	}
	out := new(GpuClaimParametersList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *GpuClaimParametersList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GpuClaimParametersSpec) DeepCopyInto(out *GpuClaimParametersSpec) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GpuClaimParametersSpec.
func (in *GpuClaimParametersSpec) DeepCopy() *GpuClaimParametersSpec {
	if in == nil {
		return nil
	}
	out := new(GpuClaimParametersSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ResourceClaimAllocation) DeepCopyInto(out *ResourceClaimAllocation) {
	*out = *in
	out.Request = in.Request
	if in.Gpus != nil {
		in, out := &in.Gpus, &out.Gpus
		*out = make(AllocatedGpus, len(*in))
		copy(*out, *in)
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ResourceClaimAllocation.
func (in *ResourceClaimAllocation) DeepCopy() *ResourceClaimAllocation {
	if in == nil {
		return nil
	}
	out := new(ResourceClaimAllocation)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in ResourceClaimAllocations) DeepCopyInto(out *ResourceClaimAllocations) {
	{
		in := &in
		*out = make(ResourceClaimAllocations, len(*in))
		for key, val := range *in {
			(*out)[key] = *val.DeepCopy()
		}
		return
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ResourceClaimAllocations.
func (in ResourceClaimAllocations) DeepCopy() ResourceClaimAllocations {
	if in == nil {
		return nil
	}
	out := new(ResourceClaimAllocations)
	in.DeepCopyInto(out)
	return *out
}