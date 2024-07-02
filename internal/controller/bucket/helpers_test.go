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

	"github.com/stretchr/testify/assert"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

//nolint:maintidx // Requires many scenarios for full coverage.
func TestIsPauseRequired(t *testing.T) {
	t.Parallel()
	available := xpv1.Available()
	unavailable := xpv1.Unavailable()
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
						Name: "bucket",
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
						Name: "bucket",
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
						Name: "bucket",
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
						Name: "bucket",
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
						Name: "bucket",
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
						Name: "bucket",
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
						Name: "bucket",
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
				providerNames: []string{"s3-backend-1", "s3-backend-2", "s3-backend-3"},
				clients: map[string]backendstore.S3Client{
					"s3-backend-1": nil,
					"s3-backend-2": nil,
					"s3-backend-3": nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						"bucket": {
							"s3-backend-1": &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
							"s3-backend-2": &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
							"s3-backend-3": &v1alpha1.BackendInfo{
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
						Name: "bucket",
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
				providerNames: []string{"s3-backend-1", "s3-backend-2", "s3-backend-3"},
				clients: map[string]backendstore.S3Client{
					"s3-backend-1": nil,
					"s3-backend-2": nil,
					"s3-backend-3": nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						"bucket": {
							"s3-backend-1": &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
							"s3-backend-2": &v1alpha1.BackendInfo{
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
						Name: "bucket",
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
				providerNames: []string{"s3-backend-1", "s3-backend-2", "s3-backend-3"},
				clients: map[string]backendstore.S3Client{
					"s3-backend-1": nil,
					"s3-backend-2": nil,
					"s3-backend-3": nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						"bucket": {
							"s3-backend-1": &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
							"s3-backend-2": &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
							"s3-backend-3": &v1alpha1.BackendInfo{
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
						Name: "bucket",
						Labels: map[string]string{
							meta.AnnotationKeyReconciliationPaused: "false",
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
				providerNames: []string{"s3-backend-1", "s3-backend-2", "s3-backend-3"},
				clients: map[string]backendstore.S3Client{
					"s3-backend-1": nil,
					"s3-backend-2": nil,
					"s3-backend-3": nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						"bucket": {
							"s3-backend-1": &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
							"s3-backend-2": &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
							"s3-backend-3": &v1alpha1.BackendInfo{
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
						Name: "bucket",
						Labels: map[string]string{
							meta.AnnotationKeyReconciliationPaused: "false",
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
				providerNames: []string{"s3-backend-1", "s3-backend-2", "s3-backend-3"},
				clients: map[string]backendstore.S3Client{
					"s3-backend-1": nil,
					"s3-backend-2": nil,
					"s3-backend-3": nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						"bucket": {
							"s3-backend-1": &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
							"s3-backend-2": &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
							"s3-backend-3": &v1alpha1.BackendInfo{
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
						Name: "bucket",
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
				providerNames: []string{"s3-backend-1", "s3-backend-2", "s3-backend-3"},
				clients: map[string]backendstore.S3Client{
					"s3-backend-1": nil,
					"s3-backend-2": nil,
					"s3-backend-3": nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						"bucket": {
							"s3-backend-1": &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
							"s3-backend-2": &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
							"s3-backend-3": &v1alpha1.BackendInfo{
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
		"All backends available in bucket backends and autopause enabled for bucket and empty pause label - pause": {
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: "bucket",
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
				providerNames: []string{"s3-backend-1", "s3-backend-2", "s3-backend-3"},
				clients: map[string]backendstore.S3Client{
					"s3-backend-1": nil,
					"s3-backend-2": nil,
					"s3-backend-3": nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						"bucket": {
							"s3-backend-1": &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
							"s3-backend-2": &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
							"s3-backend-3": &v1alpha1.BackendInfo{
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
						Name: "bucket",
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
				providerNames: []string{"s3-backend-1", "s3-backend-2", "s3-backend-3"},
				clients: map[string]backendstore.S3Client{
					"s3-backend-1": nil,
					"s3-backend-2": nil,
					"s3-backend-3": nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						"bucket": {
							"s3-backend-1": &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
							"s3-backend-2": &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
							"s3-backend-3": &v1alpha1.BackendInfo{
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
						Name: "bucket",
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
				providerNames: []string{"s3-backend-1", "s3-backend-2", "s3-backend-3"},
				clients: map[string]backendstore.S3Client{
					"s3-backend-1": nil,
					"s3-backend-2": nil,
					"s3-backend-3": nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						"bucket": {
							"s3-backend-1": &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
							"s3-backend-2": &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
							"s3-backend-3": &v1alpha1.BackendInfo{
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
						Name: "bucket",
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
										Status: "Enabled",
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
				providerNames: []string{"s3-backend-1", "s3-backend-2", "s3-backend-3"},
				clients: map[string]backendstore.S3Client{
					"s3-backend-1": nil,
					"s3-backend-2": nil,
					"s3-backend-3": nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						"bucket": {
							"s3-backend-1": &v1alpha1.BackendInfo{
								BucketCondition:                 xpv1.Available(),
								LifecycleConfigurationCondition: &available,
							},
							"s3-backend-2": &v1alpha1.BackendInfo{
								BucketCondition:                 xpv1.Available(),
								LifecycleConfigurationCondition: &available,
							},
							"s3-backend-3": &v1alpha1.BackendInfo{
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
						Name: "bucket",
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
										Status: "Enabled",
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
				providerNames: []string{"s3-backend-1", "s3-backend-2", "s3-backend-3"},
				clients: map[string]backendstore.S3Client{
					"s3-backend-1": nil,
					"s3-backend-2": nil,
					"s3-backend-3": nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						"bucket": {
							"s3-backend-1": &v1alpha1.BackendInfo{
								BucketCondition:                 xpv1.Available(),
								LifecycleConfigurationCondition: &available,
							},
							"s3-backend-2": &v1alpha1.BackendInfo{
								BucketCondition:                 xpv1.Available(),
								LifecycleConfigurationCondition: &available,
							},
							"s3-backend-3": &v1alpha1.BackendInfo{
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
						Name: "bucket",
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
										Status: "Enabled",
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
				providerNames: []string{"s3-backend-1", "s3-backend-2", "s3-backend-3"},
				clients: map[string]backendstore.S3Client{
					"s3-backend-1": nil,
					"s3-backend-2": nil,
					"s3-backend-3": nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						"bucket": {
							"s3-backend-1": &v1alpha1.BackendInfo{
								BucketCondition:                 xpv1.Available(),
								LifecycleConfigurationCondition: &available,
							},
							"s3-backend-2": &v1alpha1.BackendInfo{
								BucketCondition:                 xpv1.Available(),
								LifecycleConfigurationCondition: &available,
							},
							"s3-backend-3": &v1alpha1.BackendInfo{
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
						Name: "bucket",
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
										Status: "Enabled",
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
				providerNames: []string{"s3-backend-1", "s3-backend-2", "s3-backend-3"},
				clients: map[string]backendstore.S3Client{
					"s3-backend-1": nil,
					"s3-backend-2": nil,
					"s3-backend-3": nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						"bucket": {
							"s3-backend-1": &v1alpha1.BackendInfo{
								BucketCondition:                 xpv1.Available(),
								LifecycleConfigurationCondition: &available,
							},
							"s3-backend-2": &v1alpha1.BackendInfo{
								BucketCondition:                 xpv1.Available(),
								LifecycleConfigurationCondition: &unavailable,
							},
							"s3-backend-3": &v1alpha1.BackendInfo{
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
						Name: "bucket",
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
										Status: "Enabled",
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
				providerNames: []string{"s3-backend-1", "s3-backend-2", "s3-backend-3"},
				clients: map[string]backendstore.S3Client{
					"s3-backend-1": nil,
					"s3-backend-2": nil,
					"s3-backend-3": nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						"bucket": {
							"s3-backend-1": &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
							"s3-backend-2": &v1alpha1.BackendInfo{
								BucketCondition: xpv1.Available(),
							},
							"s3-backend-3": &v1alpha1.BackendInfo{
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
						Name: "bucket",
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
				providerNames: []string{"s3-backend-1", "s3-backend-2", "s3-backend-3"},
				clients: map[string]backendstore.S3Client{
					"s3-backend-1": nil,
					"s3-backend-2": nil,
					"s3-backend-3": nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						"bucket": {
							"s3-backend-1": &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								VersioningConfigurationCondition: &available,
							},
							"s3-backend-2": &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								VersioningConfigurationCondition: &available,
							},
							"s3-backend-3": &v1alpha1.BackendInfo{
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
						Name: "bucket",
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
				providerNames: []string{"s3-backend-1", "s3-backend-2", "s3-backend-3"},
				clients: map[string]backendstore.S3Client{
					"s3-backend-1": nil,
					"s3-backend-2": nil,
					"s3-backend-3": nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						"bucket": {
							"s3-backend-1": &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								VersioningConfigurationCondition: &available,
							},
							"s3-backend-2": &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								VersioningConfigurationCondition: &available,
							},
							"s3-backend-3": &v1alpha1.BackendInfo{
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
						Name: "bucket",
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
				providerNames: []string{"s3-backend-1", "s3-backend-2", "s3-backend-3"},
				clients: map[string]backendstore.S3Client{
					"s3-backend-1": nil,
					"s3-backend-2": nil,
					"s3-backend-3": nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						"bucket": {
							"s3-backend-1": &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								VersioningConfigurationCondition: &available,
							},
							"s3-backend-2": &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								VersioningConfigurationCondition: &available,
							},
							"s3-backend-3": &v1alpha1.BackendInfo{
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
						Name: "bucket",
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
				providerNames: []string{"s3-backend-1", "s3-backend-2", "s3-backend-3"},
				clients: map[string]backendstore.S3Client{
					"s3-backend-1": nil,
					"s3-backend-2": nil,
					"s3-backend-3": nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						"bucket": {
							"s3-backend-1": &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								VersioningConfigurationCondition: &available,
							},
							"s3-backend-2": &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								VersioningConfigurationCondition: &available,
							},
							"s3-backend-3": &v1alpha1.BackendInfo{
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
						Name: "bucket",
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
				providerNames: []string{"s3-backend-1", "s3-backend-2", "s3-backend-3"},
				clients: map[string]backendstore.S3Client{
					"s3-backend-1": nil,
					"s3-backend-2": nil,
					"s3-backend-3": nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						"bucket": {
							"s3-backend-1": &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								VersioningConfigurationCondition: &available,
							},
							"s3-backend-2": &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								VersioningConfigurationCondition: &available,
							},
							"s3-backend-3": &v1alpha1.BackendInfo{
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
						Name: "bucket",
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
				providerNames: []string{"s3-backend-1", "s3-backend-2", "s3-backend-3"},
				clients: map[string]backendstore.S3Client{
					"s3-backend-1": nil,
					"s3-backend-2": nil,
					"s3-backend-3": nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						"bucket": {
							"s3-backend-1": &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								VersioningConfigurationCondition: &available,
							},
							"s3-backend-2": &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								VersioningConfigurationCondition: &available,
							},
							"s3-backend-3": &v1alpha1.BackendInfo{
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
						Name: "bucket",
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
				providerNames: []string{"s3-backend-1", "s3-backend-2", "s3-backend-3"},
				clients: map[string]backendstore.S3Client{
					"s3-backend-1": nil,
					"s3-backend-2": nil,
					"s3-backend-3": nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						"bucket": {
							"s3-backend-1": &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								ObjectLockConfigurationCondition: &available,
							},
							"s3-backend-2": &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								ObjectLockConfigurationCondition: &available,
							},
							"s3-backend-3": &v1alpha1.BackendInfo{
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
						Name: "bucket",
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
				providerNames: []string{"s3-backend-1", "s3-backend-2", "s3-backend-3"},
				clients: map[string]backendstore.S3Client{
					"s3-backend-1": nil,
					"s3-backend-2": nil,
					"s3-backend-3": nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						"bucket": {
							"s3-backend-1": &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								ObjectLockConfigurationCondition: &available,
							},
							"s3-backend-2": &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								ObjectLockConfigurationCondition: &available,
							},
							"s3-backend-3": &v1alpha1.BackendInfo{
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
						Name: "bucket",
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
				providerNames: []string{"s3-backend-1", "s3-backend-2", "s3-backend-3"},
				clients: map[string]backendstore.S3Client{
					"s3-backend-1": nil,
					"s3-backend-2": nil,
					"s3-backend-3": nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						"bucket": {
							"s3-backend-1": &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								ObjectLockConfigurationCondition: &available,
							},
							"s3-backend-2": &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								ObjectLockConfigurationCondition: &available,
							},
							"s3-backend-3": &v1alpha1.BackendInfo{
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
						Name: "bucket",
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
										Status: "Enabled",
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
				providerNames: []string{"s3-backend-1", "s3-backend-2", "s3-backend-3"},
				clients: map[string]backendstore.S3Client{
					"s3-backend-1": nil,
					"s3-backend-2": nil,
					"s3-backend-3": nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						"bucket": {
							"s3-backend-1": &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								LifecycleConfigurationCondition:  &available,
								VersioningConfigurationCondition: &available,
								ObjectLockConfigurationCondition: &available,
							},
							"s3-backend-2": &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								LifecycleConfigurationCondition:  &available,
								VersioningConfigurationCondition: &available,
								ObjectLockConfigurationCondition: &available,
							},
							"s3-backend-3": &v1alpha1.BackendInfo{
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
						Name: "bucket",
						Labels: map[string]string{
							meta.AnnotationKeyReconciliationPaused: "",
						},
					},
					Spec: v1alpha1.BucketSpec{
						ForProvider: v1alpha1.BucketParameters{
							LifecycleConfiguration: &v1alpha1.BucketLifecycleConfiguration{
								Rules: []v1alpha1.LifecycleRule{
									{
										Status: "Enabled",
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
				providerNames: []string{"s3-backend-1", "s3-backend-2", "s3-backend-3"},
				clients: map[string]backendstore.S3Client{
					"s3-backend-1": nil,
					"s3-backend-2": nil,
					"s3-backend-3": nil,
				},
				bucketBackends: &bucketBackends{
					backends: map[string]v1alpha1.Backends{
						"bucket": {
							"s3-backend-1": &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								LifecycleConfigurationCondition:  &available,
								VersioningConfigurationCondition: &available,
								ObjectLockConfigurationCondition: &available,
							},
							"s3-backend-2": &v1alpha1.BackendInfo{
								BucketCondition:                  xpv1.Available(),
								LifecycleConfigurationCondition:  &available,
								VersioningConfigurationCondition: &available,
								ObjectLockConfigurationCondition: &available,
							},
							"s3-backend-3": &v1alpha1.BackendInfo{
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
		tc := tc
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
