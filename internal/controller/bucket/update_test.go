package bucket

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	v1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	"github.com/linode/provider-ceph/internal/backendstore/backendstorefakes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestUpdateBasicErrors(t *testing.T) {
	t.Parallel()

	type fields struct {
		backendStore *backendstore.BackendStore
	}

	type args struct {
		mg resource.Managed
	}

	type want struct {
		o   managed.ExternalUpdate
		err error
	}

	cases := map[string]struct {
		reason string
		fields fields
		args   args
		want   want
	}{
		"Invalid managed resource": {
			fields: fields{
				backendStore: backendstore.NewBackendStore(),
			},
			args: args{
				mg: unexpectedItem,
			},
			want: want{
				err: errors.New(errNotBucket),
			},
		},
		"Bucket is disabled": {
			fields: fields{
				backendStore: backendstore.NewBackendStore(),
			},
			args: args{
				mg: &v1alpha1.Bucket{
					Spec: v1alpha1.BucketSpec{
						Disabled: true,
					},
				},
			},
			want: want{
				o:   managed.ExternalUpdate{},
				err: errors.New(errNoS3BackendsStored),
			},
		},
		"No active backend": {
			fields: fields{
				backendStore: backendstore.NewBackendStore(),
			},
			args: args{
				mg: &v1alpha1.Bucket{
					Spec: v1alpha1.BucketSpec{
						Providers: []string{
							"s3-backend-1",
						},
					},
				},
			},
			want: want{
				o:   managed.ExternalUpdate{},
				err: errors.New(errNoActiveS3Backends),
			},
		},
		"Missing backend": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{
						HeadBucketStub: func(ctx context.Context, hbi *s3.HeadBucketInput, f ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
							return &s3.HeadBucketOutput{}, nil
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", &fake, nil, true, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				mg: &v1alpha1.Bucket{
					Spec: v1alpha1.BucketSpec{
						Providers: []string{
							"s3-backend-1",
							"s3-backend-2",
						},
					},
				},
			},
			want: want{
				o:   managed.ExternalUpdate{},
				err: errors.New(errMissingS3Backend),
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

			_, err := e.Update(context.Background(), tc.args.mg)
			require.EqualError(t, err, tc.want.err.Error(), "unexpected err")
		})
	}
}

