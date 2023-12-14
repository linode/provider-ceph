package bucket

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	v1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	"github.com/linode/provider-ceph/internal/backendstore/backendstorefakes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestCreateBasicErrors(t *testing.T) {
	t.Parallel()

	type fields struct {
		backendStore *backendstore.BackendStore
	}

	type args struct {
		mg resource.Managed
	}

	type want struct {
		err error
	}

	cases := map[string]struct {
		reason string
		fields fields
		args   args
		want   want
	}{
		"S3 backends missing": {
			fields: fields{
				backendStore: backendstore.NewBackendStore(),
			},
			args: args{
				mg: &v1alpha1.Bucket{},
			},
			want: want{
				err: errors.New(errNoS3BackendsStored),
			},
		},
		"S3 backend reference does not exist": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-0", nil, false, apisv1alpha1.HealthStatusUnknown)

					return bs
				}(),
			},
			args: args{
				mg: &v1alpha1.Bucket{
					Spec: v1alpha1.BucketSpec{
						Providers: []string{"s3-backend-1"},
					},
				},
			},
			want: want{
				err: errors.New(errNoActiveS3Backends),
			},
		},
		"S3 backend reference inactive": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-0", nil, true, apisv1alpha1.HealthStatusUnknown)
					bs.AddOrUpdateBackend("s3-backend-1", nil, false, apisv1alpha1.HealthStatusUnknown)

					return bs
				}(),
			},
			args: args{
				mg: &v1alpha1.Bucket{
					Spec: v1alpha1.BucketSpec{
						Providers: []string{"s3-backend-0", "s3-backend-1"},
					},
				},
			},
			want: want{
				err: errors.New(errMissingS3Backend),
			},
		},
		"S3 backend reference missing": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-0", nil, true, apisv1alpha1.HealthStatusUnknown)

					return bs
				}(),
			},
			args: args{
				mg: &v1alpha1.Bucket{
					Spec: v1alpha1.BucketSpec{
						Providers: []string{"s3-backend-0", "s3-backend-1"},
					},
				},
			},
			want: want{
				err: errors.New(errMissingS3Backend),
			},
		},
		"S3 backend not referenced and none exist": {
			fields: fields{
				backendStore: backendstore.NewBackendStore(),
			},
			args: args{
				mg: &v1alpha1.Bucket{},
			},
			want: want{
				err: errors.New(errNoS3BackendsStored),
			},
		},
	}
	for name, tc := range cases {
		tc := tc

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			e := external{
				backendStore: tc.fields.backendStore,
				log:          logging.NewNopLogger(),
			}

			_, err := e.Create(context.Background(), tc.args.mg)
			require.EqualError(t, err, tc.want.err.Error(), "unexpected error")
		})
	}
}

