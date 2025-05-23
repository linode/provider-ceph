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
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	v1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	"github.com/linode/provider-ceph/internal/backendstore/backendstorefakes"
	"github.com/linode/provider-ceph/internal/controller/s3clienthandler"
	"github.com/linode/provider-ceph/internal/rgw"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	errExternal = errors.New("external error")
	days1       = int32(1)
	days2       = int32(2)
	days3       = int32(3)
)

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
							}, errExternal
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", &fake, nil, apisv1alpha1.HealthStatusHealthy)

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
				err:    errExternal,
			},
		},
		"Attempt to observe lifecycle config on unhealthy backend (consider it NoAction to unblock)": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", &fake, nil, apisv1alpha1.HealthStatusUnhealthy)

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
				status: NoAction,
				err:    nil,
			},
		},
		"Lifecycle config not specified in CR but exists on backend so NeedsDeletion": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{

						GetBucketLifecycleConfigurationStub: func(ctx context.Context, lci *s3.GetBucketLifecycleConfigurationInput, f ...func(*s3.Options)) (*s3.GetBucketLifecycleConfigurationOutput, error) {
							return &s3.GetBucketLifecycleConfigurationOutput{
								Rules: []s3types.LifecycleRule{
									{
										Status: "Enabled",
									},
								},
							}, nil
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", &fake, nil, apisv1alpha1.HealthStatusHealthy)

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
		"Lifecycle config not specified in CR and does exists on backend so NoAction": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{

						GetBucketLifecycleConfigurationStub: func(ctx context.Context, lci *s3.GetBucketLifecycleConfigurationInput, f ...func(*s3.Options)) (*s3.GetBucketLifecycleConfigurationOutput, error) {
							return &s3.GetBucketLifecycleConfigurationOutput{
								Rules: []s3types.LifecycleRule{},
							}, &smithy.GenericAPIError{Code: rgw.LifecycleNotFoundErrCode}
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", &fake, nil, apisv1alpha1.HealthStatusHealthy)

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
				status: NoAction,
				err:    nil,
			},
		},
		"Lifecycle config specified in CR and disabled but exists on backend so NeedsDeletion": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{

						GetBucketLifecycleConfigurationStub: func(ctx context.Context, lci *s3.GetBucketLifecycleConfigurationInput, f ...func(*s3.Options)) (*s3.GetBucketLifecycleConfigurationOutput, error) {
							return &s3.GetBucketLifecycleConfigurationOutput{
								Rules: []s3types.LifecycleRule{
									{
										Status: "Enabled",
									},
								},
							}, nil
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", &fake, nil, apisv1alpha1.HealthStatusHealthy)

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
		"Lifecycle config specified in CR and disabled but does not exist on backend so is NoAction": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{

						GetBucketLifecycleConfigurationStub: func(ctx context.Context, lci *s3.GetBucketLifecycleConfigurationInput, f ...func(*s3.Options)) (*s3.GetBucketLifecycleConfigurationOutput, error) {
							return &s3.GetBucketLifecycleConfigurationOutput{
								Rules: []s3types.LifecycleRule{},
							}, &smithy.GenericAPIError{Code: rgw.LifecycleNotFoundErrCode}
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", &fake, nil, apisv1alpha1.HealthStatusHealthy)

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
				status: NoAction,
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
											Days: &days1,
										},
									},
								},
							}, &smithy.GenericAPIError{Code: rgw.LifecycleNotFoundErrCode}
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", &fake, nil, apisv1alpha1.HealthStatusHealthy)

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
											Days: &days2,
										},
										Filter: &s3types.LifecycleRuleFilterMemberPrefix{},
									},
								},
							}, &smithy.GenericAPIError{Code: rgw.LifecycleNotFoundErrCode}
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", &fake, nil, apisv1alpha1.HealthStatusHealthy)

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
											Days: &days3,
										},
										Filter: &s3types.LifecycleRuleFilterMemberPrefix{},
									},
								},
							}, &smithy.GenericAPIError{Code: rgw.LifecycleNotFoundErrCode}
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", &fake, nil, apisv1alpha1.HealthStatusHealthy)

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
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			c := NewLifecycleConfigurationClient(
				tc.fields.backendStore,
				s3clienthandler.NewHandler(
					s3clienthandler.WithAssumeRoleArn(nil),
					s3clienthandler.WithBackendStore(tc.fields.backendStore)),
				logr.Discard())

			got, err := c.observeBackend(context.Background(), tc.args.bucket, tc.args.backendName)
			require.ErrorIs(t, err, tc.want.err, "unexpected error")
			assert.Equal(t, tc.want.status, got, "unexpected status")
		})
	}
}