//nolint:maintidx // Function requires numerous checks.
func TestUpdate(t *testing.T) {
	t.Parallel()
	someError := errors.New("some error")

	type fields struct {
		backendStore    *backendstore.BackendStore
		autoPauseBucket bool
		initObjects     []client.Object
	}

	type args struct {
		mg resource.Managed
	}

	type want struct {
		o            managed.ExternalUpdate
		err          error
		specificDiff func(t *testing.T, mg resource.Managed)
	}

	cases := map[string]struct {
		reason string
		fields fields
		args   args
		want   want
	}{
		"Two backends update successfully": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{
						HeadBucketStub: func(ctx context.Context, hbi *s3.HeadBucketInput, f ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
							return &s3.HeadBucketOutput{}, nil
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", &fake, nil, true, apisv1alpha1.HealthStatusHealthy)
					bs.AddOrUpdateBackend("s3-backend-2", &fake, nil, true, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				mg: &v1alpha1.Bucket{
					Spec: v1alpha1.BucketSpec{
						Providers: []string{
							"s3-backend-1",
							"s3-backend-2",
						},
					},
				},
			},
			want: want{
				o: managed.ExternalUpdate{},
				specificDiff: func(t *testing.T, mg resource.Managed) {
					t.Helper()
					bucket, _ := mg.(*v1alpha1.Bucket)

					assert.True(t,
						bucket.Status.Conditions[0].Equal(v1.Available()),
						"unexpected bucket condition")

					assert.True(t,
						bucket.Status.AtProvider.Backends["s3-backend-1"].BucketCondition.Equal(v1.Available()),
						"bucket condition on s3-backend-1 is not available")

					assert.True(t,
						bucket.Status.AtProvider.Backends["s3-backend-2"].BucketCondition.Equal(v1.Available()),
						"bucket condition on s3-backend-2 is not available")
				},
			},
		},
		"Two backends fail to update": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{
						HeadBucketStub: func(ctx context.Context, hbi *s3.HeadBucketInput, f ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
							return &s3.HeadBucketOutput{}, someError
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", &fake, true, apisv1alpha1.HealthStatusHealthy)
					bs.AddOrUpdateBackend("s3-backend-2", &fake, true, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				mg: &v1alpha1.Bucket{
					Spec: v1alpha1.BucketSpec{
						Providers: []string{
							"s3-backend-1",
							"s3-backend-2",
						},
					},
				},
			},
			want: want{
				err: someError,
				o:   managed.ExternalUpdate{},
				specificDiff: func(t *testing.T, mg resource.Managed) {
					t.Helper()
					bucket, _ := mg.(*v1alpha1.Bucket)

					assert.True(t,
						bucket.Status.Conditions[0].Equal(v1.Unavailable()),
						"unexpected bucket condition")

					assert.True(t,
						bucket.Status.AtProvider.Backends["s3-backend-1"].BucketCondition.Equal(v1.Unavailable().WithMessage(errors.Wrap(someError, "failed to perform head bucket").Error())),
						"unexpected bucket condition for s3-backend-1")

					assert.True(t,
						bucket.Status.AtProvider.Backends["s3-backend-2"].BucketCondition.Equal(v1.Unavailable().WithMessage(errors.Wrap(someError, "failed to perform head bucket").Error())),
						"unexpected bucket condition for s3-backend-2")
				},
			},
		},
		"One backend updates successfully and one fails to update": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fakeErr := backendstorefakes.FakeS3Client{
						HeadBucketStub: func(ctx context.Context, hbi *s3.HeadBucketInput, f ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
							return &s3.HeadBucketOutput{}, someError
						},
					}
					fakeOK := backendstorefakes.FakeS3Client{
						HeadBucketStub: func(ctx context.Context, hbi *s3.HeadBucketInput, f ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
							return &s3.HeadBucketOutput{}, nil
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", &fakeOK, true, apisv1alpha1.HealthStatusHealthy)
					bs.AddOrUpdateBackend("s3-backend-2", &fakeErr, true, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				mg: &v1alpha1.Bucket{
					Spec: v1alpha1.BucketSpec{
						Providers: []string{
							"s3-backend-1",
							"s3-backend-2",
						},
					},
				},
			},
			want: want{
				err: someError,
				o:   managed.ExternalUpdate{},
				specificDiff: func(t *testing.T, mg resource.Managed) {
					t.Helper()
					bucket, _ := mg.(*v1alpha1.Bucket)

					// Bucket CR is considered Available because one or more
					// buckets on backends are Available.
					assert.True(t,
						bucket.Status.Conditions[0].Equal(v1.Available()),
						"unexpected bucket condition")

					assert.True(t,
						bucket.Status.AtProvider.Backends["s3-backend-1"].BucketCondition.Equal(v1.Available()),
						"unexpected bucket condition for s3-backend-1")

					assert.True(t,
						bucket.Status.AtProvider.Backends["s3-backend-2"].BucketCondition.Equal(v1.Unavailable().WithMessage(errors.Wrap(someError, "failed to perform head bucket").Error())),
						"unexpected bucket condition for s3-backend-2")
				},
			},
		},
		"Single backend updates successfully and is autopaused": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{
						HeadBucketStub: func(ctx context.Context, hbi *s3.HeadBucketInput, f ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
							return &s3.HeadBucketOutput{}, nil
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", &fake, nil, true, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
				autoPauseBucket: true,
				initObjects: []client.Object{
					&v1alpha1.Bucket{
						ObjectMeta: metav1.ObjectMeta{
							Name: "bucket",
							Annotations: map[string]string{
								"test": "test",
							},
						},
					},
				},
			},
			args: args{
				mg: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: "bucket",
						Annotations: map[string]string{
							"test": "test",
						},
					},
					Spec: v1alpha1.BucketSpec{
						Providers: []string{
							"s3-backend-1",
						},
					},
				},
			},
			want: want{
				o: managed.ExternalUpdate{},
				specificDiff: func(t *testing.T, mg resource.Managed) {
					t.Helper()
					bucket, _ := mg.(*v1alpha1.Bucket)
					assert.True(t,
						bucket.Status.Conditions[0].Equal(v1.Available()),
						"unexpected bucket condition")

					assert.True(t,
						bucket.Status.AtProvider.Backends["s3-backend-1"].BucketCondition.Equal(v1.Available()),
						"bucket condition on s3-backend-1 is not available")

					assert.Equal(t,
						map[string]string{
							meta.AnnotationKeyReconciliationPaused: "true",
							"provider-ceph.backends.s3-backend-1":  "",
						},
						bucket.Labels,
						"unexpected bucket labels",
					)
				},
			},
		},
	}

	bk := &v1alpha1.Bucket{}
	s := scheme.Scheme
	s.AddKnownTypes(apisv1alpha1.SchemeGroupVersion, bk)

	for name, tc := range cases {
		tc := tc

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cl := fake.NewClientBuilder().
				WithObjects(tc.fields.initObjects...).
				WithStatusSubresource(tc.fields.initObjects...).
				WithScheme(s).Build()

			e := external{
				kubeClient:      cl,
				backendStore:    tc.fields.backendStore,
				autoPauseBucket: tc.fields.autoPauseBucket,
				log:             logging.NewNopLogger(),
			}

			got, err := e.Update(context.Background(), tc.args.mg)
			require.ErrorIs(t, err, tc.want.err, "unexpected err")
			assert.Equal(t, got, tc.want.o, "unexpected result")
			if tc.want.specificDiff != nil {
				tc.want.specificDiff(t, tc.args.mg)
			}
		})
	}
}
