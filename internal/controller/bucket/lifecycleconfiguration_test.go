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
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/pkg/errors"

	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/google/go-cmp/cmp"
	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	"github.com/linode/provider-ceph/internal/backendstore/backendstorefakes"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	errExternal = "external error"
	days1       = int32(1)
	days3       = int32(3)
)

// Unlike many Kubernetes projects Crossplane does not use third party testing
// libraries, per the common Go test review comments. Crossplane encourages the
// use of table driven unit tests. The tests of the crossplane-runtime project
// are representative of the testing style Crossplane encourages.
//
// https://github.com/golang/go/wiki/TestComments
// https://github.com/crossplane/crossplane/blob/master/CONTRIBUTING.md#contributing-code

//nolint:maintidx // Function requires numerous checks.
func TestObserveBackend(t *testing.T) {
	t.Parallel()

	type fields struct {
		backendStore *backendstore.BackendStore
	}

	type args struct {
		bucket      *v1alpha1.Bucket
		backendName string
	}

	type want struct {
		status ResourceStatus
		err    error
	}

	cases := map[string]struct {
		reason string
		fields fields
		args   args
		want   want
	}{
		"External error getting lifecycle config": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{

						GetBucketLifecycleConfigurationStub: func(ctx context.Context, lci *s3.GetBucketLifecycleConfigurationInput, f ...func(*s3.Options)) (*s3.GetBucketLifecycleConfigurationOutput, error) {
							return &s3.GetBucketLifecycleConfigurationOutput{
								Rules: []s3types.LifecycleRule{},
							}, errors.New(errExternal)
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", &fake, true, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: "bucket",
					},
				},
				backendName: "s3-backend-1",
			},
			want: want{
				status: NeedsUpdate,
				err:    errors.Wrap(errors.New(errExternal), errGetLifecycleConfig),
			},
		},
		"Lifecycle config not specified in CR but exists on backend so NeedsDeletion": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{

						GetBucketLifecycleConfigurationStub: func(ctx context.Context, lci *s3.GetBucketLifecycleConfigurationInput, f ...func(*s3.Options)) (*s3.GetBucketLifecycleConfigurationOutput, error) {
							return &s3.GetBucketLifecycleConfigurationOutput{
								Rules: []s3types.LifecycleRule{},
							}, nil
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", &fake, true, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: "bucket",
					},
					Spec: v1alpha1.BucketSpec{
						LifecycleConfigurationDisabled: false,
					},
				},
				backendName: "s3-backend-1",
			},
			want: want{
				status: NeedsDeletion,
				err:    nil,
			},
		},
		"Lifecycle config not specified in CR and does exists on backend so is Updated": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{

						GetBucketLifecycleConfigurationStub: func(ctx context.Context, lci *s3.GetBucketLifecycleConfigurationInput, f ...func(*s3.Options)) (*s3.GetBucketLifecycleConfigurationOutput, error) {
							return &s3.GetBucketLifecycleConfigurationOutput{
								Rules: []s3types.LifecycleRule{},
							}, &smithy.GenericAPIError{Code: LifecycleNotFoundErrCode}
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", &fake, true, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: "bucket",
					},
					Spec: v1alpha1.BucketSpec{
						LifecycleConfigurationDisabled: false,
					},
				},
				backendName: "s3-backend-1",
			},
			want: want{
				status: Updated,
				err:    nil,
			},
		},
		"Lifecycle config specified in CR and disabled but exists on backend so NeedsDeletion": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{

						GetBucketLifecycleConfigurationStub: func(ctx context.Context, lci *s3.GetBucketLifecycleConfigurationInput, f ...func(*s3.Options)) (*s3.GetBucketLifecycleConfigurationOutput, error) {
							return &s3.GetBucketLifecycleConfigurationOutput{
								Rules: []s3types.LifecycleRule{},
							}, nil
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", &fake, true, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: "bucket",
					},
					Spec: v1alpha1.BucketSpec{
						LifecycleConfigurationDisabled: true,
						ForProvider: v1alpha1.BucketParameters{
							LifecycleConfiguration: &v1alpha1.BucketLifecycleConfiguration{
								Rules: []v1alpha1.LifecycleRule{
									{
										Expiration: &v1alpha1.LifecycleExpiration{
											Days: &days1,
										},
									},
								},
							},
						},
					},
				},
				backendName: "s3-backend-1",
			},
			want: want{
				status: NeedsDeletion,
				err:    nil,
			},
		},
		"Lifecycle config specified in CR and disabled but does not exist on backend so is Updated": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{

						GetBucketLifecycleConfigurationStub: func(ctx context.Context, lci *s3.GetBucketLifecycleConfigurationInput, f ...func(*s3.Options)) (*s3.GetBucketLifecycleConfigurationOutput, error) {
							return &s3.GetBucketLifecycleConfigurationOutput{
								Rules: []s3types.LifecycleRule{},
							}, &smithy.GenericAPIError{Code: LifecycleNotFoundErrCode}
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", &fake, true, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: "bucket",
					},
					Spec: v1alpha1.BucketSpec{
						LifecycleConfigurationDisabled: true,
						ForProvider: v1alpha1.BucketParameters{
							LifecycleConfiguration: &v1alpha1.BucketLifecycleConfiguration{
								Rules: []v1alpha1.LifecycleRule{
									{
										Expiration: &v1alpha1.LifecycleExpiration{
											Days: &days1,
										},
									},
								},
							},
						},
					},
				},
				backendName: "s3-backend-1",
			},
			want: want{
				status: Updated,
				err:    nil,
			},
		},
		"Lifecycle config has no rules in CR and is enabled but has rules on backend so NeedsDeletion": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{

						GetBucketLifecycleConfigurationStub: func(ctx context.Context, lci *s3.GetBucketLifecycleConfigurationInput, f ...func(*s3.Options)) (*s3.GetBucketLifecycleConfigurationOutput, error) {
							return &s3.GetBucketLifecycleConfigurationOutput{
								Rules: []s3types.LifecycleRule{
									{
										Expiration: &s3types.LifecycleExpiration{
											Days: days1,
										},
									},
								},
							}, &smithy.GenericAPIError{Code: LifecycleNotFoundErrCode}
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", &fake, true, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: "bucket",
					},
					Spec: v1alpha1.BucketSpec{
						LifecycleConfigurationDisabled: false,
						ForProvider: v1alpha1.BucketParameters{
							LifecycleConfiguration: &v1alpha1.BucketLifecycleConfiguration{
								Rules: []v1alpha1.LifecycleRule{},
							},
						},
					},
				},
				backendName: "s3-backend-1",
			},
			want: want{
				status: NeedsDeletion,
				err:    nil,
			},
		},
		"Lifecycle config has rules in CR and is enabled but has different rules on backend so NeedsUpdate": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{

						GetBucketLifecycleConfigurationStub: func(ctx context.Context, lci *s3.GetBucketLifecycleConfigurationInput, f ...func(*s3.Options)) (*s3.GetBucketLifecycleConfigurationOutput, error) {
							return &s3.GetBucketLifecycleConfigurationOutput{
								Rules: []s3types.LifecycleRule{
									{
										Status: "Enabled",
										Expiration: &s3types.LifecycleExpiration{
											Days: int32(2),
										},
										Filter: &s3types.LifecycleRuleFilterMemberPrefix{},
									},
								},
							}, &smithy.GenericAPIError{Code: LifecycleNotFoundErrCode}
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", &fake, true, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: "bucket",
					},
					Spec: v1alpha1.BucketSpec{
						LifecycleConfigurationDisabled: false,
						ForProvider: v1alpha1.BucketParameters{
							LifecycleConfiguration: &v1alpha1.BucketLifecycleConfiguration{
								Rules: []v1alpha1.LifecycleRule{
									{
										Status: "Enabled",
										Expiration: &v1alpha1.LifecycleExpiration{
											Days: &days1,
										},
									},
								},
							},
						},
					},
				},
				backendName: "s3-backend-1",
			},
			want: want{
				status: NeedsUpdate,
				err:    nil,
			},
		},
		"Lifecycle config has rules in CR and is enabled and has same rules on backend so is Updated": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{

						GetBucketLifecycleConfigurationStub: func(ctx context.Context, lci *s3.GetBucketLifecycleConfigurationInput, f ...func(*s3.Options)) (*s3.GetBucketLifecycleConfigurationOutput, error) {
							return &s3.GetBucketLifecycleConfigurationOutput{
								Rules: []s3types.LifecycleRule{
									{
										Expiration: &s3types.LifecycleExpiration{
											Days: int32(3),
										},
										Filter: &s3types.LifecycleRuleFilterMemberPrefix{},
									},
								},
							}, &smithy.GenericAPIError{Code: LifecycleNotFoundErrCode}
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", &fake, true, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: "bucket",
					},
					Spec: v1alpha1.BucketSpec{
						LifecycleConfigurationDisabled: false,
						ForProvider: v1alpha1.BucketParameters{
							LifecycleConfiguration: &v1alpha1.BucketLifecycleConfiguration{
								Rules: []v1alpha1.LifecycleRule{
									{
										Expiration: &v1alpha1.LifecycleExpiration{
											Days: &days3,
										},
									},
								},
							},
						},
					},
				},
				backendName: "s3-backend-1",
			},
			want: want{
				status: Updated,
				err:    nil,
			},
		},
	}
	for name, tc := range cases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			c := NewLifecycleConfigurationClient(tc.fields.backendStore, logging.NewNopLogger())
			got, err := c.observeBackend(context.Background(), tc.args.bucket, tc.args.backendName)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\ne.observeBackend(...): -want error, +got error:\n%s\n", tc.reason, diff)
			}
			if diff := cmp.Diff(tc.want.status, got); diff != "" {
				t.Errorf("\n%s\ne.observeBackend(...): -want, +got:\n%s\n", tc.reason, diff)
			}
		})
	}
}