//nolint:maintidx // Function requires numerous checks.
func TestHandle(t *testing.T) {
	t.Parallel()
	bucketName := "bucket"
	beName := "s3-backend-1"
	creating := v1.Creating()
	errRandom := errors.New("some error")
	type fields struct {
		backendStore *backendstore.BackendStore
	}

	type args struct {
		bucket      *v1alpha1.Bucket
		backendName string
	}

	type want struct {
		err          error
		specificDiff func(t *testing.T, bb *bucketBackends)
	}

	cases := map[string]struct {
		reason string
		fields fields
		args   args
		want   want
	}{
		"Unhealthy backend": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{}
					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", &fake, nil, apisv1alpha1.HealthStatusUnhealthy)

					return bs
				}(),
			},
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: bucketName,
					},
					Spec: v1alpha1.BucketSpec{
						LifecycleConfigurationDisabled: false,
					},
				},
				backendName: beName,
			},
			want: want{
				err: errUnhealthyBackend,
			},
		},
		"Lifecycle config deletes successfully": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{

						GetBucketLifecycleConfigurationStub: func(ctx context.Context, lci *s3.GetBucketLifecycleConfigurationInput, f ...func(*s3.Options)) (*s3.GetBucketLifecycleConfigurationOutput, error) {
							return &s3.GetBucketLifecycleConfigurationOutput{
								Rules: []s3types.LifecycleRule{
									{
										Status: "Enabled",
									},
								},
							}, nil
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", &fake, nil, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: bucketName,
					},
					Spec: v1alpha1.BucketSpec{
						LifecycleConfigurationDisabled: false,
					},
				},
				backendName: beName,
			},
			want: want{
				err: nil,
				specificDiff: func(t *testing.T, bb *bucketBackends) {
					t.Helper()
					backends := bb.getBackends(bucketName, []string{beName})
					// s3-backend-1 lc config was successfully deleted so was removed from bucketbackends.
					assert.True(t,
						func(bb v1alpha1.Backends) bool {
							return bb[beName].LifecycleConfigurationCondition == nil
						}(backends),
						"s3-backend-1 should not have a lc config condition")
				},
			},
		},
		"Lifecycle config delete fails": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{
						GetBucketLifecycleConfigurationStub: func(ctx context.Context, lci *s3.GetBucketLifecycleConfigurationInput, f ...func(*s3.Options)) (*s3.GetBucketLifecycleConfigurationOutput, error) {
							return &s3.GetBucketLifecycleConfigurationOutput{
								Rules: []s3types.LifecycleRule{
									{
										Status: "Enabled",
									},
								},
							}, nil
						},

						DeleteBucketLifecycleStub: func(ctx context.Context, lci *s3.DeleteBucketLifecycleInput, f ...func(*s3.Options)) (*s3.DeleteBucketLifecycleOutput, error) {
							return &s3.DeleteBucketLifecycleOutput{}, errRandom
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend(beName, &fake, nil, apisv1alpha1.HealthStatusHealthy)

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
				err: errRandom,
				specificDiff: func(t *testing.T, bb *bucketBackends) {
					t.Helper()
					backends := bb.getBackends(bucketName, []string{beName})
					assert.True(t,
						backends[beName].LifecycleConfigurationCondition.Equal(v1.Deleting().
							WithMessage(errors.Wrap(errors.Wrap(errRandom, "failed to delete bucket lifecycle"), errHandleLifecycleConfig).Error())),
						"unexpected lifecycle config condition on s3-backend-1")
				},
			},
		},
		"Lifecycle config is up to date so no action required": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{

						GetBucketLifecycleConfigurationStub: func(ctx context.Context, lci *s3.GetBucketLifecycleConfigurationInput, f ...func(*s3.Options)) (*s3.GetBucketLifecycleConfigurationOutput, error) {
							return &s3.GetBucketLifecycleConfigurationOutput{
								Rules: []s3types.LifecycleRule{},
							}, &smithy.GenericAPIError{Code: rgw.LifecycleNotFoundErrCode}
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", &fake, nil, apisv1alpha1.HealthStatusHealthy)

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
				err: nil,
			},
		},
		"Lifecycle config updates successfully": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{

						GetBucketLifecycleConfigurationStub: func(ctx context.Context, lci *s3.GetBucketLifecycleConfigurationInput, f ...func(*s3.Options)) (*s3.GetBucketLifecycleConfigurationOutput, error) {
							return &s3.GetBucketLifecycleConfigurationOutput{
								Rules: []s3types.LifecycleRule{
									{
										Status: "Enabled",
										Expiration: &s3types.LifecycleExpiration{
											Days: &days2,
										},
										Filter: &s3types.LifecycleRuleFilterMemberPrefix{},
									},
								},
							}, &smithy.GenericAPIError{Code: rgw.LifecycleNotFoundErrCode}
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", &fake, nil, apisv1alpha1.HealthStatusHealthy)

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
				err: nil,
				specificDiff: func(t *testing.T, bb *bucketBackends) {
					t.Helper()
					backends := bb.getBackends(bucketName, []string{beName})
					assert.True(t,
						backends[beName].LifecycleConfigurationCondition.Equal(v1.Available()),
						"unexpected lifecycle config condition on s3-backend-1")
				},
			},
		},
		"Lifecycle config update fails": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{

						GetBucketLifecycleConfigurationStub: func(ctx context.Context, lci *s3.GetBucketLifecycleConfigurationInput, f ...func(*s3.Options)) (*s3.GetBucketLifecycleConfigurationOutput, error) {
							return &s3.GetBucketLifecycleConfigurationOutput{
								Rules: []s3types.LifecycleRule{
									{
										Status: "Enabled",
										Expiration: &s3types.LifecycleExpiration{
											Days: &days2,
										},
										Filter: &s3types.LifecycleRuleFilterMemberPrefix{},
									},
								},
							}, &smithy.GenericAPIError{Code: rgw.LifecycleNotFoundErrCode}
						},
						PutBucketLifecycleConfigurationStub: func(ctx context.Context, lci *s3.PutBucketLifecycleConfigurationInput, f ...func(*s3.Options)) (*s3.PutBucketLifecycleConfigurationOutput, error) {
							return &s3.PutBucketLifecycleConfigurationOutput{}, errRandom
						},
					}
					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", &fake, nil, apisv1alpha1.HealthStatusHealthy)

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
				err: errRandom,
				specificDiff: func(t *testing.T, bb *bucketBackends) {
					t.Helper()
					backends := bb.getBackends(bucketName, []string{beName})
					assert.True(t,
						backends[beName].LifecycleConfigurationCondition.Equal(v1.Unavailable().
							WithMessage(errors.Wrap(errors.Wrap(errRandom, "failed to put bucket lifecycle configuration"), errHandleLifecycleConfig).Error())),
						"unexpected lifecycle config condition on s3-backend-1")
				},
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			c := NewLifecycleConfigurationClient(
				tc.fields.backendStore,
				s3clienthandler.NewHandler(
					s3clienthandler.WithAssumeRoleArn(nil),
					s3clienthandler.WithBackendStore(tc.fields.backendStore)),
				logr.Discard())

			bb := newBucketBackends()
			bb.setLifecycleConfigCondition(bucketName, beName, &creating)

			err := c.Handle(context.Background(), tc.args.bucket, tc.args.backendName, bb)
			require.ErrorIs(t, err, tc.want.err, "unexpected error")
			if tc.want.specificDiff != nil {
				tc.want.specificDiff(t, bb)
			}
		})
	}
}
