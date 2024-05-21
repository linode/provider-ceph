/*
Copyright 2022 The Crossplane Authors.

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

package v1alpha1

import (
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
)

// BucketParameters are the configurable fields of a Bucket.
type BucketParameters struct {
	// The canned ACL to apply to the bucket.
	// +kubebuilder:validation:Enum=private;public-read;public-read-write;authenticated-read
	ACL *string `json:"acl,omitempty"`

	// Contains the elements that set the ACL permissions for an object per grantee.
	AccessControlPolicy *AccessControlPolicy `json:"accessControlPolicy,omitempty"`

	// Allows grantee the read, write, read ACP, and write ACP permissions on the
	// bucket.
	GrantFullControl *string `json:"grantFullControl,omitempty"`

	// Allows grantee to list the objects in the bucket.
	GrantRead *string `json:"grantRead,omitempty"`

	// Allows grantee to read the bucket ACL.
	GrantReadACP *string `json:"grantReadACP,omitempty"`

	// Allows grantee to create new objects in the bucket.
	//
	// For the bucket and object owners of existing objects, also allows deletions
	// and overwrites of those objects.
	GrantWrite *string `json:"grantWrite,omitempty"`

	// Allows grantee to write the ACL for the applicable bucket.
	GrantWriteACP *string `json:"grantWriteACP,omitempty"`

	// Specifies whether you want S3 Object Lock to be enabled for the new bucket.
	ObjectLockEnabledForBucket *bool `json:"objectLockEnabledForBucket,omitempty"`

	// The container element for object ownership for a bucket's ownership controls.
	//
	// BucketOwnerPreferred - Objects uploaded to the bucket change ownership to
	// the bucket owner if the objects are uploaded with the bucket-owner-full-control
	// canned ACL.
	//
	// ObjectWriter - The uploading account will own the object if the object is
	// uploaded with the bucket-owner-full-control canned ACL.
	//
	// BucketOwnerEnforced - Access control lists (ACLs) are disabled and no longer
	// affect permissions. The bucket owner automatically owns and has full control
	// over every object in the bucket. The bucket only accepts PUT requests that
	// don't specify an ACL or bucket owner full control ACLs, such as the bucket-owner-full-control
	// canned ACL or an equivalent form of this ACL expressed in the XML format.
	ObjectOwnership *string `json:"objectOwnership,omitempty"`

	// Specifies the Region where the bucket will be created.
	LocationConstraint string `json:"locationConstraint,omitempty"`

	// Creates a new lifecycle configuration for the bucket or replaces an existing
	// lifecycle configuration. For information about lifecycle configuration, see
	// Managing Access Permissions to Your Amazon S3 Resources
	// (https://docs.aws.amazon.com/AmazonS3/latest/dev/s3-access-control.html).
	// +optional
	LifecycleConfiguration *BucketLifecycleConfiguration `json:"lifecycleConfiguration,omitempty"`

	// AssumeRoleTags may be used to add custom values to an AssumeRole request.
	// +optional
	AssumeRoleTags []Tag `json:"assumeRoleTags,omitempty"`

	// Policy is a JSON string of BucketPolicy.
	// If it is set, Provider-Ceph calls PutBucketPolicy API after creating the bucket.
	// Before adding it, you should validate the JSON string.
	// +optional
	Policy string `json:"policy,omitempty"`
}

// BackendInfo contains relevant information about an S3 backend for
// a single bucket.
type BackendInfo struct {
	// BucketCondition is the condition of the Bucket on the S3 backend.
	BucketCondition xpv1.Condition `json:"bucketCondition,omitempty"`
	// LifecycleConfigurationCondition is the condition of the bucket lifecycle
	// configuration on the S3 backend. Use a pointer to allow nil value when
	// there is no lifecycle configuration.
	LifecycleConfigurationCondition *xpv1.Condition `json:"lifecycleConfigurationCondition,omitempty"`
}

// Backends is a map of the names of the S3 backends to BackendInfo.
type Backends map[string]*BackendInfo

const (
	ValidationRequiredLabel = "provider-ceph.crossplane.io/validation-required"
)

// BucketObservation are the observable fields of a Bucket.
type BucketObservation struct {
	Backends          Backends `json:"backends,omitempty"`
	ConfigurableField string   `json:"configurableField"`
	ObservableField   string   `json:"observableField,omitempty"`
}

// A BucketSpec defines the desired state of a Bucket.
type BucketSpec struct {
	// +optional
	// Providers is a list of ProviderConfig names representing
	// S3 backends on which the bucket is to be created.
	Providers   []string         `json:"providers,omitempty"`
	ForProvider BucketParameters `json:"forProvider"`
	// Disabled allows the user to create a Bucket CR without creating
	// buckets on any S3 backends. If an existing bucket CR is updated
	// with Disabled=true, then provider-ceph attempts to remove any
	// existing buckets from the existing S3 backends and the Bucket
	// CR's status is updated accordingly.
	// This flag overrides 'Providers'.
	Disabled bool `json:"disabled,omitempty"`
	// LifecycleConfigurationDisabled causes provider-ceph to
	// attempt deletion and/or avoid create/updates of the
	// lifecycle config for the bucket on all of the bucket's
	// backends. The Bucket CR's status is updated accordingly.
	LifecycleConfigurationDisabled bool `json:"lifecycleConfigurationDisabled,omitempty"`
	// +optional
	// AutoPause allows the user to disable further reconciliation
	// of the bucket after successfully created or updated.
	// If `crossplane.io/paused` label is `true`, disables reconciliation of object.
	// If `crossplane.io/paused` label is missing or empty, triggers auto pause function.
	// Any other value disables auto pause function on bucket.
	AutoPause         bool `json:"autoPause,omitempty"`
	xpv1.ResourceSpec `json:",inline"`
}

// A BucketStatus represents the observed state of a Bucket.
type BucketStatus struct {
	AtProvider          BucketObservation `json:"atProvider,omitempty"`
	xpv1.ResourceStatus `json:",inline"`
}

// +kubebuilder:object:root=true

// A Bucket is an example API type.
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="SYNCED",type="string",JSONPath=".status.conditions[?(@.type=='Synced')].status"
// +kubebuilder:printcolumn:name="EXTERNAL-NAME",type="string",JSONPath=".metadata.annotations.crossplane\\.io/external-name"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,categories={crossplane,managed,ceph}
type Bucket struct {
	Spec              BucketSpec   `json:"spec"`
	Status            BucketStatus `json:"status,omitempty"`
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
}

// +kubebuilder:object:root=true

// BucketList contains a list of Bucket
type BucketList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Bucket `json:"items"`
}

// Bucket type metadata.
var (
	BucketKind             = reflect.TypeOf(Bucket{}).Name()
	BucketGroupKind        = schema.GroupKind{Group: Group, Kind: BucketKind}.String()
	BucketKindAPIVersion   = BucketKind + "." + SchemeGroupVersion.String()
	BucketGroupVersionKind = SchemeGroupVersion.WithKind(BucketKind)
)

func init() {
	SchemeBuilder.Register(&Bucket{}, &BucketList{})
}
