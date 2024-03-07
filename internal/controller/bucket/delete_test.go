package bucket

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
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
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	s3Backend1 = "s3-backend-1"
	s3Backend2 = "s3-backend-2"
)

func TestDeleteBasicErrors(t *testing.T) {
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
		"Managed resource is not a Bucket": {
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
		"S3 backend reference does not exist": {
			fields: fields{
				backendStore: backendstore.NewBackendStore(),
			},
			args: args{
				mg: &v1alpha1.Bucket{
					Spec: v1alpha1.BucketSpec{
						ResourceSpec: xpv1.ResourceSpec{
							ProviderConfigReference: &xpv1.Reference{
								Name: s3Backend1,
							},
						},
					},
				},
			},
			want: want{
				err: errors.New(errNoS3BackendsStored),
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

			err := e.Delete(context.Background(), tc.args.mg)
			require.EqualError(t, err, tc.want.err.Error(), "unexpected error")
		})
	}
}

//nolint:maintidx,paralleltest // Function requires numerous checks. Running in parallel causes issues with client.
func TestDelete(t *testing.T) {
	errRandomStr := "some err"
	errRandom := errors.New(errRandomStr)
	roleArn := "role-arn"

	type fields struct {
		backendStore *backendstore.BackendStore
		roleArn      *string
	}

	type args struct {
		mg resource.Managed
	}

	type want struct {
		err           error
		statusDiff    func(t *testing.T, mg resource.Managed)
		finalizerDiff func(t *testing.T, mg resource.Managed)
	}

	cases := map[string]struct {
		reason string
		fields fields
		args   args
		want   want
	}{
		"Delete buckets on specified backends": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					// DeleteBucket first calls HeadBucket to establish
					// if a bucket exists, so return not found
					// error to short circuit a successful delete.
					var notFoundError *s3types.NotFound
					fakeClient := &backendstorefakes.FakeS3Client{}
					fakeClient.HeadBucketReturns(
						&s3.HeadBucketOutput{},
						notFoundError,
					)

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend(s3Backend1, fakeClient, nil, true, apisv1alpha1.HealthStatusHealthy)
					bs.AddOrUpdateBackend(s3Backend2, fakeClient, nil, true, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				mg: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "test-bucket",
						Finalizers: []string{v1alpha1.InUseFinalizer},
						Labels: map[string]string{
							v1alpha1.BackendLabelPrefix + s3Backend1: "true",
							v1alpha1.BackendLabelPrefix + s3Backend2: "true",
						},
					},
					Spec: v1alpha1.BucketSpec{
						Providers: []string{s3Backend1, s3Backend2},
					},
					Status: v1alpha1.BucketStatus{
						AtProvider: v1alpha1.BucketObservation{
							Backends: v1alpha1.Backends{
								s3Backend1: &v1alpha1.BackendInfo{
									BucketCondition: xpv1.Available(),
								},
								s3Backend2: &v1alpha1.BackendInfo{
									BucketCondition: xpv1.Available(),
								},
							},
						},
					},
				},
			},
			want: want{
				err: nil,
				statusDiff: func(t *testing.T, mg resource.Managed) {
					t.Helper()
					bucket, _ := mg.(*v1alpha1.Bucket)

					// s3-backend-1 was successfully deleted so was removed from status.
					assert.False(t,
						func(b v1alpha1.Backends) bool {
							if _, ok := b[s3Backend1]; ok {
								return true
							}

							return false
						}(bucket.Status.AtProvider.Backends),
						"s3-backend-1 should not exist in backends")

					// s3-backend-2 was successfully deleted so was removed from status.
					assert.False(t,
						func(b v1alpha1.Backends) bool {
							if _, ok := b[s3Backend2]; ok {
								return true
							}

							return false
						}(bucket.Status.AtProvider.Backends),
						"s3-backend-2 should not exist in backends")
				},
				finalizerDiff: func(t *testing.T, mg resource.Managed) {
					t.Helper()
					bucket, _ := mg.(*v1alpha1.Bucket)

					assert.Empty(t,
						len(bucket.Finalizers),
						"unexpeceted finalizers",
					)
				},
			},
		},
		"Delete buckets on all backends": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					// DeleteBucket first calls HeadBucket to establish
					// if a bucket exists, so return not found
					// error to short circuit a successful delete.
					var notFoundError *s3types.NotFound
					fakeClient := &backendstorefakes.FakeS3Client{}
					fakeClient.HeadBucketReturns(
						&s3.HeadBucketOutput{},
						notFoundError,
					)

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend(s3Backend1, fakeClient, nil, true, apisv1alpha1.HealthStatusHealthy)
					bs.AddOrUpdateBackend(s3Backend2, fakeClient, nil, true, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				mg: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "test-bucket",
						Finalizers: []string{v1alpha1.InUseFinalizer},
						Labels: map[string]string{
							v1alpha1.BackendLabelPrefix + s3Backend1: "true",
							v1alpha1.BackendLabelPrefix + s3Backend2: "true",
						},
					},
					Spec: v1alpha1.BucketSpec{
						// No backends specified, so delete on all backends.
						Providers: []string{},
					},
					Status: v1alpha1.BucketStatus{
						AtProvider: v1alpha1.BucketObservation{
							Backends: v1alpha1.Backends{
								s3Backend1: &v1alpha1.BackendInfo{
									BucketCondition: xpv1.Available(),
								},
								s3Backend2: &v1alpha1.BackendInfo{
									BucketCondition: xpv1.Available(),
								},
							},
						},
					},
				},
			},
			want: want{
				err: nil,
				statusDiff: func(t *testing.T, mg resource.Managed) {
					t.Helper()
					bucket, _ := mg.(*v1alpha1.Bucket)

					// s3-backend-1 was successfully deleted so was removed from status.
					assert.False(t,
						func(b v1alpha1.Backends) bool {
							if _, ok := b[s3Backend1]; ok {
								return true
							}

							return false
						}(bucket.Status.AtProvider.Backends),
						"s3-backend-1 should not exist in backends")

					// s3-backend-2 was successfully deleted so was removed from status.
					assert.False(t,
						func(b v1alpha1.Backends) bool {
							if _, ok := b[s3Backend2]; ok {
								return true
							}

							return false
						}(bucket.Status.AtProvider.Backends),
						"s3-backend-2 should not exist in backends")
				},
				finalizerDiff: func(t *testing.T, mg resource.Managed) {
					t.Helper()
					bucket, _ := mg.(*v1alpha1.Bucket)

					assert.Empty(t,
						len(bucket.Finalizers),
						"unexpeceted finalizers",
					)
				},
			},
		},
		"Error deleting buckets on all specified backends": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					// DeleteBucket first calls HeadBucket to establish
					// if a bucket exists, so return a random error
					// to mimic a failed delete.
					fakeClient := &backendstorefakes.FakeS3Client{}
					fakeClient.HeadBucketReturns(
						&s3.HeadBucketOutput{},
						errRandom,
					)

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend(s3Backend1, fakeClient, nil, true, apisv1alpha1.HealthStatusHealthy)
					bs.AddOrUpdateBackend(s3Backend2, fakeClient, nil, true, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				mg: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "bucket",
						Finalizers: []string{v1alpha1.InUseFinalizer},
						Labels: map[string]string{
							v1alpha1.BackendLabelPrefix + s3Backend1: "true",
							v1alpha1.BackendLabelPrefix + s3Backend2: "true",
						},
					},
					Spec: v1alpha1.BucketSpec{
						Providers: []string{
							s3Backend1,
							s3Backend2,
						},
					},
					Status: v1alpha1.BucketStatus{
						AtProvider: v1alpha1.BucketObservation{
							Backends: v1alpha1.Backends{
								s3Backend1: &v1alpha1.BackendInfo{
									BucketCondition: xpv1.Available(),
								},
								s3Backend2: &v1alpha1.BackendInfo{
									BucketCondition: xpv1.Available(),
								},
							},
						},
					},
				},
			},
			want: want{
				err: errRandom,
				statusDiff: func(t *testing.T, mg resource.Managed) {
					t.Helper()
					bucket, _ := mg.(*v1alpha1.Bucket)

					assert.True(t,
						bucket.Status.AtProvider.Backends[s3Backend1].BucketCondition.Equal(xpv1.Deleting().WithMessage(errors.Wrap(errRandom, "failed to perform head bucket").Error())),
						"unexpected bucket condition on s3-backend-1")
					assert.True(t,
						bucket.Status.AtProvider.Backends[s3Backend2].BucketCondition.Equal(xpv1.Deleting().WithMessage(errors.Wrap(errRandom, "failed to perform head bucket").Error())),
						"unexpected bucket condition on s3-backend-2")
				},
				finalizerDiff: func(t *testing.T, mg resource.Managed) {
					t.Helper()
					bucket, _ := mg.(*v1alpha1.Bucket)

					assert.Equal(t,
						[]string{v1alpha1.InUseFinalizer},
						bucket.Finalizers,
						"unexpected finalizers",
					)
				},
			},
		},
		"Error deleting buckets on all backends": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					// DeleteBucket first calls HeadBucket to establish
					// if a bucket exists, so return a random error
					// to mimic a failed delete.
					fakeClient := &backendstorefakes.FakeS3Client{}
					fakeClient.HeadBucketReturns(
						&s3.HeadBucketOutput{},
						errRandom,
					)

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend(s3Backend1, fakeClient, nil, true, apisv1alpha1.HealthStatusHealthy)
					bs.AddOrUpdateBackend(s3Backend2, fakeClient, nil, true, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				mg: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "bucket",
						Finalizers: []string{v1alpha1.InUseFinalizer},
						Labels: map[string]string{
							v1alpha1.BackendLabelPrefix + s3Backend1: "true",
							v1alpha1.BackendLabelPrefix + s3Backend2: "true",
						},
					},
					Spec: v1alpha1.BucketSpec{
						Providers: []string{},
					},
					Status: v1alpha1.BucketStatus{
						AtProvider: v1alpha1.BucketObservation{
							Backends: v1alpha1.Backends{
								s3Backend1: &v1alpha1.BackendInfo{
									BucketCondition: xpv1.Available(),
								},
								s3Backend2: &v1alpha1.BackendInfo{
									BucketCondition: xpv1.Available(),
								},
							},
						},
					},
				},
			},
			want: want{
				err: errRandom,
				statusDiff: func(t *testing.T, mg resource.Managed) {
					t.Helper()
					bucket, _ := mg.(*v1alpha1.Bucket)
					assert.True(t,
						bucket.Status.AtProvider.Backends[s3Backend1].BucketCondition.Equal(xpv1.Deleting().WithMessage(errors.Wrap(errRandom, "failed to perform head bucket").Error())),
						"unexpected bucket condition on s3-backend-1")
					assert.True(t,
						bucket.Status.AtProvider.Backends[s3Backend2].BucketCondition.Equal(xpv1.Deleting().WithMessage(errors.Wrap(errRandom, "failed to perform head bucket").Error())),
						"unexpected bucket condition on s3-backend-2")
				},
				finalizerDiff: func(t *testing.T, mg resource.Managed) {
					t.Helper()
					bucket, _ := mg.(*v1alpha1.Bucket)

					assert.Equal(t,
						[]string{v1alpha1.InUseFinalizer},
						bucket.Finalizers,
						"unexpected finalizers",
					)
				},
			},
		},

		"Error deleting buckets on all backends because assume role fails for sts client": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeSTSClient{
						AssumeRoleStub: func(ctx context.Context, ari *sts.AssumeRoleInput, f ...func(*sts.Options)) (*sts.AssumeRoleOutput, error) {
							return &sts.AssumeRoleOutput{}, errRandom
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend(s3Backend1, nil, &fake, true, apisv1alpha1.HealthStatusHealthy)
					bs.AddOrUpdateBackend(s3Backend2, nil, &fake, true, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
				roleArn: &roleArn,
			},
			args: args{
				mg: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "bucket",
						Finalizers: []string{v1alpha1.InUseFinalizer},
						Labels: map[string]string{
							v1alpha1.BackendLabelPrefix + s3Backend1: "true",
							v1alpha1.BackendLabelPrefix + s3Backend2: "true",
						},
					},
					Spec: v1alpha1.BucketSpec{
						Providers: []string{},
					},
					Status: v1alpha1.BucketStatus{
						AtProvider: v1alpha1.BucketObservation{
							Backends: v1alpha1.Backends{
								s3Backend1: &v1alpha1.BackendInfo{
									BucketCondition: xpv1.Available(),
								},
								s3Backend2: &v1alpha1.BackendInfo{
									BucketCondition: xpv1.Available(),
								},
							},
						},
					},
				},
			},
			want: want{
				err: errRandom,
				statusDiff: func(t *testing.T, mg resource.Managed) {
					t.Helper()
					bucket, _ := mg.(*v1alpha1.Bucket)
					assert.True(t,
						bucket.Status.AtProvider.Backends[s3Backend1].BucketCondition.Equal(xpv1.Deleting().WithMessage(errors.Wrap(errors.Wrap(errRandom, "failed to assume role"), "Failed to create s3 client via assume role").Error())),
						"unexpected bucket condition on s3-backend-1")
					assert.True(t,
						bucket.Status.AtProvider.Backends[s3Backend2].BucketCondition.Equal(xpv1.Deleting().WithMessage(errors.Wrap(errors.Wrap(errRandom, "failed to assume role"), "Failed to create s3 client via assume role").Error())),
						"unexpected bucket condition on s3-backend-2")
				},
				finalizerDiff: func(t *testing.T, mg resource.Managed) {
					t.Helper()
					bucket, _ := mg.(*v1alpha1.Bucket)

					assert.Equal(t,
						[]string{v1alpha1.InUseFinalizer},
						bucket.Finalizers,
						"unexpected finalizers",
					)
				},
			},
		},

		"Error deleting bucket on only one specified backend": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					// DeleteBucket first calls HeadBucket to establish
					// if a bucket exists, so return a random error
					// to mimic a failed delete.
					fakeClient := &backendstorefakes.FakeS3Client{}
					fakeClient.HeadBucketReturns(
						&s3.HeadBucketOutput{},
						errRandom,
					)

					// DeleteBucket first calls HeadBucket to establish
					// if a bucket exists, so return not found
					// error to short circuit a successful delete.
					var notFoundError *s3types.NotFound
					fakeClientOK := &backendstorefakes.FakeS3Client{}
					fakeClientOK.HeadBucketReturns(
						&s3.HeadBucketOutput{},
						notFoundError,
					)

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend(s3Backend1, fakeClient, nil, true, apisv1alpha1.HealthStatusHealthy)
					bs.AddOrUpdateBackend(s3Backend2, fakeClientOK, nil, true, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				mg: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "bucket",
						Finalizers: []string{v1alpha1.InUseFinalizer},
						Labels: map[string]string{
							v1alpha1.BackendLabelPrefix + s3Backend1: "true",
							v1alpha1.BackendLabelPrefix + s3Backend2: "true",
						},
					},
					Spec: v1alpha1.BucketSpec{
						Providers: []string{
							s3Backend1,
							s3Backend2,
						},
					},
					Status: v1alpha1.BucketStatus{
						AtProvider: v1alpha1.BucketObservation{
							Backends: v1alpha1.Backends{
								s3Backend1: &v1alpha1.BackendInfo{
									BucketCondition: xpv1.Available(),
								},
								s3Backend2: &v1alpha1.BackendInfo{
									BucketCondition: xpv1.Available(),
								},
							},
						},
					},
				},
			},
			want: want{
				err: errRandom,
				statusDiff: func(t *testing.T, mg resource.Managed) {
					t.Helper()
					bucket, _ := mg.(*v1alpha1.Bucket)

					// s3-backend-1 failed so is stuck in Deleting status.
					assert.True(t,
						bucket.Status.AtProvider.Backends[s3Backend1].BucketCondition.Equal(xpv1.Deleting().WithMessage(errors.Wrap(errRandom, "failed to perform head bucket").Error())),
						"unexpected bucket condition on s3-backend-1")

					// s3-backend-2 was successfully deleted so was removed from status.
					assert.False(t,
						func(b v1alpha1.Backends) bool {
							if _, ok := b[s3Backend2]; ok {
								return true
							}

							return false
						}(bucket.Status.AtProvider.Backends),
						"s3-backend-2 should not exist in backends")
				},
				finalizerDiff: func(t *testing.T, mg resource.Managed) {
					t.Helper()
					bucket, _ := mg.(*v1alpha1.Bucket)

					assert.Equal(t,
						[]string{v1alpha1.InUseFinalizer},
						bucket.Finalizers,
						"unexpeceted finalizers",
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
			kubeClient := fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(tc.args.mg).
				WithStatusSubresource(tc.args.mg).
				Build()

			e := external{
				backendStore: tc.fields.backendStore,
				s3ClientHandler: s3clienthandler.NewHandler(
					s3clienthandler.WithAssumeRoleArn(tc.fields.roleArn),
					s3clienthandler.WithBackendStore(tc.fields.backendStore),
					s3clienthandler.WithKubeClient(kubeClient)),
				log:        logging.NewNopLogger(),
				kubeClient: kubeClient,
			}

			err := e.Delete(context.Background(), tc.args.mg)
			require.ErrorIs(t, err, tc.want.err, "unexpected err")
			if tc.want.statusDiff != nil {
				tc.want.statusDiff(t, tc.args.mg)
			}
			if tc.want.finalizerDiff != nil {
				tc.want.finalizerDiff(t, tc.args.mg)
			}
		})
	}
}