//nolint:paralleltest // Running in parallel causes issues with client.
func TestCreate(t *testing.T) {
	type fields struct {
		backendStore    *backendstore.BackendStore
		providerConfigs *apisv1alpha1.ProviderConfigList
	}

	type args struct {
		mg resource.Managed
	}

	type want struct {
		o          managed.ExternalCreation
		statusDiff func(t *testing.T, mg resource.Managed)
		err        error
	}

	cases := map[string]struct {
		reason string
		fields fields
		args   args
		want   want
	}{
		"Create succeeds on single backend": {
			fields: fields{
				providerConfigs: &apisv1alpha1.ProviderConfigList{
					Items: []apisv1alpha1.ProviderConfig{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "s3-backend-1",
							},
						},
					},
				},
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{}
					fake.CreateBucketReturns(
						&s3.CreateBucketOutput{},
						nil,
					)

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", &fake, true, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				mg: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-bucket",
					},
				},
			},
			want: want{
				err: nil,
				statusDiff: func(t *testing.T, mg resource.Managed) {
					t.Helper()
					bucket, _ := mg.(*v1alpha1.Bucket)

					assert.True(t,
						bucket.Status.Conditions[0].Equal(v1.Available()),
						"bucket cr condition is not available")

					assert.True(t,
						bucket.Status.AtProvider.Backends["s3-backend-1"].BucketCondition.Equal(v1.Available()),
						"bucket condition on backend is not available")
				},
			},
		},
		"Create fails on two backends and succeeds on one": {
			fields: fields{
				providerConfigs: &apisv1alpha1.ProviderConfigList{
					Items: []apisv1alpha1.ProviderConfig{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "s3-backend-1",
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "s3-backend-2",
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "s3-backend-3",
							},
						},
					},
				},
				backendStore: func() *backendstore.BackendStore {
					fakeClientError := backendstorefakes.FakeS3Client{}
					fakeClientOK := backendstorefakes.FakeS3Client{}

					fakeClientError.CreateBucketReturns(
						&s3.CreateBucketOutput{},
						errors.New("some error"),
					)

					fakeClientOK.CreateBucketReturns(
						&s3.CreateBucketOutput{},
						nil,
					)

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", &fakeClientError, true, apisv1alpha1.HealthStatusHealthy)
					bs.AddOrUpdateBackend("s3-backend-2", &fakeClientError, true, apisv1alpha1.HealthStatusHealthy)
					bs.AddOrUpdateBackend("s3-backend-3", &fakeClientOK, true, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				mg: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-bucket",
					},
				},
			},
			want: want{
				err: nil,
				statusDiff: func(t *testing.T, mg resource.Managed) {
					t.Helper()
					bucket, _ := mg.(*v1alpha1.Bucket)

					assert.True(t,
						bucket.Status.Conditions[0].Equal(v1.Available()),
						"bucket cr condition is not available")

					assert.True(t,
						bucket.Status.AtProvider.Backends["s3-backend-3"].BucketCondition.Equal(v1.Available()),
						"bucket condition on backend is not available")
				},
			},
		},
		"Create fails on all backends": {
			fields: fields{
				providerConfigs: &apisv1alpha1.ProviderConfigList{
					Items: []apisv1alpha1.ProviderConfig{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "s3-backend-1",
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "s3-backend-2",
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "s3-backend-3",
							},
						},
					},
				},
				backendStore: func() *backendstore.BackendStore {
					fakeClientError := backendstorefakes.FakeS3Client{}

					fakeClientError.CreateBucketReturns(
						&s3.CreateBucketOutput{},
						errors.New("some error"),
					)

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", &fakeClientError, true, apisv1alpha1.HealthStatusHealthy)
					bs.AddOrUpdateBackend("s3-backend-2", &fakeClientError, true, apisv1alpha1.HealthStatusHealthy)
					bs.AddOrUpdateBackend("s3-backend-3", &fakeClientError, true, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				mg: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-bucket",
					},
				},
			},
			want: want{
				err: nil,
				statusDiff: func(t *testing.T, mg resource.Managed) {
					t.Helper()
					bucket, _ := mg.(*v1alpha1.Bucket)

					assert.True(t,
						bucket.Status.Conditions[0].Equal(v1.Unavailable()),
						"condition is not unavailable")
				},
			},
		},
	}

	for name, tc := range cases {
		tc := tc

		t.Run(name, func(t *testing.T) {
			s := runtime.NewScheme()
			s.AddKnownTypes(v1alpha1.SchemeGroupVersion, &v1alpha1.Bucket{}, &v1alpha1.BucketList{})
			s.AddKnownTypes(apisv1alpha1.SchemeGroupVersion, &apisv1alpha1.ProviderConfig{}, &apisv1alpha1.ProviderConfigList{})

			cl := fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(tc.args.mg).
				WithStatusSubresource(tc.args.mg)

			if tc.fields.providerConfigs != nil {
				cl.WithLists(tc.fields.providerConfigs)
			}

			e := external{
				kubeClient:       cl.Build(),
				backendStore:     tc.fields.backendStore,
				log:              logging.NewNopLogger(),
				operationTimeout: time.Second * 5,
			}

			got, err := e.Create(context.Background(), tc.args.mg)
			require.ErrorIs(t, err, tc.want.err, "unexpected err")
			assert.Equal(t, got, tc.want.o, "unexpected result")
			if tc.want.statusDiff != nil {
				tc.want.statusDiff(t, tc.args.mg)
			}
		})
	}
}
