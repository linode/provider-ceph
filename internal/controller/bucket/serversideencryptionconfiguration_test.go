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
	v1 "github.com/crossplane/crossplane-runtime/v2/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	"github.com/linode/provider-ceph/internal/backendstore/backendstorefakes"
	"github.com/linode/provider-ceph/internal/consts"
	"github.com/linode/provider-ceph/internal/controller/s3clienthandler"
	"github.com/linode/provider-ceph/internal/rgw"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const aes256Str = "AES256"

//nolint:maintidx // Function requires numerous checks.
func TestSSEConfigObserveBackend(t *testing.T) {
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
		"External error getting sse config": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{

						GetBucketEncryptionStub: func(ctx context.Context, lci *s3.GetBucketEncryptionInput, f ...func(*s3.Options)) (*s3.GetBucketEncryptionOutput, error) {
							return &s3.GetBucketEncryptionOutput{
								ServerSideEncryptionConfiguration: &s3types.ServerSideEncryptionConfiguration{
									Rules: []s3types.ServerSideEncryptionRule{},
								},
							}, errExternal
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend(consts.S3Backend1, &fake, nil, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: consts.TestBucket,
					},
				},
				backendName: consts.S3Backend1,
			},
			want: want{
				status: NeedsUpdate,
				err:    errExternal,
			},
		},
		"Attempt to observe sse config on unhealthy backend (consider it NoAction to unblock)": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend(consts.S3Backend1, &fake, nil, apisv1alpha1.HealthStatusUnhealthy)

					return bs
				}(),
			},
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: consts.TestBucket,
					},
				},
				backendName: consts.S3Backend1,
			},
			want: want{
				status: NoAction,
				err:    nil,
			},
		},
		"SSE config not specified in CR but exists on backend so NeedsDeletion": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{

						GetBucketEncryptionStub: func(ctx context.Context, lci *s3.GetBucketEncryptionInput, f ...func(*s3.Options)) (*s3.GetBucketEncryptionOutput, error) {
							return &s3.GetBucketEncryptionOutput{
								ServerSideEncryptionConfiguration: &s3types.ServerSideEncryptionConfiguration{
									Rules: []s3types.ServerSideEncryptionRule{
										{
											ApplyServerSideEncryptionByDefault: &s3types.ServerSideEncryptionByDefault{
												SSEAlgorithm: aes256Str,
											},
										},
									},
								},
							}, nil
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend(consts.S3Backend1, &fake, nil, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: consts.TestBucket,
					},
					Spec: v1alpha1.BucketSpec{
						ServerSideEncryptionConfigurationDisabled: false,
					},
				},
				backendName: consts.S3Backend1,
			},
			want: want{
				status: NeedsDeletion,
				err:    nil,
			},
		},
		"SSE config not specified in CR and does not exist on backend so NoAction": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{
						GetBucketEncryptionStub: func(ctx context.Context, lci *s3.GetBucketEncryptionInput, f ...func(*s3.Options)) (*s3.GetBucketEncryptionOutput, error) {
							return &s3.GetBucketEncryptionOutput{
								ServerSideEncryptionConfiguration: &s3types.ServerSideEncryptionConfiguration{
									Rules: []s3types.ServerSideEncryptionRule{},
								},
							}, &smithy.GenericAPIError{Code: rgw.ServerSideEncryptionConfigurationNotFoundErrCode}
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend(consts.S3Backend1, &fake, nil, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: consts.TestBucket,
					},
					Spec: v1alpha1.BucketSpec{
						ServerSideEncryptionConfigurationDisabled: false,
					},
				},
				backendName: consts.S3Backend1,
			},
			want: want{
				status: NoAction,
				err:    nil,
			},
		},
		"SSE config specified in CR and disabled but exists on backend so NeedsDeletion": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{
						GetBucketEncryptionStub: func(ctx context.Context, lci *s3.GetBucketEncryptionInput, f ...func(*s3.Options)) (*s3.GetBucketEncryptionOutput, error) {
							return &s3.GetBucketEncryptionOutput{
								ServerSideEncryptionConfiguration: &s3types.ServerSideEncryptionConfiguration{
									Rules: []s3types.ServerSideEncryptionRule{
										{
											ApplyServerSideEncryptionByDefault: &s3types.ServerSideEncryptionByDefault{
												SSEAlgorithm: s3types.ServerSideEncryptionAes256,
											},
										},
									},
								},
							}, nil
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend(consts.S3Backend1, &fake, nil, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: consts.TestBucket,
					},
					Spec: v1alpha1.BucketSpec{
						ServerSideEncryptionConfigurationDisabled: true,
						ForProvider: v1alpha1.BucketParameters{
							ServerSideEncryptionConfiguration: &v1alpha1.ServerSideEncryptionConfiguration{
								Rules: []v1alpha1.ServerSideEncryptionRule{
									{
										ApplyServerSideEncryptionByDefault: v1alpha1.ServerSideEncryptionByDefault{
											SSEAlgorithm: aes256Str,
										},
									},
								},
							},
						},
					},
				},
				backendName: consts.S3Backend1,
			},
			want: want{
				status: NeedsDeletion,
				err:    nil,
			},
		},
		"SSE config specified in CR and disabled but does not exist on backend so is NoAction": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{
						GetBucketEncryptionStub: func(ctx context.Context, lci *s3.GetBucketEncryptionInput, f ...func(*s3.Options)) (*s3.GetBucketEncryptionOutput, error) {
							return &s3.GetBucketEncryptionOutput{
								ServerSideEncryptionConfiguration: &s3types.ServerSideEncryptionConfiguration{
									Rules: []s3types.ServerSideEncryptionRule{},
								},
							}, &smithy.GenericAPIError{Code: rgw.ServerSideEncryptionConfigurationNotFoundErrCode}
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend(consts.S3Backend1, &fake, nil, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: consts.TestBucket,
					},
					Spec: v1alpha1.BucketSpec{
						ServerSideEncryptionConfigurationDisabled: true,
						ForProvider: v1alpha1.BucketParameters{
							ServerSideEncryptionConfiguration: &v1alpha1.ServerSideEncryptionConfiguration{
								Rules: []v1alpha1.ServerSideEncryptionRule{
									{
										ApplyServerSideEncryptionByDefault: v1alpha1.ServerSideEncryptionByDefault{
											SSEAlgorithm: aes256Str,
										},
									},
								},
							},
						},
					},
				},
				backendName: consts.S3Backend1,
			},
			want: want{
				status: NoAction,
				err:    nil,
			},
		},
		"SSE config has no rules in CR and is enabled but has rules on backend so NeedsDeletion": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{
						GetBucketEncryptionStub: func(ctx context.Context, lci *s3.GetBucketEncryptionInput, f ...func(*s3.Options)) (*s3.GetBucketEncryptionOutput, error) {
							return &s3.GetBucketEncryptionOutput{
								ServerSideEncryptionConfiguration: &s3types.ServerSideEncryptionConfiguration{
									Rules: []s3types.ServerSideEncryptionRule{
										{
											ApplyServerSideEncryptionByDefault: &s3types.ServerSideEncryptionByDefault{
												SSEAlgorithm: s3types.ServerSideEncryptionAes256,
											},
										},
									},
								},
							}, nil
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend(consts.S3Backend1, &fake, nil, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: consts.TestBucket,
					},
					Spec: v1alpha1.BucketSpec{
						ServerSideEncryptionConfigurationDisabled: false,
						ForProvider: v1alpha1.BucketParameters{
							ServerSideEncryptionConfiguration: &v1alpha1.ServerSideEncryptionConfiguration{
								Rules: []v1alpha1.ServerSideEncryptionRule{},
							},
						},
					},
				},
				backendName: consts.S3Backend1,
			},
			want: want{
				status: NeedsDeletion,
				err:    nil,
			},
		},
		"SSE config has rules in CR and is enabled but has different rules on backend so NeedsUpdate": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{
						GetBucketEncryptionStub: func(ctx context.Context, lci *s3.GetBucketEncryptionInput, f ...func(*s3.Options)) (*s3.GetBucketEncryptionOutput, error) {
							return &s3.GetBucketEncryptionOutput{
								ServerSideEncryptionConfiguration: &s3types.ServerSideEncryptionConfiguration{
									Rules: []s3types.ServerSideEncryptionRule{
										{
											ApplyServerSideEncryptionByDefault: &s3types.ServerSideEncryptionByDefault{
												SSEAlgorithm: s3types.ServerSideEncryptionAes256,
											},
										},
									},
								},
							}, nil
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend(consts.S3Backend1, &fake, nil, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: consts.TestBucket,
					},
					Spec: v1alpha1.BucketSpec{
						ServerSideEncryptionConfigurationDisabled: false,
						ForProvider: v1alpha1.BucketParameters{
							ServerSideEncryptionConfiguration: &v1alpha1.ServerSideEncryptionConfiguration{
								Rules: []v1alpha1.ServerSideEncryptionRule{
									{
										ApplyServerSideEncryptionByDefault: v1alpha1.ServerSideEncryptionByDefault{
											SSEAlgorithm: "aws:kms",
										},
									},
								},
							},
						},
					},
				},
				backendName: consts.S3Backend1,
			},
			want: want{
				status: NeedsUpdate,
				err:    nil,
			},
		},
		"SSE config has rules in CR and is enabled and has same rules on backend so is Updated": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{
						GetBucketEncryptionStub: func(ctx context.Context, lci *s3.GetBucketEncryptionInput, f ...func(*s3.Options)) (*s3.GetBucketEncryptionOutput, error) {
							return &s3.GetBucketEncryptionOutput{
								ServerSideEncryptionConfiguration: &s3types.ServerSideEncryptionConfiguration{
									Rules: []s3types.ServerSideEncryptionRule{
										{
											ApplyServerSideEncryptionByDefault: &s3types.ServerSideEncryptionByDefault{
												SSEAlgorithm: s3types.ServerSideEncryptionAes256,
											},
										},
									},
								},
							}, nil
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend(consts.S3Backend1, &fake, nil, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: consts.TestBucket,
					},
					Spec: v1alpha1.BucketSpec{
						ServerSideEncryptionConfigurationDisabled: false,
						ForProvider: v1alpha1.BucketParameters{
							ServerSideEncryptionConfiguration: &v1alpha1.ServerSideEncryptionConfiguration{
								Rules: []v1alpha1.ServerSideEncryptionRule{
									{
										ApplyServerSideEncryptionByDefault: v1alpha1.ServerSideEncryptionByDefault{
											SSEAlgorithm: aes256Str,
										},
									},
								},
							},
						},
					},
				},
				backendName: consts.S3Backend1,
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

			c := NewServerSideEncryptionConfigurationClient(
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
func TestSSEConfigHandle(t *testing.T) {
	t.Parallel()
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
					bs.AddOrUpdateBackend(consts.S3Backend1, &fake, nil, apisv1alpha1.HealthStatusUnhealthy)

					return bs
				}(),
			},
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: consts.TestBucket,
					},
					Spec: v1alpha1.BucketSpec{
						ServerSideEncryptionConfigurationDisabled: false,
					},
				},
				backendName: consts.S3Backend1,
			},
			want: want{
				err: errUnhealthyBackend,
			},
		},
		"SSE config deletes successfully": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{
						GetBucketEncryptionStub: func(ctx context.Context, lci *s3.GetBucketEncryptionInput, f ...func(*s3.Options)) (*s3.GetBucketEncryptionOutput, error) {
							return &s3.GetBucketEncryptionOutput{
								ServerSideEncryptionConfiguration: &s3types.ServerSideEncryptionConfiguration{
									Rules: []s3types.ServerSideEncryptionRule{
										{
											ApplyServerSideEncryptionByDefault: &s3types.ServerSideEncryptionByDefault{
												SSEAlgorithm: s3types.ServerSideEncryptionAes256,
											},
										},
									},
								},
							}, nil
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend(consts.S3Backend1, &fake, nil, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: consts.TestBucket,
					},
					Spec: v1alpha1.BucketSpec{
						ServerSideEncryptionConfigurationDisabled: false,
					},
				},
				backendName: consts.S3Backend1,
			},
			want: want{
				err: nil,
				specificDiff: func(t *testing.T, bb *bucketBackends) {
					t.Helper()
					backends := bb.getBackends(consts.TestBucket, []string{consts.S3Backend1})
					// s3-backend-1 sse config was successfully deleted so was removed from bucketbackends.
					assert.True(t,
						func(bb v1alpha1.Backends) bool {
							return bb[consts.S3Backend1].ServerSideEncryptionConfigurationCondition == nil
						}(backends),
						"s3-backend-1 should not have a sse config condition")
				},
			},
		},
		"SSE config delete fails": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{
						GetBucketEncryptionStub: func(ctx context.Context, lci *s3.GetBucketEncryptionInput, f ...func(*s3.Options)) (*s3.GetBucketEncryptionOutput, error) {
							return &s3.GetBucketEncryptionOutput{
								ServerSideEncryptionConfiguration: &s3types.ServerSideEncryptionConfiguration{
									Rules: []s3types.ServerSideEncryptionRule{
										{
											ApplyServerSideEncryptionByDefault: &s3types.ServerSideEncryptionByDefault{
												SSEAlgorithm: s3types.ServerSideEncryptionAes256,
											},
										},
									},
								},
							}, nil
						},

						DeleteBucketEncryptionStub: func(ctx context.Context, lci *s3.DeleteBucketEncryptionInput, f ...func(*s3.Options)) (*s3.DeleteBucketEncryptionOutput, error) {
							return &s3.DeleteBucketEncryptionOutput{}, errRandom
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend(consts.S3Backend1, &fake, nil, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: consts.TestBucket,
					},
					Spec: v1alpha1.BucketSpec{
						ServerSideEncryptionConfigurationDisabled: false,
					},
				},
				backendName: consts.S3Backend1,
			},
			want: want{
				err: errRandom,
				specificDiff: func(t *testing.T, bb *bucketBackends) {
					t.Helper()
					backends := bb.getBackends(consts.TestBucket, []string{consts.S3Backend1})
					assert.True(t,
						backends[consts.S3Backend1].ServerSideEncryptionConfigurationCondition.Equal(v1.Deleting().
							WithMessage(errors.Wrap(errors.Wrap(errRandom, "failed to delete bucket encryption"), errHandleSSEConfig).Error())),
						"unexpected sse config condition on s3-backend-1")
				},
			},
		},
		"SSE config is not found and disabled on CR so no action required": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{
						GetBucketEncryptionStub: func(ctx context.Context, lci *s3.GetBucketEncryptionInput, f ...func(*s3.Options)) (*s3.GetBucketEncryptionOutput, error) {
							return &s3.GetBucketEncryptionOutput{
								ServerSideEncryptionConfiguration: &s3types.ServerSideEncryptionConfiguration{
									Rules: []s3types.ServerSideEncryptionRule{},
								},
							}, &smithy.GenericAPIError{Code: rgw.ServerSideEncryptionConfigurationNotFoundErrCode}
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend(consts.S3Backend1, &fake, nil, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: consts.TestBucket,
					},
					Spec: v1alpha1.BucketSpec{
						ServerSideEncryptionConfigurationDisabled: true,
						ForProvider: v1alpha1.BucketParameters{
							ServerSideEncryptionConfiguration: &v1alpha1.ServerSideEncryptionConfiguration{
								Rules: []v1alpha1.ServerSideEncryptionRule{
									{
										ApplyServerSideEncryptionByDefault: v1alpha1.ServerSideEncryptionByDefault{
											SSEAlgorithm: aes256Str,
										},
									},
								},
							},
						},
					},
				},
				backendName: consts.S3Backend1,
			},
			want: want{
				err: nil,
			},
		},
		"SSE config updates successfully": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{
						GetBucketEncryptionStub: func(ctx context.Context, lci *s3.GetBucketEncryptionInput, f ...func(*s3.Options)) (*s3.GetBucketEncryptionOutput, error) {
							return &s3.GetBucketEncryptionOutput{
								ServerSideEncryptionConfiguration: &s3types.ServerSideEncryptionConfiguration{
									Rules: []s3types.ServerSideEncryptionRule{
										{
											ApplyServerSideEncryptionByDefault: &s3types.ServerSideEncryptionByDefault{
												SSEAlgorithm: s3types.ServerSideEncryptionAes256,
											},
										},
									},
								},
							}, nil
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend(consts.S3Backend1, &fake, nil, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: consts.TestBucket,
					},
					Spec: v1alpha1.BucketSpec{
						ServerSideEncryptionConfigurationDisabled: false,
						ForProvider: v1alpha1.BucketParameters{
							ServerSideEncryptionConfiguration: &v1alpha1.ServerSideEncryptionConfiguration{
								Rules: []v1alpha1.ServerSideEncryptionRule{
									{
										ApplyServerSideEncryptionByDefault: v1alpha1.ServerSideEncryptionByDefault{
											SSEAlgorithm: aes256Str,
										},
									},
								},
							},
						},
					},
				},
				backendName: consts.S3Backend1,
			},
			want: want{
				err: nil,
				specificDiff: func(t *testing.T, bb *bucketBackends) {
					t.Helper()
					backends := bb.getBackends(consts.TestBucket, []string{consts.S3Backend1})
					assert.True(t,
						backends[consts.S3Backend1].ServerSideEncryptionConfigurationCondition.Equal(v1.Available()),
						"unexpected SSE config condition on s3-backend-1")
				},
			},
		},
		"SSE config update fails": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{

						GetBucketEncryptionStub: func(ctx context.Context, lci *s3.GetBucketEncryptionInput, f ...func(*s3.Options)) (*s3.GetBucketEncryptionOutput, error) {
							return &s3.GetBucketEncryptionOutput{
								ServerSideEncryptionConfiguration: &s3types.ServerSideEncryptionConfiguration{
									Rules: []s3types.ServerSideEncryptionRule{
										{
											ApplyServerSideEncryptionByDefault: &s3types.ServerSideEncryptionByDefault{
												SSEAlgorithm: s3types.ServerSideEncryptionAes256,
											},
										},
									},
								},
							}, nil
						},
						PutBucketEncryptionStub: func(ctx context.Context, lci *s3.PutBucketEncryptionInput, f ...func(*s3.Options)) (*s3.PutBucketEncryptionOutput, error) {
							return &s3.PutBucketEncryptionOutput{}, errRandom
						},
					}
					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend(consts.S3Backend1, &fake, nil, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: consts.TestBucket,
					},
					Spec: v1alpha1.BucketSpec{
						ServerSideEncryptionConfigurationDisabled: false,
						ForProvider: v1alpha1.BucketParameters{
							ServerSideEncryptionConfiguration: &v1alpha1.ServerSideEncryptionConfiguration{
								Rules: []v1alpha1.ServerSideEncryptionRule{
									{
										ApplyServerSideEncryptionByDefault: v1alpha1.ServerSideEncryptionByDefault{
											SSEAlgorithm: "aws:kms",
										},
									},
								},
							},
						},
					},
				},
				backendName: consts.S3Backend1,
			},
			want: want{
				err: errRandom,
				specificDiff: func(t *testing.T, bb *bucketBackends) {
					t.Helper()
					backends := bb.getBackends(consts.TestBucket, []string{consts.S3Backend1})
					assert.True(t,
						backends[consts.S3Backend1].ServerSideEncryptionConfigurationCondition.Equal(v1.Unavailable().
							WithMessage(errors.Wrap(errors.Wrap(errRandom, "failed to put bucket encryption"), errHandleSSEConfig).Error())),
						"unexpected SSE config condition on s3-backend-1")
				},
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			c := NewServerSideEncryptionConfigurationClient(
				tc.fields.backendStore,
				s3clienthandler.NewHandler(
					s3clienthandler.WithAssumeRoleArn(nil),
					s3clienthandler.WithBackendStore(tc.fields.backendStore)),
				logr.Discard())

			bb := newBucketBackends()
			bb.setSSEConfigCondition(consts.TestBucket, consts.S3Backend1, &creating)

			err := c.Handle(context.Background(), tc.args.bucket, tc.args.backendName, bb)
			require.ErrorIs(t, err, tc.want.err, "unexpected error")
			if tc.want.specificDiff != nil {
				tc.want.specificDiff(t, bb)
			}
		})
	}
}
