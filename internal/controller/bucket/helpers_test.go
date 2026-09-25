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

package bucket

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	xpv1 "github.com/crossplane/crossplane-runtime/v2/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	"github.com/crossplane/crossplane-runtime/v2/pkg/meta"
	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	"github.com/linode/provider-ceph/internal/consts"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

//nolint:maintidx // Requires many scenarios for full coverage.
func TestIsPauseRequired(t *testing.T) {
	t.Parallel()
	available := xpv1.Available()
	unavailable := xpv1.Unavailable()
	deletionTimestamp := metav1.Now()
	vEnabled := v1alpha1.VersioningStatusEnabled
	someErr := errors.New("some error")
	type args struct {
		bucket           *v1alpha1.Bucket
		providerNames    []string
		clients          map[string]backendstore.S3Client
		bucketBackends   *bucketBackends
		autoPauseEnabled bool
	}

	type want struct {
		pauseIsRequired bool
	}

	cases := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"Bucket Status has no conditions - no pause": {
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: consts.TestBucket,
					},
					Status: v1alpha1.BucketStatus{
						ResourceStatus: xpv1.ResourceStatus{
							ConditionedStatus: xpv1.ConditionedStatus{
								Conditions: []xpv1.Condition{},
							},
						},
					},
				},
			},
			want: want{
				pauseIsRequired: false,
			},
		},
		"Bucket Status has Ready condition but no Synced condition - no pause": {
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: consts.TestBucket,
					},
					Status: v1alpha1.BucketStatus{
						ResourceStatus: xpv1.ResourceStatus{
							ConditionedStatus: xpv1.ConditionedStatus{
								Conditions: []xpv1.Condition{
									xpv1.Available(),
								},
							},
						},
					},
				},
			},
			want: want{
				pauseIsRequired: false,
			},
		},
		"Bucket Status has Synced condition but no Ready condition - no pause": {
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: consts.TestBucket,
					},
					Status: v1alpha1.BucketStatus{
						ResourceStatus: xpv1.ResourceStatus{
							ConditionedStatus: xpv1.ConditionedStatus{
								Conditions: []xpv1.Condition{
									xpv1.ReconcileError(someErr),
								},
							},
						},
					},
				},
			},
			want: want{
				pauseIsRequired: false,
			},
		},
		"Bucket Status has not Ready and not Synced conditions - no pause": {
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: consts.TestBucket,
					},
					Status: v1alpha1.BucketStatus{
						ResourceStatus: xpv1.ResourceStatus{
							ConditionedStatus: xpv1.ConditionedStatus{
								Conditions: []xpv1.Condition{
									xpv1.Unavailable(),
									xpv1.ReconcileError(someErr),
								},
							},
						},
					},
				},
			},
			want: want{
				pauseIsRequired: false,
			},
		},
		"Bucket Status has Ready but not Synced conditions - no pause": {
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: consts.TestBucket,
					},
					Status: v1alpha1.BucketStatus{
						ResourceStatus: xpv1.ResourceStatus{
							ConditionedStatus: xpv1.ConditionedStatus{
								Conditions: []xpv1.Condition{
									xpv1.Available(),
									xpv1.ReconcileError(someErr),
								},
							},
						},
					},
				},
			},
			want: want{
				pauseIsRequired: false,
			},
		},
		"Bucket Status has Synced but not Ready conditions - no pause": {
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: consts.TestBucket,
					},
					Status: v1alpha1.BucketStatus{
						ResourceStatus: xpv1.ResourceStatus{
							ConditionedStatus: xpv1.ConditionedStatus{
								Conditions: []xpv1.Condition{
									xpv1.Unavailable(),
									xpv1.ReconcileSuccess(),
								},
							},
						},
					},
				},
			},
			want: want{
				pauseIsRequired: false,
			},
		},
		// All Buckets from this point are Ready and Synced.
		"One backend unavailable in bucket backends - no pause": {
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: consts.TestBucket,
					},
					Status: v1alpha1.BucketStatus{
						ResourceStatus: xpv1.ResourceStatus{
							ConditionedStatus: xpv1.ConditionedStatus{
								Conditions: []xpv1.Condition{
									xpv1.Available(),
									xpv1.ReconcileSuccess(),
								},
							},
						},
					},
				},
				providerNames: []string{consts.S3Backend1, consts.S3Backend2, consts.S3Backend3},
				clients: map[string]backendstore.S3Client{
					consts.S3Backend1: nil,
					consts.S3Backend2: nil,
					consts.S3Backend3: nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						consts.TestBucket: {
							consts.S3Backend1: &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
							consts.S3Backend2: &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
							consts.S3Backend3: &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Unavailable(),
							},
						},
					},
				},
			},
			want: want{
				pauseIsRequired: false,
			},
		},
		"One backend missing in bucket backends - no pause": {
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: consts.TestBucket,
					},
					Status: v1alpha1.BucketStatus{
						ResourceStatus: xpv1.ResourceStatus{
							ConditionedStatus: xpv1.ConditionedStatus{
								Conditions: []xpv1.Condition{
									xpv1.Available(),
									xpv1.ReconcileSuccess(),
								},
							},
						},
					},
				},
				providerNames: []string{consts.S3Backend1, consts.S3Backend2, consts.S3Backend3},
				clients: map[string]backendstore.S3Client{
					consts.S3Backend1: nil,
					consts.S3Backend2: nil,
					consts.S3Backend3: nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						consts.TestBucket: {
							consts.S3Backend1: &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
							consts.S3Backend2: &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
						},
					},
				},
			},
			want: want{
				pauseIsRequired: false,
			},
		},
		"All backends available in bucket backends but no autopause - no pause": {
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: consts.TestBucket,
					},
					Status: v1alpha1.BucketStatus{
						ResourceStatus: xpv1.ResourceStatus{
							ConditionedStatus: xpv1.ConditionedStatus{
								Conditions: []xpv1.Condition{
									xpv1.Available(),
									xpv1.ReconcileSuccess(),
								},
							},
						},
					},
				},
				providerNames: []string{consts.S3Backend1, consts.S3Backend2, consts.S3Backend3},
				clients: map[string]backendstore.S3Client{
					consts.S3Backend1: nil,
					consts.S3Backend2: nil,
					consts.S3Backend3: nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						consts.TestBucket: {
							consts.S3Backend1: &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
							consts.S3Backend2: &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
							consts.S3Backend3: &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
						},
					},
				},
			},
			want: want{
				pauseIsRequired: false,
			},
		},
		"All backends available in bucket backends and autopause enabled but pause label false - no pause": {
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: consts.TestBucket,
						Labels: map[string]string{
							meta.AnnotationKeyReconciliationPaused: consts.FalseStr,
						},
					},
					Status: v1alpha1.BucketStatus{
						ResourceStatus: xpv1.ResourceStatus{
							ConditionedStatus: xpv1.ConditionedStatus{
								Conditions: []xpv1.Condition{
									xpv1.Available(),
									xpv1.ReconcileSuccess(),
								},
							},
						},
					},
				},
				providerNames: []string{consts.S3Backend1, consts.S3Backend2, consts.S3Backend3},
				clients: map[string]backendstore.S3Client{
					consts.S3Backend1: nil,
					consts.S3Backend2: nil,
					consts.S3Backend3: nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						consts.TestBucket: {
							consts.S3Backend1: &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
							consts.S3Backend2: &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
							consts.S3Backend3: &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
						},
					},
				},
				autoPauseEnabled: true,
			},
			want: want{
				pauseIsRequired: false,
			},
		},
		"All backends available in bucket backends and autopause enabled for bucket but pause label false - no pause": {
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: consts.TestBucket,
						Labels: map[string]string{
							meta.AnnotationKeyReconciliationPaused: consts.FalseStr,
						},
					},
					Spec: v1alpha1.BucketSpec{
						AutoPause: true,
					},
					Status: v1alpha1.BucketStatus{
						ResourceStatus: xpv1.ResourceStatus{
							ConditionedStatus: xpv1.ConditionedStatus{
								Conditions: []xpv1.Condition{
									xpv1.Available(),
									xpv1.ReconcileSuccess(),
								},
							},
						},
					},
				},
				providerNames: []string{consts.S3Backend1, consts.S3Backend2, consts.S3Backend3},
				clients: map[string]backendstore.S3Client{
					consts.S3Backend1: nil,
					consts.S3Backend2: nil,
					consts.S3Backend3: nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						consts.TestBucket: {
							consts.S3Backend1: &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
							consts.S3Backend2: &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
							consts.S3Backend3: &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
						},
					},
				},
			},
			want: want{
				pauseIsRequired: false,
			},
		},
		"All backends available in bucket backends and autopause enabled and empty pause label - pause": {
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: consts.TestBucket,
						Labels: map[string]string{
							meta.AnnotationKeyReconciliationPaused: "",
						},
					},
					Status: v1alpha1.BucketStatus{
						ResourceStatus: xpv1.ResourceStatus{
							ConditionedStatus: xpv1.ConditionedStatus{
								Conditions: []xpv1.Condition{
									xpv1.Available(),
									xpv1.ReconcileSuccess(),
								},
							},
						},
					},
				},
				providerNames: []string{consts.S3Backend1, consts.S3Backend2, consts.S3Backend3},
				clients: map[string]backendstore.S3Client{
					consts.S3Backend1: nil,
					consts.S3Backend2: nil,
					consts.S3Backend3: nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						consts.TestBucket: {
							consts.S3Backend1: &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
							consts.S3Backend2: &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
							consts.S3Backend3: &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
						},
					},
				},
				autoPauseEnabled: true,
			},
			want: want{
				pauseIsRequired: true,
			},
		},
		"All backends available in bucket backends and autopause enabled but Bucket CR disabled - no pause": {
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: consts.TestBucket,
						Labels: map[string]string{
							meta.AnnotationKeyReconciliationPaused: "",
						},
					},
					Spec: v1alpha1.BucketSpec{
						Disabled: true,
					},
					Status: v1alpha1.BucketStatus{
						ResourceStatus: xpv1.ResourceStatus{
							ConditionedStatus: xpv1.ConditionedStatus{
								Conditions: []xpv1.Condition{
									xpv1.Available(),
									xpv1.ReconcileSuccess(),
								},
							},
						},
					},
				},
				providerNames: []string{consts.S3Backend1, consts.S3Backend2, consts.S3Backend3},
				clients: map[string]backendstore.S3Client{
					consts.S3Backend1: nil,
					consts.S3Backend2: nil,
					consts.S3Backend3: nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						consts.TestBucket: {
							consts.S3Backend1: &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
							consts.S3Backend2: &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
							consts.S3Backend3: &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
						},
					},
				},
				autoPauseEnabled: true,
			},
			want: want{
				pauseIsRequired: false,
			},
		},
		"All backends available in bucket backends and autopause enabled but Bucket CR deleting - no pause": {
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name:              consts.TestBucket,
						DeletionTimestamp: &deletionTimestamp,
						Finalizers:        []string{"finalizer.managedresource.crossplane.io"},
						Labels: map[string]string{
							meta.AnnotationKeyReconciliationPaused: "",
						},
					},
					Status: v1alpha1.BucketStatus{
						ResourceStatus: xpv1.ResourceStatus{
							ConditionedStatus: xpv1.ConditionedStatus{
								Conditions: []xpv1.Condition{
									xpv1.Available(),
									xpv1.ReconcileSuccess(),
								},
							},
						},
					},
				},
				providerNames: []string{consts.S3Backend1, consts.S3Backend2, consts.S3Backend3},
				clients: map[string]backendstore.S3Client{
					consts.S3Backend1: nil,
					consts.S3Backend2: nil,
					consts.S3Backend3: nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						consts.TestBucket: {
							consts.S3Backend1: &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
							consts.S3Backend2: &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
							consts.S3Backend3: &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
						},
					},
				},
				autoPauseEnabled: true,
			},
			want: want{
				pauseIsRequired: false,
			},
		},
		"All backends available in bucket backends and autopause enabled for bucket and empty pause label - pause": {
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: consts.TestBucket,
						Labels: map[string]string{
							meta.AnnotationKeyReconciliationPaused: "",
						},
					},
					Spec: v1alpha1.BucketSpec{
						AutoPause: true,
					},
					Status: v1alpha1.BucketStatus{
						ResourceStatus: xpv1.ResourceStatus{
							ConditionedStatus: xpv1.ConditionedStatus{
								Conditions: []xpv1.Condition{
									xpv1.Available(),
									xpv1.ReconcileSuccess(),
								},
							},
						},
					},
				},
				providerNames: []string{consts.S3Backend1, consts.S3Backend2, consts.S3Backend3},
				clients: map[string]backendstore.S3Client{
					consts.S3Backend1: nil,
					consts.S3Backend2: nil,
					consts.S3Backend3: nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						consts.TestBucket: {
							consts.S3Backend1: &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
							consts.S3Backend2: &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
							consts.S3Backend3: &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
						},
					},
				},
			},
			want: want{
				pauseIsRequired: true,
			},
		},
		"All backends available in bucket backends and autopause enabled and no pause label - pause": {
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: consts.TestBucket,
						Labels: map[string]string{
							"some": "label",
						},
					},
					Status: v1alpha1.BucketStatus{
						ResourceStatus: xpv1.ResourceStatus{
							ConditionedStatus: xpv1.ConditionedStatus{
								Conditions: []xpv1.Condition{
									xpv1.Available(),
									xpv1.ReconcileSuccess(),
								},
							},
						},
					},
				},
				providerNames: []string{consts.S3Backend1, consts.S3Backend2, consts.S3Backend3},
				clients: map[string]backendstore.S3Client{
					consts.S3Backend1: nil,
					consts.S3Backend2: nil,
					consts.S3Backend3: nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						consts.TestBucket: {
							consts.S3Backend1: &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
							consts.S3Backend2: &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
							consts.S3Backend3: &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
						},
					},
				},
				autoPauseEnabled: true,
			},
			want: want{
				pauseIsRequired: true,
			},
		},
		"All backends available in bucket backends and autopause enabled for bucket and no pause label - pause": {
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: consts.TestBucket,
						Labels: map[string]string{
							"some": "label",
						},
					},
					Spec: v1alpha1.BucketSpec{
						AutoPause: true,
					},
					Status: v1alpha1.BucketStatus{
						ResourceStatus: xpv1.ResourceStatus{
							ConditionedStatus: xpv1.ConditionedStatus{
								Conditions: []xpv1.Condition{
									xpv1.Available(),
									xpv1.ReconcileSuccess(),
								},
							},
						},
					},
				},
				providerNames: []string{consts.S3Backend1, consts.S3Backend2, consts.S3Backend3},
				clients: map[string]backendstore.S3Client{
					consts.S3Backend1: nil,
					consts.S3Backend2: nil,
					consts.S3Backend3: nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						consts.TestBucket: {
							consts.S3Backend1: &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
							consts.S3Backend2: &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
							consts.S3Backend3: &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
						},
					},
				},
			},
			want: want{
				pauseIsRequired: true,
			},
		},
		"Lifecycle config enabled and specified but unavailable on one backend - no pause": {
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: consts.TestBucket,
						Labels: map[string]string{
							meta.AnnotationKeyReconciliationPaused: "",
						},
					},
					Spec: v1alpha1.BucketSpec{
						AutoPause: true,
						ForProvider: v1alpha1.BucketParameters{
							LifecycleConfiguration: &v1alpha1.BucketLifecycleConfiguration{
								Rules: []v1alpha1.LifecycleRule{
									{
										Status: consts.EnabledStr,
									},
								},
							},
						},
					},
					Status: v1alpha1.BucketStatus{
						ResourceStatus: xpv1.ResourceStatus{
							ConditionedStatus: xpv1.ConditionedStatus{
								Conditions: []xpv1.Condition{
									xpv1.Available(),
									xpv1.ReconcileSuccess(),
								},
							},
						},
					},
				},
				providerNames: []string{consts.S3Backend1, consts.S3Backend2, consts.S3Backend3},
				clients: map[string]backendstore.S3Client{
					consts.S3Backend1: nil,
					consts.S3Backend2: nil,
					consts.S3Backend3: nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						consts.TestBucket: {
							consts.S3Backend1: &v1alpha1.BackendInfo{
								BucketCondition:                 xpv1.Available(),
								LifecycleConfigurationCondition: &available,
							},
							consts.S3Backend2: &v1alpha1.BackendInfo{
								BucketCondition:                 xpv1.Available(),
								LifecycleConfigurationCondition: &available,
							},
							consts.S3Backend3: &v1alpha1.BackendInfo{
								BucketCondition:                 xpv1.Available(),
								LifecycleConfigurationCondition: &unavailable,
							},
						},
					},
				},
			},
			want: want{
				pauseIsRequired: false,
			},
		},
		"Lifecycle config enabled and specified but missing from one backend - no pause": {
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: consts.TestBucket,
						Labels: map[string]string{
							meta.AnnotationKeyReconciliationPaused: "",
						},
					},
					Spec: v1alpha1.BucketSpec{
						AutoPause: true,
						ForProvider: v1alpha1.BucketParameters{
							LifecycleConfiguration: &v1alpha1.BucketLifecycleConfiguration{
								Rules: []v1alpha1.LifecycleRule{
									{
										Status: consts.EnabledStr,
									},
								},
							},
						},
					},
					Status: v1alpha1.BucketStatus{
						ResourceStatus: xpv1.ResourceStatus{
							ConditionedStatus: xpv1.ConditionedStatus{
								Conditions: []xpv1.Condition{
									xpv1.Available(),
									xpv1.ReconcileSuccess(),
								},
							},
						},
					},
				},
				providerNames: []string{consts.S3Backend1, consts.S3Backend2, consts.S3Backend3},
				clients: map[string]backendstore.S3Client{
					consts.S3Backend1: nil,
					consts.S3Backend2: nil,
					consts.S3Backend3: nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						consts.TestBucket: {
							consts.S3Backend1: &v1alpha1.BackendInfo{
								BucketCondition:                 xpv1.Available(),
								LifecycleConfigurationCondition: &available,
							},
							consts.S3Backend2: &v1alpha1.BackendInfo{
								BucketCondition:                 xpv1.Available(),
								LifecycleConfigurationCondition: &available,
							},
							consts.S3Backend3: &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
						},
					},
				},
			},
			want: want{
				pauseIsRequired: false,
			},
		},
		"Lifecycle config enabled and specified and available on all backends - pause": {
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: consts.TestBucket,
						Labels: map[string]string{
							meta.AnnotationKeyReconciliationPaused: "",
						},
					},
					Spec: v1alpha1.BucketSpec{
						AutoPause: true,
						ForProvider: v1alpha1.BucketParameters{
							LifecycleConfiguration: &v1alpha1.BucketLifecycleConfiguration{
								Rules: []v1alpha1.LifecycleRule{
									{
										Status: consts.EnabledStr,
									},
								},
							},
						},
					},
					Status: v1alpha1.BucketStatus{
						ResourceStatus: xpv1.ResourceStatus{
							ConditionedStatus: xpv1.ConditionedStatus{
								Conditions: []xpv1.Condition{
									xpv1.Available(),
									xpv1.ReconcileSuccess(),
								},
							},
						},
					},
				},
				providerNames: []string{consts.S3Backend1, consts.S3Backend2, consts.S3Backend3},
				clients: map[string]backendstore.S3Client{
					consts.S3Backend1: nil,
					consts.S3Backend2: nil,
					consts.S3Backend3: nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						consts.TestBucket: {
							consts.S3Backend1: &v1alpha1.BackendInfo{
								BucketCondition:                 xpv1.Available(),
								LifecycleConfigurationCondition: &available,
							},
							consts.S3Backend2: &v1alpha1.BackendInfo{
								BucketCondition:                 xpv1.Available(),
								LifecycleConfigurationCondition: &available,
							},
							consts.S3Backend3: &v1alpha1.BackendInfo{
								BucketCondition:                 xpv1.Available(),
								LifecycleConfigurationCondition: &available,
							},
						},
					},
				},
			},
			want: want{
				pauseIsRequired: true,
			},
		},
		"Lifecycle config disabled but not removed from all backends - no pause": {
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: consts.TestBucket,
						Labels: map[string]string{
							meta.AnnotationKeyReconciliationPaused: "",
						},
					},
					Spec: v1alpha1.BucketSpec{
						LifecycleConfigurationDisabled: true,
						AutoPause:                      true,
						ForProvider: v1alpha1.BucketParameters{
							LifecycleConfiguration: &v1alpha1.BucketLifecycleConfiguration{
								Rules: []v1alpha1.LifecycleRule{
									{
										Status: consts.EnabledStr,
									},
								},
							},
						},
					},
					Status: v1alpha1.BucketStatus{
						ResourceStatus: xpv1.ResourceStatus{
							ConditionedStatus: xpv1.ConditionedStatus{
								Conditions: []xpv1.Condition{
									xpv1.Available(),
									xpv1.ReconcileSuccess(),
								},
							},
						},
					},
				},
				providerNames: []string{consts.S3Backend1, consts.S3Backend2, consts.S3Backend3},
				clients: map[string]backendstore.S3Client{
					consts.S3Backend1: nil,
					consts.S3Backend2: nil,
					consts.S3Backend3: nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						consts.TestBucket: {
							consts.S3Backend1: &v1alpha1.BackendInfo{
								BucketCondition:                 xpv1.Available(),
								LifecycleConfigurationCondition: &available,
							},
							consts.S3Backend2: &v1alpha1.BackendInfo{
								BucketCondition:                 xpv1.Available(),
								LifecycleConfigurationCondition: &unavailable,
							},
							consts.S3Backend3: &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
						},
					},
				},
			},
			want: want{
				pauseIsRequired: false,
			},
		},
		"Lifecycle config disabled and removed from all backends - pause": {
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: consts.TestBucket,
						Labels: map[string]string{
							meta.AnnotationKeyReconciliationPaused: "",
						},
					},
					Spec: v1alpha1.BucketSpec{
						LifecycleConfigurationDisabled: true,
						AutoPause:                      true,
						ForProvider: v1alpha1.BucketParameters{
							LifecycleConfiguration: &v1alpha1.BucketLifecycleConfiguration{
								Rules: []v1alpha1.LifecycleRule{
									{
										Status: consts.EnabledStr,
									},
								},
							},
						},
					},
					Status: v1alpha1.BucketStatus{
						ResourceStatus: xpv1.ResourceStatus{
							ConditionedStatus: xpv1.ConditionedStatus{
								Conditions: []xpv1.Condition{
									xpv1.Available(),
									xpv1.ReconcileSuccess(),
								},
							},
						},
					},
				},
				providerNames: []string{consts.S3Backend1, consts.S3Backend2, consts.S3Backend3},
				clients: map[string]backendstore.S3Client{
					consts.S3Backend1: nil,
					consts.S3Backend2: nil,
					consts.S3Backend3: nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						consts.TestBucket: {
							consts.S3Backend1: &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
							consts.S3Backend2: &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
							consts.S3Backend3: &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
						},
					},
				},
			},
			want: want{
				pauseIsRequired: true,
			},
		},
		"Versioning config specified but unavailable on one backend - no pause": {
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: consts.TestBucket,
						Labels: map[string]string{
							meta.AnnotationKeyReconciliationPaused: "",
						},
					},
					Spec: v1alpha1.BucketSpec{
						AutoPause: true,
						ForProvider: v1alpha1.BucketParameters{
							VersioningConfiguration: &v1alpha1.VersioningConfiguration{
								Status: &vEnabled,
							},
						},
					},
					Status: v1alpha1.BucketStatus{
						ResourceStatus: xpv1.ResourceStatus{
							ConditionedStatus: xpv1.ConditionedStatus{
								Conditions: []xpv1.Condition{
									xpv1.Available(),
									xpv1.ReconcileSuccess(),
								},
							},
						},
					},
				},
				providerNames: []string{consts.S3Backend1, consts.S3Backend2, consts.S3Backend3},
				clients: map[string]backendstore.S3Client{
					consts.S3Backend1: nil,
					consts.S3Backend2: nil,
					consts.S3Backend3: nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						consts.TestBucket: {
							consts.S3Backend1: &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								VersioningConfigurationCondition: &available,
							},
							consts.S3Backend2: &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								VersioningConfigurationCondition: &available,
							},
							consts.S3Backend3: &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								VersioningConfigurationCondition: &unavailable,
							},
						},
					},
				},
			},
			want: want{
				pauseIsRequired: false,
			},
		},
		"Versioning config specified but missing on one backend - no pause": {
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: consts.TestBucket,
						Labels: map[string]string{
							meta.AnnotationKeyReconciliationPaused: "",
						},
					},
					Spec: v1alpha1.BucketSpec{
						AutoPause: true,
						ForProvider: v1alpha1.BucketParameters{
							VersioningConfiguration: &v1alpha1.VersioningConfiguration{
								Status: &vEnabled,
							},
						},
					},
					Status: v1alpha1.BucketStatus{
						ResourceStatus: xpv1.ResourceStatus{
							ConditionedStatus: xpv1.ConditionedStatus{
								Conditions: []xpv1.Condition{
									xpv1.Available(),
									xpv1.ReconcileSuccess(),
								},
							},
						},
					},
				},
				providerNames: []string{consts.S3Backend1, consts.S3Backend2, consts.S3Backend3},
				clients: map[string]backendstore.S3Client{
					consts.S3Backend1: nil,
					consts.S3Backend2: nil,
					consts.S3Backend3: nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						consts.TestBucket: {
							consts.S3Backend1: &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								VersioningConfigurationCondition: &available,
							},
							consts.S3Backend2: &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								VersioningConfigurationCondition: &available,
							},
							consts.S3Backend3: &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
						},
					},
				},
			},
			want: want{
				pauseIsRequired: false,
			},
		},
		"Versioning config specified and available on all backends - pause": {
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: consts.TestBucket,
						Labels: map[string]string{
							meta.AnnotationKeyReconciliationPaused: "",
						},
					},
					Spec: v1alpha1.BucketSpec{
						AutoPause: true,
						ForProvider: v1alpha1.BucketParameters{
							VersioningConfiguration: &v1alpha1.VersioningConfiguration{
								Status: &vEnabled,
							},
						},
					},
					Status: v1alpha1.BucketStatus{
						ResourceStatus: xpv1.ResourceStatus{
							ConditionedStatus: xpv1.ConditionedStatus{
								Conditions: []xpv1.Condition{
									xpv1.Available(),
									xpv1.ReconcileSuccess(),
								},
							},
						},
					},
				},
				providerNames: []string{consts.S3Backend1, consts.S3Backend2, consts.S3Backend3},
				clients: map[string]backendstore.S3Client{
					consts.S3Backend1: nil,
					consts.S3Backend2: nil,
					consts.S3Backend3: nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						consts.TestBucket: {
							consts.S3Backend1: &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								VersioningConfigurationCondition: &available,
							},
							consts.S3Backend2: &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								VersioningConfigurationCondition: &available,
							},
							consts.S3Backend3: &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								VersioningConfigurationCondition: &available,
							},
						},
					},
				},
			},
			want: want{
				pauseIsRequired: true,
			},
		},
		"Versioning config not specified (suspended) but unavailable on one backend - no pause": {
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: consts.TestBucket,
						Labels: map[string]string{
							meta.AnnotationKeyReconciliationPaused: "",
						},
					},
					Spec: v1alpha1.BucketSpec{
						AutoPause:   true,
						ForProvider: v1alpha1.BucketParameters{},
					},
					Status: v1alpha1.BucketStatus{
						ResourceStatus: xpv1.ResourceStatus{
							ConditionedStatus: xpv1.ConditionedStatus{
								Conditions: []xpv1.Condition{
									xpv1.Available(),
									xpv1.ReconcileSuccess(),
								},
							},
						},
					},
				},
				providerNames: []string{consts.S3Backend1, consts.S3Backend2, consts.S3Backend3},
				clients: map[string]backendstore.S3Client{
					consts.S3Backend1: nil,
					consts.S3Backend2: nil,
					consts.S3Backend3: nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						consts.TestBucket: {
							consts.S3Backend1: &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								VersioningConfigurationCondition: &available,
							},
							consts.S3Backend2: &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								VersioningConfigurationCondition: &available,
							},
							consts.S3Backend3: &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								VersioningConfigurationCondition: &unavailable,
							},
						},
					},
				},
			},
			want: want{
				pauseIsRequired: false,
			},
		},
		"Versioning config not specified (suspended) but missing on one backend - no pause": {
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: consts.TestBucket,
						Labels: map[string]string{
							meta.AnnotationKeyReconciliationPaused: "",
						},
					},
					Spec: v1alpha1.BucketSpec{
						AutoPause:   true,
						ForProvider: v1alpha1.BucketParameters{},
					},
					Status: v1alpha1.BucketStatus{
						ResourceStatus: xpv1.ResourceStatus{
							ConditionedStatus: xpv1.ConditionedStatus{
								Conditions: []xpv1.Condition{
									xpv1.Available(),
									xpv1.ReconcileSuccess(),
								},
							},
						},
					},
				},
				providerNames: []string{consts.S3Backend1, consts.S3Backend2, consts.S3Backend3},
				clients: map[string]backendstore.S3Client{
					consts.S3Backend1: nil,
					consts.S3Backend2: nil,
					consts.S3Backend3: nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						consts.TestBucket: {
							consts.S3Backend1: &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								VersioningConfigurationCondition: &available,
							},
							consts.S3Backend2: &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								VersioningConfigurationCondition: &available,
							},
							consts.S3Backend3: &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
						},
					},
				},
			},
			want: want{
				pauseIsRequired: false,
			},
		},
		"Versioning config not specified (suspended) and available on all backends - pause": {
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: consts.TestBucket,
						Labels: map[string]string{
							meta.AnnotationKeyReconciliationPaused: "",
						},
					},
					Spec: v1alpha1.BucketSpec{
						AutoPause:   true,
						ForProvider: v1alpha1.BucketParameters{},
					},
					Status: v1alpha1.BucketStatus{
						ResourceStatus: xpv1.ResourceStatus{
							ConditionedStatus: xpv1.ConditionedStatus{
								Conditions: []xpv1.Condition{
									xpv1.Available(),
									xpv1.ReconcileSuccess(),
								},
							},
						},
					},
				},
				providerNames: []string{consts.S3Backend1, consts.S3Backend2, consts.S3Backend3},
				clients: map[string]backendstore.S3Client{
					consts.S3Backend1: nil,
					consts.S3Backend2: nil,
					consts.S3Backend3: nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						consts.TestBucket: {
							consts.S3Backend1: &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								VersioningConfigurationCondition: &available,
							},
							consts.S3Backend2: &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								VersioningConfigurationCondition: &available,
							},
							consts.S3Backend3: &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								VersioningConfigurationCondition: &available,
							},
						},
					},
				},
			},
			want: want{
				pauseIsRequired: true,
			},
		},
		"Object lock config specified but unavailable on one backend - no pause": {
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: consts.TestBucket,
						Labels: map[string]string{
							meta.AnnotationKeyReconciliationPaused: "",
						},
					},
					Spec: v1alpha1.BucketSpec{
						AutoPause: true,
						ForProvider: v1alpha1.BucketParameters{
							ObjectLockConfiguration: &v1alpha1.ObjectLockConfiguration{
								ObjectLockEnabled: &objLockEnabled,
							},
						},
					},
					Status: v1alpha1.BucketStatus{
						ResourceStatus: xpv1.ResourceStatus{
							ConditionedStatus: xpv1.ConditionedStatus{
								Conditions: []xpv1.Condition{
									xpv1.Available(),
									xpv1.ReconcileSuccess(),
								},
							},
						},
					},
				},
				providerNames: []string{consts.S3Backend1, consts.S3Backend2, consts.S3Backend3},
				clients: map[string]backendstore.S3Client{
					consts.S3Backend1: nil,
					consts.S3Backend2: nil,
					consts.S3Backend3: nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						consts.TestBucket: {
							consts.S3Backend1: &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								ObjectLockConfigurationCondition: &available,
							},
							consts.S3Backend2: &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								ObjectLockConfigurationCondition: &available,
							},
							consts.S3Backend3: &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								ObjectLockConfigurationCondition: &unavailable,
							},
						},
					},
				},
			},
			want: want{
				pauseIsRequired: false,
			},
		},
		"Object lock config specified but missing on one backend - no pause": {
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: consts.TestBucket,
						Labels: map[string]string{
							meta.AnnotationKeyReconciliationPaused: "",
						},
					},
					Spec: v1alpha1.BucketSpec{
						AutoPause: true,
						ForProvider: v1alpha1.BucketParameters{
							ObjectLockConfiguration: &v1alpha1.ObjectLockConfiguration{
								ObjectLockEnabled: &objLockEnabled,
							},
						},
					},
					Status: v1alpha1.BucketStatus{
						ResourceStatus: xpv1.ResourceStatus{
							ConditionedStatus: xpv1.ConditionedStatus{
								Conditions: []xpv1.Condition{
									xpv1.Available(),
									xpv1.ReconcileSuccess(),
								},
							},
						},
					},
				},
				providerNames: []string{consts.S3Backend1, consts.S3Backend2, consts.S3Backend3},
				clients: map[string]backendstore.S3Client{
					consts.S3Backend1: nil,
					consts.S3Backend2: nil,
					consts.S3Backend3: nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						consts.TestBucket: {
							consts.S3Backend1: &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								ObjectLockConfigurationCondition: &available,
							},
							consts.S3Backend2: &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								ObjectLockConfigurationCondition: &available,
							},
							consts.S3Backend3: &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
						},
					},
				},
			},
			want: want{
				pauseIsRequired: false,
			},
		},
		"Object lock config specified and available on all backends - pause": {
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: consts.TestBucket,
						Labels: map[string]string{
							meta.AnnotationKeyReconciliationPaused: "",
						},
					},
					Spec: v1alpha1.BucketSpec{
						AutoPause: true,
						ForProvider: v1alpha1.BucketParameters{
							ObjectLockConfiguration: &v1alpha1.ObjectLockConfiguration{
								ObjectLockEnabled: &objLockEnabled,
							},
						},
					},
					Status: v1alpha1.BucketStatus{
						ResourceStatus: xpv1.ResourceStatus{
							ConditionedStatus: xpv1.ConditionedStatus{
								Conditions: []xpv1.Condition{
									xpv1.Available(),
									xpv1.ReconcileSuccess(),
								},
							},
						},
					},
				},
				providerNames: []string{consts.S3Backend1, consts.S3Backend2, consts.S3Backend3},
				clients: map[string]backendstore.S3Client{
					consts.S3Backend1: nil,
					consts.S3Backend2: nil,
					consts.S3Backend3: nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						consts.TestBucket: {
							consts.S3Backend1: &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								ObjectLockConfigurationCondition: &available,
							},
							consts.S3Backend2: &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								ObjectLockConfigurationCondition: &available,
							},
							consts.S3Backend3: &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								ObjectLockConfigurationCondition: &available,
							},
						},
					},
				},
			},
			want: want{
				pauseIsRequired: true,
			},
		},
		"All subresources specified and available on all backends and autopause enabled for bucket - pause": {
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: consts.TestBucket,
						Labels: map[string]string{
							meta.AnnotationKeyReconciliationPaused: "",
						},
					},
					Spec: v1alpha1.BucketSpec{
						AutoPause: true,
						ForProvider: v1alpha1.BucketParameters{
							LifecycleConfiguration: &v1alpha1.BucketLifecycleConfiguration{
								Rules: []v1alpha1.LifecycleRule{
									{
										Status: consts.EnabledStr,
									},
								},
							},
							VersioningConfiguration: &v1alpha1.VersioningConfiguration{
								Status: &vEnabled,
							},
							ObjectLockConfiguration: &v1alpha1.ObjectLockConfiguration{
								ObjectLockEnabled: &objLockEnabled,
							},
						},
					},
					Status: v1alpha1.BucketStatus{
						ResourceStatus: xpv1.ResourceStatus{
							ConditionedStatus: xpv1.ConditionedStatus{
								Conditions: []xpv1.Condition{
									xpv1.Available(),
									xpv1.ReconcileSuccess(),
								},
							},
						},
					},
				},
				providerNames: []string{consts.S3Backend1, consts.S3Backend2, consts.S3Backend3},
				clients: map[string]backendstore.S3Client{
					consts.S3Backend1: nil,
					consts.S3Backend2: nil,
					consts.S3Backend3: nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						consts.TestBucket: {
							consts.S3Backend1: &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								LifecycleConfigurationCondition:  &available,
								VersioningConfigurationCondition: &available,
								ObjectLockConfigurationCondition: &available,
							},
							consts.S3Backend2: &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								LifecycleConfigurationCondition:  &available,
								VersioningConfigurationCondition: &available,
								ObjectLockConfigurationCondition: &available,
							},
							consts.S3Backend3: &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								LifecycleConfigurationCondition:  &available,
								VersioningConfigurationCondition: &available,
								ObjectLockConfigurationCondition: &available,
							},
						},
					},
				},
			},
			want: want{
				pauseIsRequired: true,
			},
		},
		"All subresources specified and available on all backends and autopause enabled - pause": {
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: consts.TestBucket,
						Labels: map[string]string{
							meta.AnnotationKeyReconciliationPaused: "",
						},
					},
					Spec: v1alpha1.BucketSpec{
						ForProvider: v1alpha1.BucketParameters{
							LifecycleConfiguration: &v1alpha1.BucketLifecycleConfiguration{
								Rules: []v1alpha1.LifecycleRule{
									{
										Status: consts.EnabledStr,
									},
								},
							},
							VersioningConfiguration: &v1alpha1.VersioningConfiguration{
								Status: &vEnabled,
							},
							ObjectLockConfiguration: &v1alpha1.ObjectLockConfiguration{
								ObjectLockEnabled: &objLockEnabled,
							},
						},
					},
					Status: v1alpha1.BucketStatus{
						ResourceStatus: xpv1.ResourceStatus{
							ConditionedStatus: xpv1.ConditionedStatus{
								Conditions: []xpv1.Condition{
									xpv1.Available(),
									xpv1.ReconcileSuccess(),
								},
							},
						},
					},
				},
				providerNames: []string{consts.S3Backend1, consts.S3Backend2, consts.S3Backend3},
				clients: map[string]backendstore.S3Client{
					consts.S3Backend1: nil,
					consts.S3Backend2: nil,
					consts.S3Backend3: nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						consts.TestBucket: {
							consts.S3Backend1: &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								LifecycleConfigurationCondition:  &available,
								VersioningConfigurationCondition: &available,
								ObjectLockConfigurationCondition: &available,
							},
							consts.S3Backend2: &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								LifecycleConfigurationCondition:  &available,
								VersioningConfigurationCondition: &available,
								ObjectLockConfigurationCondition: &available,
							},
							consts.S3Backend3: &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								LifecycleConfigurationCondition:  &available,
								VersioningConfigurationCondition: &available,
								ObjectLockConfigurationCondition: &available,
							},
						},
					},
				},
				autoPauseEnabled: true,
			},
			want: want{
				pauseIsRequired: true,
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := isPauseRequired(tc.args.bucket,
				tc.args.providerNames,
				tc.args.clients,
				tc.args.bucketBackends,
				tc.args.autoPauseEnabled,
			)
			assert.Equal(t, tc.want.pauseIsRequired, got, "unexpected response")
		})
	}
}

