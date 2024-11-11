package bucket

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sts"
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
	"github.com/linode/provider-ceph/internal/controller/s3clienthandler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var vEnabled = v1alpha1.VersioningStatusEnabled
var lEnabled = v1alpha1.ObjectLockEnabledEnabled

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
	}
	for name, tc := range cases {
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
	roleArn := "role-arn"

	type fields struct {
		backendStore    *backendstore.BackendStore
		autoPauseBucket bool
		roleArn         *string
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
						"unexpected bucket ready condition")

					assert.True(t,
						bucket.Status.Conditions[1].Equal(v1.ReconcileSuccess()),
						"unexpected bucket synced condition")

					assert.True(t,
						bucket.Status.AtProvider.Backends["s3-backend-1"].BucketCondition.Equal(v1.Available()),
						"bucket condition on s3-backend-1 is not available")

					assert.True(t,
						bucket.Status.AtProvider.Backends["s3-backend-2"].BucketCondition.Equal(v1.Available()),
						"bucket condition on s3-backend-2 is not available")
				},
			},
		},
		"Update skipped for both backends because assume role fails for sts client": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeSTSClient{
						AssumeRoleStub: func(ctx context.Context, ari *sts.AssumeRoleInput, f ...func(*sts.Options)) (*sts.AssumeRoleOutput, error) {
							return &sts.AssumeRoleOutput{}, someError
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", nil, &fake, true, apisv1alpha1.HealthStatusHealthy)
					bs.AddOrUpdateBackend("s3-backend-2", nil, &fake, true, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
				roleArn: &roleArn,
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
						bucket.Status.Conditions[0].Equal(v1.Unavailable()),
						"unexpected bucket ready condition")

					unavailableBackends := []string{"s3-backend-1", "s3-backend-2"}
					slices.Sort(unavailableBackends)
					assert.True(t,
						bucket.Status.Conditions[1].Equal(v1.ReconcileError(errors.New(
							fmt.Sprintf(errUnavailableBackends, strings.Join(unavailableBackends, ", "))))),
						"unexpected bucket synced condition")

					assert.True(t,
						bucket.Status.AtProvider.Backends["s3-backend-1"].BucketCondition.Equal(v1.Unavailable().
							WithMessage(errors.Wrap(errors.Wrap(someError, "failed to assume role"), "Failed to create s3 client via assume role").Error())), "unexpected bucket condition for s3-backend-1")

					assert.True(t,
						bucket.Status.AtProvider.Backends["s3-backend-2"].BucketCondition.Equal(v1.Unavailable().
							WithMessage(errors.Wrap(errors.Wrap(someError, "failed to assume role"), "Failed to create s3 client via assume role").Error())), "unexpected bucket condition for s3-backend-2")
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
				err: someError,
				o:   managed.ExternalUpdate{},
				specificDiff: func(t *testing.T, mg resource.Managed) {
					t.Helper()
					bucket, _ := mg.(*v1alpha1.Bucket)
					assert.True(t,
						bucket.Status.Conditions[0].Equal(v1.Unavailable()),
						"unexpected bucket ready condition")

					unavailableBackends := []string{"s3-backend-1", "s3-backend-2"}
					slices.Sort(unavailableBackends)
					assert.True(t,
						bucket.Status.Conditions[1].Equal(v1.ReconcileError(errors.New(
							fmt.Sprintf(errUnavailableBackends, strings.Join(unavailableBackends, ", "))))),
						"unexpected bucket synced condition")

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
					bs.AddOrUpdateBackend("s3-backend-1", &fakeOK, nil, true, apisv1alpha1.HealthStatusHealthy)
					bs.AddOrUpdateBackend("s3-backend-2", &fakeErr, nil, true, apisv1alpha1.HealthStatusHealthy)

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
						"unexpected bucket ready condition")

					assert.True(t,
						bucket.Status.Conditions[1].Equal(v1.ReconcileError(errors.New(
							fmt.Sprintf(errUnavailableBackends, strings.Join([]string{"s3-backend-2"}, ", "))))),
						"unexpected bucket synced condition")

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
							ObjectLockEnabledForBucket: &enabledTrue,
							ObjectLockConfiguration: &v1alpha1.ObjectLockConfiguration{
								ObjectLockEnabled: &lEnabled,
							},
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
						"unexpected bucket ready condition")

					assert.True(t,
						bucket.Status.Conditions[1].Equal(v1.ReconcileSuccess()),
						"unexpected bucket synced condition")

					assert.True(t,
						bucket.Status.AtProvider.Backends["s3-backend-1"].BucketCondition.Equal(v1.Available()),
						"bucket condition on s3-backend-1 is not available")
					assert.True(t,
						bucket.Status.AtProvider.Backends["s3-backend-1"].LifecycleConfigurationCondition.Equal(v1.Available()),
						"lifecycle config condition on s3-backend-1 is not available")
					assert.True(t,
						bucket.Status.AtProvider.Backends["s3-backend-1"].VersioningConfigurationCondition.Equal(v1.Available()),
						"versioning config condition on s3-backend-1 is not available")
					assert.True(t,
						bucket.Status.AtProvider.Backends["s3-backend-1"].ObjectLockConfigurationCondition.Equal(v1.Available()),
						"object lock config condition on s3-backend-1 is not available")

					assert.Equal(t,
						map[string]string{
							meta.AnnotationKeyReconciliationPaused: True,
							"provider-ceph.backends.s3-backend-1":  True,
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
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cl := fake.NewClientBuilder().
				WithObjects(tc.fields.initObjects...).
				WithStatusSubresource(tc.fields.initObjects...).
				WithScheme(s).Build()

			s3ClientHandler := s3clienthandler.NewHandler(
				s3clienthandler.WithAssumeRoleArn(tc.fields.roleArn),
				s3clienthandler.WithBackendStore(tc.fields.backendStore),
				s3clienthandler.WithKubeClient(cl))

			e := external{
				kubeClient:         cl,
				backendStore:       tc.fields.backendStore,
				s3ClientHandler:    s3ClientHandler,
				autoPauseBucket:    tc.fields.autoPauseBucket,
				minReplicas:        1,
				log:                logging.NewNopLogger(),
				subresourceClients: NewSubresourceClients(tc.fields.backendStore, s3ClientHandler, SubresourceClientConfig{}, logging.NewNopLogger()),
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

//nolint:maintidx // Function requires numerous checks.
func TestUpdateLifecycleConfigSubResource(t *testing.T) {
	t.Parallel()
	someError := errors.New("some error")

	type fields struct {
		backendStore    *backendstore.BackendStore
		autoPauseBucket bool
		roleArn         *string
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
		"Two backends update lifecycle config successfully": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{}

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
				},
			},
			want: want{
				o: managed.ExternalUpdate{},
				specificDiff: func(t *testing.T, mg resource.Managed) {
					t.Helper()
					bucket, _ := mg.(*v1alpha1.Bucket)

					assert.True(t,
						bucket.Status.AtProvider.Backends["s3-backend-1"].LifecycleConfigurationCondition.Equal(v1.Available()),
						"lifecycle configuration condition on s3-backend-1 is not available")

					assert.True(t,
						bucket.Status.AtProvider.Backends["s3-backend-2"].LifecycleConfigurationCondition.Equal(v1.Available()),
						"lifecycle configuration condition on s3-backend-2 is not available")
				},
			},
		},
		"Two backends fail to update lifecycle config": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{
						PutBucketLifecycleConfigurationStub: func(ctx context.Context, hbi *s3.PutBucketLifecycleConfigurationInput, f ...func(*s3.Options)) (*s3.PutBucketLifecycleConfigurationOutput, error) {
							return &s3.PutBucketLifecycleConfigurationOutput{}, someError
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
				},
			},
			want: want{
				err: someError,
				o:   managed.ExternalUpdate{},
				specificDiff: func(t *testing.T, mg resource.Managed) {
					t.Helper()
					bucket, _ := mg.(*v1alpha1.Bucket)
					unavailableBackends := []string{"s3-backend-1", "s3-backend-2"}
					slices.Sort(unavailableBackends)
					assert.True(t,
						bucket.Status.AtProvider.Backends["s3-backend-1"].LifecycleConfigurationCondition.Equal(
							v1.Unavailable().WithMessage(
								errors.Wrap(
									errors.Wrap(someError, "failed to put bucket lifecycle configuration"),
									"failed to handle bucket lifecycle configuration").Error(),
							),
						),
						"unexpected lifecycle configuration condition for s3-backend-1")

					assert.True(t,
						bucket.Status.AtProvider.Backends["s3-backend-2"].LifecycleConfigurationCondition.Equal(
							v1.Unavailable().WithMessage(
								errors.Wrap(
									errors.Wrap(someError, "failed to put bucket lifecycle configuration"),
									"failed to handle bucket lifecycle configuration").Error(),
							),
						),
						"unexpected lifecycle configuration condition for s3-backend-2")
				},
			},
		},
		"One backend updates lifecycle config successfully and one fails to update lifecycle config": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fakeErr := backendstorefakes.FakeS3Client{
						PutBucketLifecycleConfigurationStub: func(ctx context.Context, hbi *s3.PutBucketLifecycleConfigurationInput, f ...func(*s3.Options)) (*s3.PutBucketLifecycleConfigurationOutput, error) {
							return &s3.PutBucketLifecycleConfigurationOutput{}, someError
						},
					}
					fakeOK := backendstorefakes.FakeS3Client{}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", &fakeOK, nil, true, apisv1alpha1.HealthStatusHealthy)
					bs.AddOrUpdateBackend("s3-backend-2", &fakeErr, nil, true, apisv1alpha1.HealthStatusHealthy)

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
				},
			},
			want: want{
				err: someError,
				o:   managed.ExternalUpdate{},
				specificDiff: func(t *testing.T, mg resource.Managed) {
					t.Helper()
					bucket, _ := mg.(*v1alpha1.Bucket)

					assert.True(t,
						bucket.Status.AtProvider.Backends["s3-backend-1"].LifecycleConfigurationCondition.Equal(v1.Available()),
						"unexpected lifecycle configuration condition for s3-backend-1")

					assert.True(t,
						bucket.Status.AtProvider.Backends["s3-backend-2"].LifecycleConfigurationCondition.Equal(
							v1.Unavailable().WithMessage(
								errors.Wrap(
									errors.Wrap(someError, "failed to put bucket lifecycle configuration"),
									"failed to handle bucket lifecycle configuration").Error(),
							),
						),
						"unexpected lifecycle configuration condition for s3-backend-2")
				},
			},
		},
		"Single backend updates lifecycle configuration successfully and is autopaused": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{}

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
				},
			},
			want: want{
				o: managed.ExternalUpdate{},
				specificDiff: func(t *testing.T, mg resource.Managed) {
					t.Helper()
					bucket, _ := mg.(*v1alpha1.Bucket)
					assert.True(t,
						bucket.Status.Conditions[0].Equal(v1.Available()),
						"unexpected bucket ready condition")

					assert.True(t,
						bucket.Status.Conditions[1].Equal(v1.ReconcileSuccess()),
						"unexpected bucket synced condition")

					assert.True(t,
						bucket.Status.AtProvider.Backends["s3-backend-1"].LifecycleConfigurationCondition.Equal(v1.Available()),
						"lifecycle configuration condition on s3-backend-1 is not available")

					assert.Equal(t,
						map[string]string{
							meta.AnnotationKeyReconciliationPaused: True,
							"provider-ceph.backends.s3-backend-1":  True,
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
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cl := fake.NewClientBuilder().
				WithObjects(tc.fields.initObjects...).
				WithStatusSubresource(tc.fields.initObjects...).
				WithScheme(s).Build()

			s3ClientHandler := s3clienthandler.NewHandler(
				s3clienthandler.WithAssumeRoleArn(tc.fields.roleArn),
				s3clienthandler.WithBackendStore(tc.fields.backendStore),
				s3clienthandler.WithKubeClient(cl))

			e := external{
				kubeClient:         cl,
				backendStore:       tc.fields.backendStore,
				s3ClientHandler:    s3ClientHandler,
				autoPauseBucket:    tc.fields.autoPauseBucket,
				minReplicas:        1,
				log:                logging.NewNopLogger(),
				subresourceClients: NewSubresourceClients(tc.fields.backendStore, s3ClientHandler, SubresourceClientConfig{}, logging.NewNopLogger()),
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

//nolint:maintidx // Function requires numerous checks.
func TestUpdateVersioningConfigSubResource(t *testing.T) {
	t.Parallel()
	someError := errors.New("some error")

	type fields struct {
		backendStore    *backendstore.BackendStore
		autoPauseBucket bool
		roleArn         *string
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
		"Two backends update versioning configuration successfully": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{}

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
						ForProvider: v1alpha1.BucketParameters{
							VersioningConfiguration: &v1alpha1.VersioningConfiguration{
								Status: &vEnabled,
							},
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
						bucket.Status.AtProvider.Backends["s3-backend-1"].VersioningConfigurationCondition.Equal(v1.Available()),
						"versioning configuration condition on s3-backend-1 is not available")

					assert.True(t,
						bucket.Status.AtProvider.Backends["s3-backend-2"].VersioningConfigurationCondition.Equal(v1.Available()),
						"versioning configuration condition on s3-backend-2 is not available")
				},
			},
		},
		"Two backends fail to update versioning config": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{
						PutBucketVersioningStub: func(ctx context.Context, hbi *s3.PutBucketVersioningInput, f ...func(*s3.Options)) (*s3.PutBucketVersioningOutput, error) {
							return &s3.PutBucketVersioningOutput{}, someError
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
						ForProvider: v1alpha1.BucketParameters{
							VersioningConfiguration: &v1alpha1.VersioningConfiguration{
								Status: &vEnabled,
							},
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
					unavailableBackends := []string{"s3-backend-1", "s3-backend-2"}
					slices.Sort(unavailableBackends)

					assert.True(t,
						bucket.Status.AtProvider.Backends["s3-backend-1"].VersioningConfigurationCondition.Equal(
							v1.Unavailable().WithMessage(
								errors.Wrap(
									errors.Wrap(someError, "failed to put bucket versioning"),
									"failed to handle bucket versioning configuration").Error(),
							),
						),
						"unexpected versioning configuration condition for s3-backend-1")

					assert.True(t,
						bucket.Status.AtProvider.Backends["s3-backend-2"].VersioningConfigurationCondition.Equal(
							v1.Unavailable().WithMessage(
								errors.Wrap(
									errors.Wrap(someError, "failed to put bucket versioning"),
									"failed to handle bucket versioning configuration").Error(),
							),
						),
						"unexpected versioning configuration condition for s3-backend-2")
				},
			},
		},
		"One backend updates versioning configuration successfully and one fails to update": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fakeErr := backendstorefakes.FakeS3Client{
						PutBucketVersioningStub: func(ctx context.Context, hbi *s3.PutBucketVersioningInput, f ...func(*s3.Options)) (*s3.PutBucketVersioningOutput, error) {
							return &s3.PutBucketVersioningOutput{}, someError
						},
					}
					fakeOK := backendstorefakes.FakeS3Client{}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", &fakeOK, nil, true, apisv1alpha1.HealthStatusHealthy)
					bs.AddOrUpdateBackend("s3-backend-2", &fakeErr, nil, true, apisv1alpha1.HealthStatusHealthy)

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
						ForProvider: v1alpha1.BucketParameters{
							VersioningConfiguration: &v1alpha1.VersioningConfiguration{
								Status: &vEnabled,
							},
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
						bucket.Status.AtProvider.Backends["s3-backend-1"].VersioningConfigurationCondition.Equal(v1.Available()),
						"unexpected versioning configuration condition for s3-backend-1")

					assert.True(t,
						bucket.Status.AtProvider.Backends["s3-backend-2"].VersioningConfigurationCondition.Equal(
							v1.Unavailable().WithMessage(
								errors.Wrap(
									errors.Wrap(someError, "failed to put bucket versioning"),
									"failed to handle bucket versioning configuration").Error(),
							),
						),
						"unexpected versioning configuration condition for s3-backend-2")
				},
			},
		},
		"Single backend updates versioning configuration successfully and is autopaused": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{}

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
						ForProvider: v1alpha1.BucketParameters{
							VersioningConfiguration: &v1alpha1.VersioningConfiguration{
								Status: &vEnabled,
							},
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
						"unexpected bucket ready condition")

					assert.True(t,
						bucket.Status.Conditions[1].Equal(v1.ReconcileSuccess()),
						"unexpected bucket synced condition")

					assert.True(t,
						bucket.Status.AtProvider.Backends["s3-backend-1"].VersioningConfigurationCondition.Equal(v1.Available()),
						"versioning configuration condition on s3-backend-1 is not available")

					assert.Equal(t,
						map[string]string{
							meta.AnnotationKeyReconciliationPaused: True,
							"provider-ceph.backends.s3-backend-1":  True,
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
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cl := fake.NewClientBuilder().
				WithObjects(tc.fields.initObjects...).
				WithStatusSubresource(tc.fields.initObjects...).
				WithScheme(s).Build()

			s3ClientHandler := s3clienthandler.NewHandler(
				s3clienthandler.WithAssumeRoleArn(tc.fields.roleArn),
				s3clienthandler.WithBackendStore(tc.fields.backendStore),
				s3clienthandler.WithKubeClient(cl))

			e := external{
				kubeClient:         cl,
				backendStore:       tc.fields.backendStore,
				s3ClientHandler:    s3ClientHandler,
				autoPauseBucket:    tc.fields.autoPauseBucket,
				minReplicas:        1,
				log:                logging.NewNopLogger(),
				subresourceClients: NewSubresourceClients(tc.fields.backendStore, s3ClientHandler, SubresourceClientConfig{}, logging.NewNopLogger()),
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

//nolint:maintidx // Function requires numerous checks.
func TestUpdateObjectLockConfigSubResource(t *testing.T) {
	t.Parallel()
	someError := errors.New("some error")

	type fields struct {
		backendStore    *backendstore.BackendStore
		autoPauseBucket bool
		roleArn         *string
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
		"Two backends update object lock configuration successfully": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{}

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
						ForProvider: v1alpha1.BucketParameters{
							ObjectLockEnabledForBucket: &enabledTrue,
							ObjectLockConfiguration: &v1alpha1.ObjectLockConfiguration{
								ObjectLockEnabled: &lEnabled,
							},
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
						bucket.Status.AtProvider.Backends["s3-backend-1"].ObjectLockConfigurationCondition.Equal(v1.Available()),

						"object lock configuration condition on s3-backend-1 is not available")

					assert.True(t,
						bucket.Status.AtProvider.Backends["s3-backend-2"].ObjectLockConfigurationCondition.Equal(v1.Available()),
						"object lock configuration condition on s3-backend-2 is not available")
				},
			},
		},
		"Two backends fail to update versioning config": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{
						PutObjectLockConfigurationStub: func(ctx context.Context, hbi *s3.PutObjectLockConfigurationInput, f ...func(*s3.Options)) (*s3.PutObjectLockConfigurationOutput, error) {
							return &s3.PutObjectLockConfigurationOutput{}, someError
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
						ForProvider: v1alpha1.BucketParameters{
							ObjectLockEnabledForBucket: &enabledTrue,
							ObjectLockConfiguration: &v1alpha1.ObjectLockConfiguration{
								ObjectLockEnabled: &lEnabled,
							},
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
					unavailableBackends := []string{"s3-backend-1", "s3-backend-2"}
					slices.Sort(unavailableBackends)

					assert.True(t,
						bucket.Status.AtProvider.Backends["s3-backend-1"].ObjectLockConfigurationCondition.Equal(
							v1.Unavailable().WithMessage(
								errors.Wrap(
									errors.Wrap(someError, "failed to put object lock configuration"),
									"failed to handle object lock configuration").Error(),
							),
						),
						"unexpected object lock configuration condition for s3-backend-1")

					assert.True(t,
						bucket.Status.AtProvider.Backends["s3-backend-2"].ObjectLockConfigurationCondition.Equal(
							v1.Unavailable().WithMessage(
								errors.Wrap(
									errors.Wrap(someError, "failed to put object lock configuration"),
									"failed to handle object lock configuration").Error(),
							),
						),
						"unexpected object lock configuration condition for s3-backend-1")
				},
			},
		},
		"One backend updates object lock configuration successfully and one fails to update": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fakeErr := backendstorefakes.FakeS3Client{
						PutObjectLockConfigurationStub: func(ctx context.Context, hbi *s3.PutObjectLockConfigurationInput, f ...func(*s3.Options)) (*s3.PutObjectLockConfigurationOutput, error) {
							return &s3.PutObjectLockConfigurationOutput{}, someError
						},
					}
					fakeOK := backendstorefakes.FakeS3Client{}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", &fakeOK, nil, true, apisv1alpha1.HealthStatusHealthy)
					bs.AddOrUpdateBackend("s3-backend-2", &fakeErr, nil, true, apisv1alpha1.HealthStatusHealthy)

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
						ForProvider: v1alpha1.BucketParameters{
							ObjectLockEnabledForBucket: &enabledTrue,
							ObjectLockConfiguration: &v1alpha1.ObjectLockConfiguration{
								ObjectLockEnabled: &lEnabled,
							},
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
						bucket.Status.AtProvider.Backends["s3-backend-1"].ObjectLockConfigurationCondition.Equal(v1.Available()),
						"unexpected object lock configuration condition for s3-backend-1")

					assert.True(t,
						bucket.Status.AtProvider.Backends["s3-backend-2"].ObjectLockConfigurationCondition.Equal(
							v1.Unavailable().WithMessage(
								errors.Wrap(
									errors.Wrap(someError, "failed to put object lock configuration"),
									"failed to handle object lock configuration").Error(),
							),
						),
						"unexpected object lock configuration condition for s3-backend-1")
				},
			},
		},
		"Single backend updates object lock configuration successfully and is autopaused": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{}

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
						ForProvider: v1alpha1.BucketParameters{
							ObjectLockEnabledForBucket: &enabledTrue,
							ObjectLockConfiguration: &v1alpha1.ObjectLockConfiguration{
								ObjectLockEnabled: &lEnabled,
							},
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
						"unexpected bucket ready condition")

					assert.True(t,
						bucket.Status.Conditions[1].Equal(v1.ReconcileSuccess()),
						"unexpected bucket synced condition")

					assert.True(t,
						bucket.Status.AtProvider.Backends["s3-backend-1"].ObjectLockConfigurationCondition.Equal(v1.Available()),
						"object lock configuration condition on s3-backend-1 is not available")

					assert.Equal(t,
						map[string]string{
							meta.AnnotationKeyReconciliationPaused: True,
							"provider-ceph.backends.s3-backend-1":  True,
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
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cl := fake.NewClientBuilder().
				WithObjects(tc.fields.initObjects...).
				WithStatusSubresource(tc.fields.initObjects...).
				WithScheme(s).Build()

			s3ClientHandler := s3clienthandler.NewHandler(
				s3clienthandler.WithAssumeRoleArn(tc.fields.roleArn),
				s3clienthandler.WithBackendStore(tc.fields.backendStore),
				s3clienthandler.WithKubeClient(cl))

			e := external{
				kubeClient:         cl,
				backendStore:       tc.fields.backendStore,
				s3ClientHandler:    s3ClientHandler,
				autoPauseBucket:    tc.fields.autoPauseBucket,
				minReplicas:        1,
				log:                logging.NewNopLogger(),
				subresourceClients: NewSubresourceClients(tc.fields.backendStore, s3ClientHandler, SubresourceClientConfig{}, logging.NewNopLogger()),
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
