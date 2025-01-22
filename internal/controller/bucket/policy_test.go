package bucket

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	"github.com/linode/provider-ceph/internal/backendstore/backendstorefakes"
	"github.com/linode/provider-ceph/internal/controller/s3clienthandler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	errS3        = errors.New("some error")
	samplePolicy = `{"Policy": "{\"Version\": \"2012-10-17\", \"Statement\": [{\"Action\": [\"*\"], \"Principal\": \"*\", \"Sid\": \"\", \"Effect\": \"Allow\", \"Resource\": \"arn:aws:s3:::shunsuke-rgw-acl-test/*\"},]}"}`
)

//nolint:maintidx // for testing
func TestPolicyObserveBackend(t *testing.T) {
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
		"Attempt to observe policy on unhealthy backend": {
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
				status: Updated,
			},
		},
		"s3 error": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{
						GetBucketPolicyStub: func(ctx context.Context, in *s3.GetBucketPolicyInput, f ...func(*s3.Options)) (*s3.GetBucketPolicyOutput, error) {
							return nil, errS3
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
				err:    errS3,
			},
		},
		"ok - policy is updated": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{
						GetBucketPolicyStub: func(ctx context.Context, in *s3.GetBucketPolicyInput, f ...func(*s3.Options)) (*s3.GetBucketPolicyOutput, error) {
							return &s3.GetBucketPolicyOutput{Policy: aws.String(samplePolicy)}, nil
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
							Policy: samplePolicy,
						},
					},
				},
				backendName: "s3-backend-1",
			},
			want: want{
				status: Updated,
			},
		},
		"ok - no bucket policy in backend, but bucket CR has": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{
						GetBucketPolicyStub: func(ctx context.Context, in *s3.GetBucketPolicyInput, f ...func(*s3.Options)) (*s3.GetBucketPolicyOutput, error) {
							return nil, &smithy.GenericAPIError{Code: "NoSuchBucketPolicy", Message: "no policy"}
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
							Policy: samplePolicy,
						},
					},
				},
				backendName: "s3-backend-1",
			},
			want: want{
				status: NeedsUpdate,
			},
		},
		"ok - no bucket policy both in backend and bucket cr": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{
						GetBucketPolicyStub: func(ctx context.Context, in *s3.GetBucketPolicyInput, f ...func(*s3.Options)) (*s3.GetBucketPolicyOutput, error) {
							return nil, &smithy.GenericAPIError{Code: "NoSuchBucketPolicy", Message: "no policy"}
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
				status: Updated,
			},
		},
		"ok - policy is returned, but empty": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					emptyPolicy := ""
					fake := backendstorefakes.FakeS3Client{
						GetBucketPolicyStub: func(ctx context.Context, in *s3.GetBucketPolicyInput, f ...func(*s3.Options)) (*s3.GetBucketPolicyOutput, error) {
							return &s3.GetBucketPolicyOutput{Policy: aws.String(emptyPolicy)}, nil
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
							Policy: samplePolicy,
						},
					},
				},
				backendName: "s3-backend-1",
			},
			want: want{
				status: NeedsUpdate,
			},
		},
		"ok - backend has policy, but bucket cr doesn't": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{
						GetBucketPolicyStub: func(ctx context.Context, in *s3.GetBucketPolicyInput, f ...func(*s3.Options)) (*s3.GetBucketPolicyOutput, error) {
							return &s3.GetBucketPolicyOutput{Policy: aws.String(samplePolicy)}, nil
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
				status: NeedsDeletion,
			},
		},
		"ok - backend has different policy": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					differentPolicy := "{}"
					fake := backendstorefakes.FakeS3Client{
						GetBucketPolicyStub: func(ctx context.Context, in *s3.GetBucketPolicyInput, f ...func(*s3.Options)) (*s3.GetBucketPolicyOutput, error) {
							return &s3.GetBucketPolicyOutput{Policy: aws.String(differentPolicy)}, nil
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
							Policy: samplePolicy,
						},
					},
				},
				backendName: "s3-backend-1",
			},
			want: want{
				status: NeedsUpdate,
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			c := NewPolicyClient(
				tc.fields.backendStore,
				s3clienthandler.NewHandler(
					s3clienthandler.WithAssumeRoleArn(nil),
					s3clienthandler.WithBackendStore(tc.fields.backendStore),
				),
				logging.NewNopLogger(),
			)

			got, err := c.observeBackend(context.Background(), tc.args.bucket, tc.args.backendName)
			require.ErrorIs(t, err, tc.want.err, "unexpected error")
			assert.Equal(t, tc.want.status, got, "unexpected status")
		})
	}
}