//nolint:maintidx // Function requires numerous checks.
func TestBucketStatusConditionsEqual(t *testing.T) {
	t.Parallel()

	available := xpv1.Available()
	unavailable := xpv1.Unavailable()

	type args struct {
		originalStatus v1alpha1.BucketStatus
		latestStatus   v1alpha1.BucketStatus
	}

	cases := map[string]struct {
		reason string
		args   args
		want   bool
	}{
		// Main condition changes
		"No changes": {
			reason: "Identical statuses should be considered equal",
			args: args{
				originalStatus: v1alpha1.BucketStatus{
					ResourceStatus: xpv1.ResourceStatus{
						ConditionedStatus: xpv1.ConditionedStatus{
							Conditions: []xpv1.Condition{xpv1.Available()},
						},
					},
					AtProvider: v1alpha1.BucketObservation{
						Backends: v1alpha1.Backends{
							consts.S3Backend1: &v1alpha1.BackendInfo{BucketCondition: xpv1.Available()},
						},
					},
				},
				latestStatus: v1alpha1.BucketStatus{
					ResourceStatus: xpv1.ResourceStatus{
						ConditionedStatus: xpv1.ConditionedStatus{
							Conditions: []xpv1.Condition{xpv1.Available()},
						},
					},
					AtProvider: v1alpha1.BucketObservation{
						Backends: v1alpha1.Backends{
							consts.S3Backend1: &v1alpha1.BackendInfo{BucketCondition: xpv1.Available()},
						},
					},
				},
			},
			want: true,
		},
		"Condition count changed": {
			reason: "Different condition counts should indicate not equal",
			args: args{
				originalStatus: v1alpha1.BucketStatus{
					ResourceStatus: xpv1.ResourceStatus{
						ConditionedStatus: xpv1.ConditionedStatus{
							Conditions: []xpv1.Condition{xpv1.Available()},
						},
					},
					AtProvider: v1alpha1.BucketObservation{Backends: v1alpha1.Backends{}},
				},
				latestStatus: v1alpha1.BucketStatus{
					ResourceStatus: xpv1.ResourceStatus{
						ConditionedStatus: xpv1.ConditionedStatus{
							Conditions: []xpv1.Condition{xpv1.Available(), xpv1.Unavailable()},
						},
					},
					AtProvider: v1alpha1.BucketObservation{Backends: v1alpha1.Backends{}},
				},
			},
			want: false,
		},
		"Condition value changed": {
			reason: "Changed condition values should indicate not equal",
			args: args{
				originalStatus: v1alpha1.BucketStatus{
					ResourceStatus: xpv1.ResourceStatus{
						ConditionedStatus: xpv1.ConditionedStatus{
							Conditions: []xpv1.Condition{xpv1.Available()},
						},
					},
					AtProvider: v1alpha1.BucketObservation{Backends: v1alpha1.Backends{}},
				},
				latestStatus: v1alpha1.BucketStatus{
					ResourceStatus: xpv1.ResourceStatus{
						ConditionedStatus: xpv1.ConditionedStatus{
							Conditions: []xpv1.Condition{xpv1.Unavailable()},
						},
					},
					AtProvider: v1alpha1.BucketObservation{Backends: v1alpha1.Backends{}},
				},
			},
			want: false,
		},

		// Backend changes
		"Backend count changed": {
			reason: "Different backend counts should indicate not equal",
			args: args{
				originalStatus: v1alpha1.BucketStatus{
					ResourceStatus: xpv1.ResourceStatus{
						ConditionedStatus: xpv1.ConditionedStatus{Conditions: []xpv1.Condition{}},
					},
					AtProvider: v1alpha1.BucketObservation{
						Backends: v1alpha1.Backends{consts.S3Backend1: &v1alpha1.BackendInfo{BucketCondition: xpv1.Available()}},
					},
				},
				latestStatus: v1alpha1.BucketStatus{
					ResourceStatus: xpv1.ResourceStatus{
						ConditionedStatus: xpv1.ConditionedStatus{Conditions: []xpv1.Condition{}},
					},
					AtProvider: v1alpha1.BucketObservation{
						Backends: v1alpha1.Backends{
							consts.S3Backend1: &v1alpha1.BackendInfo{BucketCondition: xpv1.Available()},
							consts.S3Backend2: &v1alpha1.BackendInfo{BucketCondition: xpv1.Available()},
						},
					},
				},
			},
			want: false,
		},
		"Missing backend in latest": {
			reason: "Backend removed should indicate not equal",
			args: args{
				originalStatus: v1alpha1.BucketStatus{
					ResourceStatus: xpv1.ResourceStatus{
						ConditionedStatus: xpv1.ConditionedStatus{Conditions: []xpv1.Condition{}},
					},
					AtProvider: v1alpha1.BucketObservation{
						Backends: v1alpha1.Backends{
							consts.S3Backend1: &v1alpha1.BackendInfo{BucketCondition: xpv1.Available()},
							consts.S3Backend2: &v1alpha1.BackendInfo{BucketCondition: xpv1.Available()},
						},
					},
				},
				latestStatus: v1alpha1.BucketStatus{
					ResourceStatus: xpv1.ResourceStatus{
						ConditionedStatus: xpv1.ConditionedStatus{Conditions: []xpv1.Condition{}},
					},
					AtProvider: v1alpha1.BucketObservation{
						Backends: v1alpha1.Backends{consts.S3Backend1: &v1alpha1.BackendInfo{BucketCondition: xpv1.Available()}},
					},
				},
			},
			want: false,
		},

		// Backend configuration condition changes
		"Different backend in latest to original": {
			reason: "Changed backend should indicate not equal",
			args: args{
				originalStatus: v1alpha1.BucketStatus{
					ResourceStatus: xpv1.ResourceStatus{
						ConditionedStatus: xpv1.ConditionedStatus{Conditions: []xpv1.Condition{}},
					},
					AtProvider: v1alpha1.BucketObservation{
						Backends: v1alpha1.Backends{
							consts.S3Backend1: &v1alpha1.BackendInfo{
								BucketCondition:                 xpv1.Available(),
								LifecycleConfigurationCondition: &available,
							},
							consts.S3Backend2: &v1alpha1.BackendInfo{
								BucketCondition:                 xpv1.Available(),
								LifecycleConfigurationCondition: &available,
							},
						},
					},
				},
				latestStatus: v1alpha1.BucketStatus{
					ResourceStatus: xpv1.ResourceStatus{
						ConditionedStatus: xpv1.ConditionedStatus{Conditions: []xpv1.Condition{}},
					},
					AtProvider: v1alpha1.BucketObservation{
						Backends: v1alpha1.Backends{
							consts.S3Backend1: &v1alpha1.BackendInfo{
								BucketCondition:                 xpv1.Available(),
								LifecycleConfigurationCondition: &available,
							},
							consts.S3Backend3: &v1alpha1.BackendInfo{
								BucketCondition:                 xpv1.Available(),
								LifecycleConfigurationCondition: &available,
							},
						},
					},
				},
			},
			want: false,
		},
		"Backend lifecycle condition changed": {
			reason: "Changed backend configuration condition should indicate not equal",
			args: args{
				originalStatus: v1alpha1.BucketStatus{
					ResourceStatus: xpv1.ResourceStatus{
						ConditionedStatus: xpv1.ConditionedStatus{Conditions: []xpv1.Condition{}},
					},
					AtProvider: v1alpha1.BucketObservation{
						Backends: v1alpha1.Backends{
							consts.S3Backend1: &v1alpha1.BackendInfo{
								BucketCondition:                 xpv1.Available(),
								LifecycleConfigurationCondition: &available,
							},
						},
					},
				},
				latestStatus: v1alpha1.BucketStatus{
					ResourceStatus: xpv1.ResourceStatus{
						ConditionedStatus: xpv1.ConditionedStatus{Conditions: []xpv1.Condition{}},
					},
					AtProvider: v1alpha1.BucketObservation{
						Backends: v1alpha1.Backends{
							consts.S3Backend1: &v1alpha1.BackendInfo{
								BucketCondition:                 xpv1.Available(),
								LifecycleConfigurationCondition: &unavailable,
							},
						},
					},
				},
			},
			want: false,
		},
		"Nil pointer: condition added": {
			reason: "Condition transitioning from nil to non-nil should indicate not equal",
			args: args{
				originalStatus: v1alpha1.BucketStatus{
					ResourceStatus: xpv1.ResourceStatus{
						ConditionedStatus: xpv1.ConditionedStatus{Conditions: []xpv1.Condition{}},
					},
					AtProvider: v1alpha1.BucketObservation{
						Backends: v1alpha1.Backends{
							consts.S3Backend1: &v1alpha1.BackendInfo{
								BucketCondition:                 xpv1.Available(),
								LifecycleConfigurationCondition: nil,
							},
						},
					},
				},
				latestStatus: v1alpha1.BucketStatus{
					ResourceStatus: xpv1.ResourceStatus{
						ConditionedStatus: xpv1.ConditionedStatus{Conditions: []xpv1.Condition{}},
					},
					AtProvider: v1alpha1.BucketObservation{
						Backends: v1alpha1.Backends{
							consts.S3Backend1: &v1alpha1.BackendInfo{
								BucketCondition:                 xpv1.Available(),
								LifecycleConfigurationCondition: &available,
							},
						},
					},
				},
			},
			want: false,
		},
		"Nil pointer: both conditions nil": {
			reason: "Both conditions nil should indicate equal",
			args: args{
				originalStatus: v1alpha1.BucketStatus{
					ResourceStatus: xpv1.ResourceStatus{
						ConditionedStatus: xpv1.ConditionedStatus{Conditions: []xpv1.Condition{}},
					},
					AtProvider: v1alpha1.BucketObservation{
						Backends: v1alpha1.Backends{
							consts.S3Backend1: &v1alpha1.BackendInfo{
								BucketCondition:                 xpv1.Available(),
								LifecycleConfigurationCondition: nil,
							},
						},
					},
				},
				latestStatus: v1alpha1.BucketStatus{
					ResourceStatus: xpv1.ResourceStatus{
						ConditionedStatus: xpv1.ConditionedStatus{Conditions: []xpv1.Condition{}},
					},
					AtProvider: v1alpha1.BucketObservation{
						Backends: v1alpha1.Backends{
							consts.S3Backend1: &v1alpha1.BackendInfo{
								BucketCondition:                 xpv1.Available(),
								LifecycleConfigurationCondition: nil,
							},
						},
					},
				},
			},
			want: true,
		},
		"LastTransitionTime only changed": {
			reason: "Only LastTransitionTime changed should indicate equal",
			args: args{
				originalStatus: v1alpha1.BucketStatus{
					ResourceStatus: xpv1.ResourceStatus{
						ConditionedStatus: xpv1.ConditionedStatus{
							Conditions: []xpv1.Condition{{
								Type:               xpv1.TypeReady,
								Status:             corev1.ConditionTrue,
								LastTransitionTime: metav1.Now(),
								Reason:             xpv1.ReasonAvailable,
								Message:            "Available",
							}},
						},
					},
					AtProvider: v1alpha1.BucketObservation{Backends: v1alpha1.Backends{}},
				},
				latestStatus: v1alpha1.BucketStatus{
					ResourceStatus: xpv1.ResourceStatus{
						ConditionedStatus: xpv1.ConditionedStatus{
							Conditions: []xpv1.Condition{{
								Type:               xpv1.TypeReady,
								Status:             corev1.ConditionTrue,
								LastTransitionTime: metav1.NewTime(metav1.Now().Add(time.Hour)),
								Reason:             xpv1.ReasonAvailable,
								Message:            "Available",
							}},
						},
					},
					AtProvider: v1alpha1.BucketObservation{Backends: v1alpha1.Backends{}},
				},
			},
			want: true,
		},
		"Backend versioning condition changed": {
			reason: "Changed backend versioning configuration condition should indicate not equal",
			args: args{
				originalStatus: v1alpha1.BucketStatus{
					ResourceStatus: xpv1.ResourceStatus{
						ConditionedStatus: xpv1.ConditionedStatus{Conditions: []xpv1.Condition{}},
					},
					AtProvider: v1alpha1.BucketObservation{
						Backends: v1alpha1.Backends{
							consts.S3Backend1: &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								VersioningConfigurationCondition: &available,
							},
						},
					},
				},
				latestStatus: v1alpha1.BucketStatus{
					ResourceStatus: xpv1.ResourceStatus{
						ConditionedStatus: xpv1.ConditionedStatus{Conditions: []xpv1.Condition{}},
					},
					AtProvider: v1alpha1.BucketObservation{
						Backends: v1alpha1.Backends{
							consts.S3Backend1: &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								VersioningConfigurationCondition: &unavailable,
							},
						},
					},
				},
			},
			want: false,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := bucketStatusConditionsEqual(tc.args.originalStatus, tc.args.latestStatus)
			assert.Equal(t, tc.want, got, tc.reason)
		})
	}
}

