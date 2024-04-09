//go:build !ignore_autogenerated

/*
Copyright 2020 The Crossplane Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Code generated by controller-gen. DO NOT EDIT.

package v1alpha1

import (
	"github.com/crossplane/crossplane-runtime/apis/common/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AbortIncompleteMultipartUpload) DeepCopyInto(out *AbortIncompleteMultipartUpload) {
	*out = *in
	if in.DaysAfterInitiation != nil {
		in, out := &in.DaysAfterInitiation, &out.DaysAfterInitiation
		*out = new(int32)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new AbortIncompleteMultipartUpload.
func (in *AbortIncompleteMultipartUpload) DeepCopy() *AbortIncompleteMultipartUpload {
	if in == nil {
		return nil
	}
	out := new(AbortIncompleteMultipartUpload)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AccessControlPolicy) DeepCopyInto(out *AccessControlPolicy) {
	*out = *in
	if in.Grants != nil {
		in, out := &in.Grants, &out.Grants
		*out = make([]Grant, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.Owner != nil {
		in, out := &in.Owner, &out.Owner
		*out = new(Owner)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new AccessControlPolicy.
func (in *AccessControlPolicy) DeepCopy() *AccessControlPolicy {
	if in == nil {
		return nil
	}
	out := new(AccessControlPolicy)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *BackendInfo) DeepCopyInto(out *BackendInfo) {
	*out = *in
	in.BucketCondition.DeepCopyInto(&out.BucketCondition)
	if in.LifecycleConfigurationCondition != nil {
		in, out := &in.LifecycleConfigurationCondition, &out.LifecycleConfigurationCondition
		*out = new(v1.Condition)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new BackendInfo.
func (in *BackendInfo) DeepCopy() *BackendInfo {
	if in == nil {
		return nil
	}
	out := new(BackendInfo)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in Backends) DeepCopyInto(out *Backends) {
	{
		in := &in
		*out = make(Backends, len(*in))
		for key, val := range *in {
			var outVal *BackendInfo
			if val == nil {
				(*out)[key] = nil
			} else {
				inVal := (*in)[key]
				in, out := &inVal, &outVal
				*out = new(BackendInfo)
				(*in).DeepCopyInto(*out)
			}
			(*out)[key] = outVal
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Backends.
func (in Backends) DeepCopy() Backends {
	if in == nil {
		return nil
	}
	out := new(Backends)
	in.DeepCopyInto(out)
	return *out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Bucket) DeepCopyInto(out *Bucket) {
	*out = *in
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Bucket.
func (in *Bucket) DeepCopy() *Bucket {
	if in == nil {
		return nil
	}
	out := new(Bucket)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *Bucket) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *BucketLifecycleConfiguration) DeepCopyInto(out *BucketLifecycleConfiguration) {
	*out = *in
	if in.Rules != nil {
		in, out := &in.Rules, &out.Rules
		*out = make([]LifecycleRule, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new BucketLifecycleConfiguration.
func (in *BucketLifecycleConfiguration) DeepCopy() *BucketLifecycleConfiguration {
	if in == nil {
		return nil
	}
	out := new(BucketLifecycleConfiguration)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *BucketList) DeepCopyInto(out *BucketList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]Bucket, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new BucketList.
func (in *BucketList) DeepCopy() *BucketList {
	if in == nil {
		return nil
	}
	out := new(BucketList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *BucketList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *BucketObservation) DeepCopyInto(out *BucketObservation) {
	*out = *in
	if in.Backends != nil {
		in, out := &in.Backends, &out.Backends
		*out = make(Backends, len(*in))
		for key, val := range *in {
			var outVal *BackendInfo
			if val == nil {
				(*out)[key] = nil
			} else {
				inVal := (*in)[key]
				in, out := &inVal, &outVal
				*out = new(BackendInfo)
				(*in).DeepCopyInto(*out)
			}
			(*out)[key] = outVal
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new BucketObservation.
func (in *BucketObservation) DeepCopy() *BucketObservation {
	if in == nil {
		return nil
	}
	out := new(BucketObservation)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *BucketParameters) DeepCopyInto(out *BucketParameters) {
	*out = *in
	if in.ACL != nil {
		in, out := &in.ACL, &out.ACL
		*out = new(string)
		**out = **in
	}
	if in.AccessControlPolicy != nil {
		in, out := &in.AccessControlPolicy, &out.AccessControlPolicy
		*out = new(AccessControlPolicy)
		(*in).DeepCopyInto(*out)
	}
	if in.GrantFullControl != nil {
		in, out := &in.GrantFullControl, &out.GrantFullControl
		*out = new(string)
		**out = **in
	}
	if in.GrantRead != nil {
		in, out := &in.GrantRead, &out.GrantRead
		*out = new(string)
		**out = **in
	}
	if in.GrantReadACP != nil {
		in, out := &in.GrantReadACP, &out.GrantReadACP
		*out = new(string)
		**out = **in
	}
	if in.GrantWrite != nil {
		in, out := &in.GrantWrite, &out.GrantWrite
		*out = new(string)
		**out = **in
	}
	if in.GrantWriteACP != nil {
		in, out := &in.GrantWriteACP, &out.GrantWriteACP
		*out = new(string)
		**out = **in
	}
	if in.ObjectLockEnabledForBucket != nil {
		in, out := &in.ObjectLockEnabledForBucket, &out.ObjectLockEnabledForBucket
		*out = new(bool)
		**out = **in
	}
	if in.ObjectOwnership != nil {
		in, out := &in.ObjectOwnership, &out.ObjectOwnership
		*out = new(string)
		**out = **in
	}
	if in.LifecycleConfiguration != nil {
		in, out := &in.LifecycleConfiguration, &out.LifecycleConfiguration
		*out = new(BucketLifecycleConfiguration)
		(*in).DeepCopyInto(*out)
	}
	if in.AssumeRoleTags != nil {
		in, out := &in.AssumeRoleTags, &out.AssumeRoleTags
		*out = make([]Tag, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new BucketParameters.
func (in *BucketParameters) DeepCopy() *BucketParameters {
	if in == nil {
		return nil
	}
	out := new(BucketParameters)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *BucketSpec) DeepCopyInto(out *BucketSpec) {
	*out = *in
	if in.Providers != nil {
		in, out := &in.Providers, &out.Providers
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	in.ForProvider.DeepCopyInto(&out.ForProvider)
	in.ResourceSpec.DeepCopyInto(&out.ResourceSpec)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new BucketSpec.
func (in *BucketSpec) DeepCopy() *BucketSpec {
	if in == nil {
		return nil
	}
	out := new(BucketSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *BucketStatus) DeepCopyInto(out *BucketStatus) {
	*out = *in
	in.AtProvider.DeepCopyInto(&out.AtProvider)
	in.ResourceStatus.DeepCopyInto(&out.ResourceStatus)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new BucketStatus.
func (in *BucketStatus) DeepCopy() *BucketStatus {
	if in == nil {
		return nil
	}
	out := new(BucketStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Grant) DeepCopyInto(out *Grant) {
	*out = *in
	if in.Grantee != nil {
		in, out := &in.Grantee, &out.Grantee
		*out = new(Grantee)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Grant.
func (in *Grant) DeepCopy() *Grant {
	if in == nil {
		return nil
	}
	out := new(Grant)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Grantee) DeepCopyInto(out *Grantee) {
	*out = *in
	if in.DisplayName != nil {
		in, out := &in.DisplayName, &out.DisplayName
		*out = new(string)
		**out = **in
	}
	if in.EmailAddress != nil {
		in, out := &in.EmailAddress, &out.EmailAddress
		*out = new(string)
		**out = **in
	}
	if in.ID != nil {
		in, out := &in.ID, &out.ID
		*out = new(string)
		**out = **in
	}
	if in.URI != nil {
		in, out := &in.URI, &out.URI
		*out = new(string)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Grantee.
func (in *Grantee) DeepCopy() *Grantee {
	if in == nil {
		return nil
	}
	out := new(Grantee)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *LifecycleExpiration) DeepCopyInto(out *LifecycleExpiration) {
	*out = *in
	if in.Date != nil {
		in, out := &in.Date, &out.Date
		*out = (*in).DeepCopy()
	}
	if in.Days != nil {
		in, out := &in.Days, &out.Days
		*out = new(int32)
		**out = **in
	}
	if in.ExpiredObjectDeleteMarker != nil {
		in, out := &in.ExpiredObjectDeleteMarker, &out.ExpiredObjectDeleteMarker
		*out = new(bool)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new LifecycleExpiration.
func (in *LifecycleExpiration) DeepCopy() *LifecycleExpiration {
	if in == nil {
		return nil
	}
	out := new(LifecycleExpiration)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *LifecycleRule) DeepCopyInto(out *LifecycleRule) {
	*out = *in
	if in.AbortIncompleteMultipartUpload != nil {
		in, out := &in.AbortIncompleteMultipartUpload, &out.AbortIncompleteMultipartUpload
		*out = new(AbortIncompleteMultipartUpload)
		(*in).DeepCopyInto(*out)
	}
	if in.Expiration != nil {
		in, out := &in.Expiration, &out.Expiration
		*out = new(LifecycleExpiration)
		(*in).DeepCopyInto(*out)
	}
	if in.Filter != nil {
		in, out := &in.Filter, &out.Filter
		*out = new(LifecycleRuleFilter)
		(*in).DeepCopyInto(*out)
	}
	if in.ID != nil {
		in, out := &in.ID, &out.ID
		*out = new(string)
		**out = **in
	}
	if in.NoncurrentVersionExpiration != nil {
		in, out := &in.NoncurrentVersionExpiration, &out.NoncurrentVersionExpiration
		*out = new(NoncurrentVersionExpiration)
		(*in).DeepCopyInto(*out)
	}
	if in.NoncurrentVersionTransitions != nil {
		in, out := &in.NoncurrentVersionTransitions, &out.NoncurrentVersionTransitions
		*out = make([]NoncurrentVersionTransition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.Transitions != nil {
		in, out := &in.Transitions, &out.Transitions
		*out = make([]Transition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.Prefix != nil {
		in, out := &in.Prefix, &out.Prefix
		*out = new(string)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new LifecycleRule.
func (in *LifecycleRule) DeepCopy() *LifecycleRule {
	if in == nil {
		return nil
	}
	out := new(LifecycleRule)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *LifecycleRuleAndOperator) DeepCopyInto(out *LifecycleRuleAndOperator) {
	*out = *in
	if in.Prefix != nil {
		in, out := &in.Prefix, &out.Prefix
		*out = new(string)
		**out = **in
	}
	if in.Tags != nil {
		in, out := &in.Tags, &out.Tags
		*out = make([]Tag, len(*in))
		copy(*out, *in)
	}
	if in.ObjectSizeGreaterThan != nil {
		in, out := &in.ObjectSizeGreaterThan, &out.ObjectSizeGreaterThan
		*out = new(int64)
		**out = **in
	}
	if in.ObjectSizeLessThan != nil {
		in, out := &in.ObjectSizeLessThan, &out.ObjectSizeLessThan
		*out = new(int64)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new LifecycleRuleAndOperator.
func (in *LifecycleRuleAndOperator) DeepCopy() *LifecycleRuleAndOperator {
	if in == nil {
		return nil
	}
	out := new(LifecycleRuleAndOperator)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *LifecycleRuleFilter) DeepCopyInto(out *LifecycleRuleFilter) {
	*out = *in
	if in.And != nil {
		in, out := &in.And, &out.And
		*out = new(LifecycleRuleAndOperator)
		(*in).DeepCopyInto(*out)
	}
	if in.Prefix != nil {
		in, out := &in.Prefix, &out.Prefix
		*out = new(string)
		**out = **in
	}
	if in.Tag != nil {
		in, out := &in.Tag, &out.Tag
		*out = new(Tag)
		**out = **in
	}
	if in.ObjectSizeGreaterThan != nil {
		in, out := &in.ObjectSizeGreaterThan, &out.ObjectSizeGreaterThan
		*out = new(int64)
		**out = **in
	}
	if in.ObjectSizeLessThan != nil {
		in, out := &in.ObjectSizeLessThan, &out.ObjectSizeLessThan
		*out = new(int64)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new LifecycleRuleFilter.
func (in *LifecycleRuleFilter) DeepCopy() *LifecycleRuleFilter {
	if in == nil {
		return nil
	}
	out := new(LifecycleRuleFilter)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *NoncurrentVersionExpiration) DeepCopyInto(out *NoncurrentVersionExpiration) {
	*out = *in
	if in.NoncurrentDays != nil {
		in, out := &in.NoncurrentDays, &out.NoncurrentDays
		*out = new(int32)
		**out = **in
	}
	if in.NewerNoncurrentVersions != nil {
		in, out := &in.NewerNoncurrentVersions, &out.NewerNoncurrentVersions
		*out = new(int32)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new NoncurrentVersionExpiration.
func (in *NoncurrentVersionExpiration) DeepCopy() *NoncurrentVersionExpiration {
	if in == nil {
		return nil
	}
	out := new(NoncurrentVersionExpiration)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *NoncurrentVersionTransition) DeepCopyInto(out *NoncurrentVersionTransition) {
	*out = *in
	if in.NoncurrentDays != nil {
		in, out := &in.NoncurrentDays, &out.NoncurrentDays
		*out = new(int32)
		**out = **in
	}
	if in.NewerNoncurrentVersions != nil {
		in, out := &in.NewerNoncurrentVersions, &out.NewerNoncurrentVersions
		*out = new(int32)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new NoncurrentVersionTransition.
func (in *NoncurrentVersionTransition) DeepCopy() *NoncurrentVersionTransition {
	if in == nil {
		return nil
	}
	out := new(NoncurrentVersionTransition)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Owner) DeepCopyInto(out *Owner) {
	*out = *in
	if in.DisplayName != nil {
		in, out := &in.DisplayName, &out.DisplayName
		*out = new(string)
		**out = **in
	}
	if in.ID != nil {
		in, out := &in.ID, &out.ID
		*out = new(string)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Owner.
func (in *Owner) DeepCopy() *Owner {
	if in == nil {
		return nil
	}
	out := new(Owner)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Tag) DeepCopyInto(out *Tag) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Tag.
func (in *Tag) DeepCopy() *Tag {
	if in == nil {
		return nil
	}
	out := new(Tag)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Transition) DeepCopyInto(out *Transition) {
	*out = *in
	if in.Date != nil {
		in, out := &in.Date, &out.Date
		*out = (*in).DeepCopy()
	}
	if in.Days != nil {
		in, out := &in.Days, &out.Days
		*out = new(int32)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Transition.
func (in *Transition) DeepCopy() *Transition {
	if in == nil {
		return nil
	}
	out := new(Transition)
	in.DeepCopyInto(out)
	return out
}
