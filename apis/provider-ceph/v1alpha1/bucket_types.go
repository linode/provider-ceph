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
	ConfigurableField string `json:"configurableField"`

	// The canned ACL to apply to the bucket.
	ACL *string `json:"acl,omitempty"`

	// Specifies the Region where the bucket will be created.
	LocationConstraint *string `json:"locationConstraint,omitempty"`

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
}

// BucketObservation are the observable fields of a Bucket.
type BucketObservation struct {
	ObservableField string `json:"observableField,omitempty"`
}

// A BucketSpec defines the desired state of a Bucket.
type BucketSpec struct {
	xpv1.ResourceSpec `json:",inline"`
	ForProvider       BucketParameters `json:"forProvider"`
}

// A BucketStatus represents the observed state of a Bucket.
type BucketStatus struct {
	xpv1.ResourceStatus `json:",inline"`
	AtProvider          BucketObservation `json:"atProvider,omitempty"`
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
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BucketSpec   `json:"spec"`
	Status BucketStatus `json:"status,omitempty"`
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