func TestLabelsEqual(t *testing.T) {
	t.Parallel()

	key1, key2, value1, value2 := "key1", "key2", "value1", "value2"
	type args struct {
		original map[string]string
		latest   map[string]string
	}

	cases := map[string]struct {
		reason string
		args   args
		want   bool
	}{
		"Both empty - equal": {
			reason: "Empty maps should be considered equal",
			args: args{
				original: map[string]string{},
				latest:   map[string]string{},
			},
			want: true,
		},
		"Both nil - equal": {
			reason: "Nil maps should be considered equal",
			args: args{
				original: nil,
				latest:   nil,
			},
			want: true,
		},
		"Empty vs nil - equal": {
			reason: "Empty map and nil should be considered equal",
			args: args{
				original: map[string]string{},
				latest:   nil,
			},
			want: true,
		},
		"Labels added - not equal": {
			reason: "Adding labels should be considered different",
			args: args{
				original: map[string]string{},
				latest:   map[string]string{key1: value1},
			},
			want: false,
		},
		"Labels removed - not equal": {
			reason: "Removing labels should be considered different",
			args: args{
				original: map[string]string{key1: value1},
				latest:   map[string]string{},
			},
			want: false,
		},
		"Label value changed - not equal": {
			reason: "Changing label value should be considered different",
			args: args{
				original: map[string]string{key1: value1},
				latest:   map[string]string{key1: value2},
			},
			want: false,
		},
		"Same labels - equal": {
			reason: "Same labels should be considered equal",
			args: args{
				original: map[string]string{key1: value1},
				latest:   map[string]string{key1: value1},
			},
			want: true,
		},
		"Multiple labels unchanged - equal": {
			reason: "Multiple identical labels should be considered equal",
			args: args{
				original: map[string]string{key1: value1, key2: value2},
				latest:   map[string]string{key1: value1, key2: value2},
			},
			want: true,
		},
		"One label added to multiple - not equal": {
			reason: "Adding a label to existing labels should be considered different",
			args: args{
				original: map[string]string{key1: value1},
				latest:   map[string]string{key1: value1, key2: value2},
			},
			want: false,
		},
		"One label removed from multiple - not equal": {
			reason: "Removing a label from multiple should be considered different",
			args: args{
				original: map[string]string{key1: value1, key2: value2},
				latest:   map[string]string{key1: value1},
			},
			want: false,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := labelsEqual(tc.args.original, tc.args.latest)
			assert.Equal(t, tc.want, got, tc.reason)
		})
	}
}
