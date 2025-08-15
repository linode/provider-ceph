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
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	v1 "github.com/crossplane/crossplane-runtime/v2/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	"github.com/linode/provider-ceph/internal/backendstore/backendstorefakes"
	"github.com/linode/provider-ceph/internal/controller/s3clienthandler"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var objLockEnabled = v1alpha1.ObjectLockEnabledEnabled

func TestObjectLockConfigObserveBackend(t *testing.T) {
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
		"External error getting object lock": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{

						GetObjectLockConfigurationStub: func(ctx context.Context, lci *s3.GetObjectLockConfigurationInput, f ...func(*s3.Options)) (*s3.GetObjectLockConfigurationOutput, error) {
							return &s3.GetObjectLockConfigurationOutput{}, errExternal
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
						ForProvider: v1alpha1.BucketParameters{
							ObjectLockConfiguration: &v1alpha1.ObjectLockConfiguration{
								ObjectLockEnabled: &objLockEnabled,
							},
						},
					},
				},
				backendName: "s3-backend-1",
			},
			want: want{
				status: NeedsUpdate,
				err:    errExternal,
			},
		},
		"Object lock config specified in CR and exists on backend and is the same so is Updated": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{

						GetObjectLockConfigurationStub: func(ctx context.Context, lci *s3.GetObjectLockConfigurationInput, f ...func(*s3.Options)) (*s3.GetObjectLockConfigurationOutput, error) {
							return &s3.GetObjectLockConfigurationOutput{
								ObjectLockConfiguration: &s3types.ObjectLockConfiguration{
									ObjectLockEnabled: s3types.ObjectLockEnabledEnabled,
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
						ForProvider: v1alpha1.BucketParameters{
							ObjectLockConfiguration: &v1alpha1.ObjectLockConfiguration{
								ObjectLockEnabled: &objLockEnabled,
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
		"Object lock config specified in CR and exists on backend but is different so is NeedsUpdate": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{

						GetObjectLockConfigurationStub: func(ctx context.Context, lci *s3.GetObjectLockConfigurationInput, f ...func(*s3.Options)) (*s3.GetObjectLockConfigurationOutput, error) {
							return &s3.GetObjectLockConfigurationOutput{
								ObjectLockConfiguration: &s3types.ObjectLockConfiguration{
									ObjectLockEnabled: s3types.ObjectLockEnabledEnabled,
									Rule: &s3types.ObjectLockRule{
										DefaultRetention: &s3types.DefaultRetention{
											Mode: s3types.ObjectLockRetentionModeCompliance,
										},
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
						ForProvider: v1alpha1.BucketParameters{
							ObjectLockConfiguration: &v1alpha1.ObjectLockConfiguration{
								ObjectLockEnabled: &objLockEnabled,
								Rule: &v1alpha1.ObjectLockRule{
									DefaultRetention: &v1alpha1.DefaultRetention{
										Mode: v1alpha1.ModeGovernance,
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
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			c := NewObjectLockConfigurationClient(
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

//nolint:maintidx //Test with lots of cases.
func TestObjectLockConfigurationHandle(t *testing.T) {
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
					bs.AddOrUpdateBackend(beName, &fake, nil, apisv1alpha1.HealthStatusUnhealthy)

					return bs
				}(),
			},
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: bucketName,
					},
					Spec: v1alpha1.BucketSpec{
						ForProvider: v1alpha1.BucketParameters{
							ObjectLockEnabledForBucket: &enabledTrue,
							ObjectLockConfiguration: &v1alpha1.ObjectLockConfiguration{
								ObjectLockEnabled: &objLockEnabled,
								Rule: &v1alpha1.ObjectLockRule{
									DefaultRetention: &v1alpha1.DefaultRetention{
										Mode: v1alpha1.ModeGovernance,
									},
								},
							},
						},
					},
				},
				backendName: beName,
			},
			want: want{
				err: errUnhealthyBackend,
			},
		},
		"Object lock is not enabled for Bucket CR - nil value": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{}

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
						ForProvider: v1alpha1.BucketParameters{},
					},
				},
				backendName: "s3-backend-1",
			},
			want: want{
				err: nil,
			},
		},
		"Object lock is not enabled for Bucket CR - false": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{}

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
						ForProvider: v1alpha1.BucketParameters{
							ObjectLockEnabledForBucket: &enabledFalse,
						},
					},
				},
				backendName: "s3-backend-1",
			},
			want: want{
				err: nil,
			},
		},
		"Object lock config is up to date so no action required": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{
						GetObjectLockConfigurationStub: func(ctx context.Context, lci *s3.GetObjectLockConfigurationInput, f ...func(*s3.Options)) (*s3.GetObjectLockConfigurationOutput, error) {
							return &s3.GetObjectLockConfigurationOutput{
								ObjectLockConfiguration: &s3types.ObjectLockConfiguration{
									ObjectLockEnabled: s3types.ObjectLockEnabledEnabled,
									Rule: &s3types.ObjectLockRule{
										DefaultRetention: &s3types.DefaultRetention{
											Mode: s3types.ObjectLockRetentionModeCompliance,
										},
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
						ForProvider: v1alpha1.BucketParameters{
							ObjectLockConfiguration: &v1alpha1.ObjectLockConfiguration{
								ObjectLockEnabled: &objLockEnabled,
								Rule: &v1alpha1.ObjectLockRule{
									DefaultRetention: &v1alpha1.DefaultRetention{
										Mode: v1alpha1.ModeCompliance,
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
		"Object lock config updates successfully": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{
						GetObjectLockConfigurationStub: func(ctx context.Context, lci *s3.GetObjectLockConfigurationInput, f ...func(*s3.Options)) (*s3.GetObjectLockConfigurationOutput, error) {
							return &s3.GetObjectLockConfigurationOutput{
								ObjectLockConfiguration: &s3types.ObjectLockConfiguration{
									ObjectLockEnabled: s3types.ObjectLockEnabledEnabled,
									Rule: &s3types.ObjectLockRule{
										DefaultRetention: &s3types.DefaultRetention{
											Mode: s3types.ObjectLockRetentionModeCompliance,
										},
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
						ForProvider: v1alpha1.BucketParameters{
							ObjectLockEnabledForBucket: &enabledTrue,
							ObjectLockConfiguration: &v1alpha1.ObjectLockConfiguration{
								ObjectLockEnabled: &objLockEnabled,
								Rule: &v1alpha1.ObjectLockRule{
									DefaultRetention: &v1alpha1.DefaultRetention{
										Mode: v1alpha1.ModeGovernance,
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
						backends[beName].ObjectLockConfigurationCondition.Equal(v1.Available()),
						"unexpected object lock config condition on s3-backend-1")
				},
			},
		},
		"Versioning config update fails": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{
						GetObjectLockConfigurationStub: func(ctx context.Context, lci *s3.GetObjectLockConfigurationInput, f ...func(*s3.Options)) (*s3.GetObjectLockConfigurationOutput, error) {
							return &s3.GetObjectLockConfigurationOutput{
								ObjectLockConfiguration: &s3types.ObjectLockConfiguration{
									ObjectLockEnabled: s3types.ObjectLockEnabledEnabled,
									Rule: &s3types.ObjectLockRule{
										DefaultRetention: &s3types.DefaultRetention{
											Mode: s3types.ObjectLockRetentionModeCompliance,
										},
									},
								},
							}, nil
						},
						PutObjectLockConfigurationStub: func(ctx context.Context, lci *s3.PutObjectLockConfigurationInput, f ...func(*s3.Options)) (*s3.PutObjectLockConfigurationOutput, error) {
							return &s3.PutObjectLockConfigurationOutput{}, errRandom
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
						ForProvider: v1alpha1.BucketParameters{
							ObjectLockEnabledForBucket: &enabledTrue,
							ObjectLockConfiguration: &v1alpha1.ObjectLockConfiguration{
								ObjectLockEnabled: &objLockEnabled,
								Rule: &v1alpha1.ObjectLockRule{
									DefaultRetention: &v1alpha1.DefaultRetention{
										Mode: v1alpha1.ModeGovernance,
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
						backends[beName].ObjectLockConfigurationCondition.Equal(v1.Unavailable().
							WithMessage(errors.Wrap(errors.Wrap(errRandom, "failed to put object lock configuration"), errHandleObjectLockConfig).Error())),
						"unexpected versioning config condition on s3-backend-1")
				},
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			c := NewObjectLockConfigurationClient(
				tc.fields.backendStore,
				s3clienthandler.NewHandler(
					s3clienthandler.WithAssumeRoleArn(nil),
					s3clienthandler.WithBackendStore(tc.fields.backendStore)),
				logr.Discard())

			bb := newBucketBackends()
			bb.setObjectLockConfigCondition(bucketName, beName, &creating)

			err := c.Handle(context.Background(), tc.args.bucket, tc.args.backendName, bb)
			require.ErrorIs(t, err, tc.want.err, "unexpected error")
			if tc.want.specificDiff != nil {
				tc.want.specificDiff(t, bb)
			}
		})
	}
}
