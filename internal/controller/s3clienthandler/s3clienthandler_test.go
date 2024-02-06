package s3clienthandler

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sts"
	ststypes "github.com/aws/aws-sdk-go-v2/service/sts/types"
	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	"github.com/linode/provider-ceph/internal/backendstore/backendstorefakes"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
)

//nolint:maintidx //Test with lots of cases.
func TestCreateAssumeRoleS3Client(t *testing.T) {
	t.Parallel()

	errAssumeRole := errors.New("some assume role error")
	roleArn := "role-arn"
	dummySK := "secretkey"
	dummyAK := "accesskey"
	dummyST := "sessiontoken"

	type fields struct {
		backendStore *backendstore.BackendStore
		roleArn      *string
		initObjects  []client.Object
	}

	type args struct {
		bucket      *v1alpha1.Bucket
		backendName string
	}

	type want struct {
		requireErr func(t *testing.T, err error)
	}

	tests := map[string]struct {
		fields fields
		args   args
		want   want
	}{
		"no sts clients in backend store": {
			fields: fields{
				roleArn: &roleArn,
				backendStore: func() *backendstore.BackendStore {
					bs := backendstore.NewBackendStore()

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
							AssumeRoleTags: []v1alpha1.Tag{},
						},
					},
				},
				backendName: "s3-backend-2",
			},
			want: want{
				requireErr: func(t *testing.T, err error) {
					t.Helper()

					require.ErrorIs(t, err, errNoSTSClient, "unexpected error")
				},
			},
		},
		"no sts client for backend": {
			fields: fields{
				roleArn: &roleArn,
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeSTSClient{
						AssumeRoleStub: func(context.Context, *sts.AssumeRoleInput, ...func(*sts.Options)) (*sts.AssumeRoleOutput, error) {
							return &sts.AssumeRoleOutput{}, nil
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", nil, &fake, true, apisv1alpha1.HealthStatusHealthy)

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
							AssumeRoleTags: []v1alpha1.Tag{},
						},
					},
				},
				backendName: "s3-backend-2",
			},
			want: want{
				requireErr: func(t *testing.T, err error) {
					t.Helper()

					require.ErrorIs(t, err, errNoSTSClient, "unexpected error")
				},
			},
		},
		"assume role error": {
			fields: fields{
				roleArn: &roleArn,
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeSTSClient{
						AssumeRoleStub: func(context.Context, *sts.AssumeRoleInput, ...func(*sts.Options)) (*sts.AssumeRoleOutput, error) {
							return &sts.AssumeRoleOutput{}, errAssumeRole
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", nil, &fake, true, apisv1alpha1.HealthStatusHealthy)

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
							AssumeRoleTags: []v1alpha1.Tag{},
						},
					},
				},
				backendName: "s3-backend-1",
			},
			want: want{
				requireErr: func(t *testing.T, err error) {
					t.Helper()

					require.ErrorIs(t, err, errAssumeRole, "unexpected error")
				},
			},
		},
		"missing credentials error": {
			fields: fields{
				roleArn: &roleArn,
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeSTSClient{
						AssumeRoleStub: func(context.Context, *sts.AssumeRoleInput, ...func(*sts.Options)) (*sts.AssumeRoleOutput, error) {
							return &sts.AssumeRoleOutput{
								Credentials: &ststypes.Credentials{},
							}, nil
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", nil, &fake, true, apisv1alpha1.HealthStatusHealthy)

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
							AssumeRoleTags: []v1alpha1.Tag{},
						},
					},
				},
				backendName: "s3-backend-1",
			},
			want: want{
				requireErr: func(t *testing.T, err error) {
					t.Helper()

					require.ErrorIs(t, err, errNoCreds, "unexpected error")
				},
			},
		},
		"missing credentials error no access key": {
			fields: fields{
				roleArn: &roleArn,
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeSTSClient{
						AssumeRoleStub: func(context.Context, *sts.AssumeRoleInput, ...func(*sts.Options)) (*sts.AssumeRoleOutput, error) {
							return &sts.AssumeRoleOutput{
								Credentials: &ststypes.Credentials{
									SecretAccessKey: &dummySK,
									SessionToken:    &dummyST,
								},
							}, nil
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", nil, &fake, true, apisv1alpha1.HealthStatusHealthy)

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
							AssumeRoleTags: []v1alpha1.Tag{},
						},
					},
				},
				backendName: "s3-backend-1",
			},
			want: want{
				requireErr: func(t *testing.T, err error) {
					t.Helper()

					require.ErrorIs(t, err, errNoCreds, "unexpected error")
				},
			},
		},
		"missing credentials error no secret key": {
			fields: fields{
				roleArn: &roleArn,
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeSTSClient{
						AssumeRoleStub: func(context.Context, *sts.AssumeRoleInput, ...func(*sts.Options)) (*sts.AssumeRoleOutput, error) {
							return &sts.AssumeRoleOutput{
								Credentials: &ststypes.Credentials{
									AccessKeyId:  &dummyAK,
									SessionToken: &dummyST,
								},
							}, nil
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", nil, &fake, true, apisv1alpha1.HealthStatusHealthy)

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
							AssumeRoleTags: []v1alpha1.Tag{},
						},
					},
				},
				backendName: "s3-backend-1",
			},
			want: want{
				requireErr: func(t *testing.T, err error) {
					t.Helper()

					require.ErrorIs(t, err, errNoCreds, "unexpected error")
				},
			},
		},
		"missing credentials error no session token": {
			fields: fields{
				roleArn: &roleArn,
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeSTSClient{
						AssumeRoleStub: func(context.Context, *sts.AssumeRoleInput, ...func(*sts.Options)) (*sts.AssumeRoleOutput, error) {
							return &sts.AssumeRoleOutput{
								Credentials: &ststypes.Credentials{
									AccessKeyId:     &dummyAK,
									SecretAccessKey: &dummySK,
								},
							}, nil
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", nil, &fake, true, apisv1alpha1.HealthStatusHealthy)

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
							AssumeRoleTags: []v1alpha1.Tag{},
						},
					},
				},
				backendName: "s3-backend-1",
			},
			want: want{
				requireErr: func(t *testing.T, err error) {
					t.Helper()

					require.ErrorIs(t, err, errNoCreds, "unexpected error")
				},
			},
		},
		"provider config not found for backend": {
			fields: fields{
				roleArn: &roleArn,
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeSTSClient{
						AssumeRoleStub: func(context.Context, *sts.AssumeRoleInput, ...func(*sts.Options)) (*sts.AssumeRoleOutput, error) {
							return &sts.AssumeRoleOutput{
								Credentials: &ststypes.Credentials{
									AccessKeyId:     &dummyAK,
									SecretAccessKey: &dummySK,
								},
							}, nil
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", nil, &fake, true, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
				initObjects: []client.Object{
					&apisv1alpha1.ProviderConfig{
						ObjectMeta: metav1.ObjectMeta{
							Name: "s3-backend-2",
						},
					},
					&apisv1alpha1.ProviderConfig{
						ObjectMeta: metav1.ObjectMeta{
							Name: "s3-backend-3",
						},
					},
				},
			},
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: "bucket",
					},
					Spec: v1alpha1.BucketSpec{
						ForProvider: v1alpha1.BucketParameters{
							AssumeRoleTags: []v1alpha1.Tag{},
						},
					},
				},
				backendName: "s3-backend-1",
			},
			want: want{
				requireErr: func(t *testing.T, err error) {
					t.Helper()

					require.ErrorContains(t, err, errFailedToCreateAssumeRoleS3Client.Error(), "unexpected error")
				},
			},
		},

		"success": {
			fields: fields{
				roleArn: &roleArn,
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeSTSClient{
						AssumeRoleStub: func(context.Context, *sts.AssumeRoleInput, ...func(*sts.Options)) (*sts.AssumeRoleOutput, error) {
							return &sts.AssumeRoleOutput{
								Credentials: &ststypes.Credentials{
									AccessKeyId:     &dummyAK,
									SecretAccessKey: &dummySK,
								},
							}, nil
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", nil, &fake, true, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
				initObjects: []client.Object{
					&apisv1alpha1.ProviderConfig{
						ObjectMeta: metav1.ObjectMeta{
							Name: "s3-backend-1",
						},
					},
				},
			},
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: "bucket",
					},
					Spec: v1alpha1.BucketSpec{
						ForProvider: v1alpha1.BucketParameters{
							AssumeRoleTags: []v1alpha1.Tag{},
						},
					},
				},
				backendName: "s3-backend-1",
			},
			want: want{
				requireErr: nil,
			},
		},
	}

	pc := &apisv1alpha1.ProviderConfig{}
	s := scheme.Scheme
	s.AddKnownTypes(apisv1alpha1.SchemeGroupVersion, pc)

	for name, tt := range tests {
		tt := tt
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cl := fake.NewClientBuilder().
				WithObjects(tt.fields.initObjects...).
				WithStatusSubresource(tt.fields.initObjects...).
				WithScheme(s).Build()

			h := NewHandler(
				WithBackendStore(tt.fields.backendStore),
				WithAssumeRoleArn(tt.fields.roleArn),
				WithKubeClient(cl),
				WithS3Timeout(time.Second*5),
				WithLog(logging.NewNopLogger()))

			_, err := h.createAssumeRoleS3Client(context.TODO(), tt.args.bucket, tt.args.backendName)
			if tt.want.requireErr != nil {
				tt.want.requireErr(t, err)
			}
		})
	}
}
