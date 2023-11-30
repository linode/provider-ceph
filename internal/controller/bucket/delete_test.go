package bucket

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	v1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/google/go-cmp/cmp"
	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	"github.com/linode/provider-ceph/internal/backendstore/backendstorefakes"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

//nolint:maintidx,paralleltest // Function requires numerous checks. Running in parallel causes issues with client.
func TestDelete(t *testing.T) {
	type fields struct {
		backendStore *backendstore.BackendStore
	}

	type args struct {
		mg resource.Managed
	}

	type want struct {
		err           error
		statusDiff    func(mg resource.Managed) string
		finalizerDiff func(mg resource.Managed) string
	}

	cases := map[string]struct {
		reason string
		fields fields
		args   args
		want   want
	}{
		"S3 backend reference does not exist": {
			fields: fields{
				backendStore: backendstore.NewBackendStore(),
			},
			args: args{
				mg: &v1alpha1.Bucket{
					Spec: v1alpha1.BucketSpec{
						ResourceSpec: v1.ResourceSpec{
							ProviderConfigReference: &v1.Reference{
								Name: "s3-backend-1",
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
					bs.AddOrUpdateBackend("s3-backend-1", fakeClient, true, apisv1alpha1.HealthStatusHealthy)
					bs.AddOrUpdateBackend("s3-backend-2", fakeClient, true, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				mg: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-bucket",
					},
					Spec: v1alpha1.BucketSpec{
						Providers: []string{"s3-backend-1", "s3-backend-2"},
					},
				},
			},
			want: want{
				err: nil,
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
					bs.AddOrUpdateBackend("s3-backend-1", fakeClient, true, apisv1alpha1.HealthStatusHealthy)
					bs.AddOrUpdateBackend("s3-backend-2", fakeClient, true, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				mg: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-bucket",
					},
					Spec: v1alpha1.BucketSpec{
						// No backends specified, so delete on all backends.
						Providers: []string{},
					},
				},
			},
			want: want{
				err: nil,
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
						errors.New("some error"),
					)

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", fakeClient, true, apisv1alpha1.HealthStatusHealthy)
					bs.AddOrUpdateBackend("s3-backend-2", fakeClient, true, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				mg: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Finalizers: []string{inUseFinalizer},
					},
					Spec: v1alpha1.BucketSpec{
						Providers: []string{
							"s3-backend-1",
							"s3-backend-2",
						},
					},
					Status: v1alpha1.BucketStatus{
						AtProvider: v1alpha1.BucketObservation{
							Backends: v1alpha1.Backends{
								"s3-backend-1": &v1alpha1.BackendInfo{
									BucketStatus: v1alpha1.ReadyStatus,
								},
								"s3-backend-2": &v1alpha1.BackendInfo{
									BucketStatus: v1alpha1.ReadyStatus,
								},
							},
						},
					},
				},
			},
			want: want{
				err: errors.Wrap(errors.New("some error"), errDeleteBucket),
				statusDiff: func(mg resource.Managed) string {
					bucket, _ := mg.(*v1alpha1.Bucket)

					return cmp.Diff(
						v1alpha1.Backends{
							"s3-backend-1": &v1alpha1.BackendInfo{
								BucketStatus: v1alpha1.DeletingStatus,
							},
							"s3-backend-2": &v1alpha1.BackendInfo{
								BucketStatus: v1alpha1.DeletingStatus,
							},
						},
						bucket.Status.AtProvider.Backends,
					)
				},
				finalizerDiff: func(mg resource.Managed) string {
					bucket, _ := mg.(*v1alpha1.Bucket)

					return cmp.Diff(
						[]string{inUseFinalizer},
						bucket.Finalizers,
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
						errors.New("some error"),
					)

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", fakeClient, true, apisv1alpha1.HealthStatusHealthy)
					bs.AddOrUpdateBackend("s3-backend-2", fakeClient, true, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				mg: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Finalizers: []string{inUseFinalizer},
					},
					Spec: v1alpha1.BucketSpec{
						Providers: []string{},
					},
					Status: v1alpha1.BucketStatus{
						AtProvider: v1alpha1.BucketObservation{
							Backends: v1alpha1.Backends{
								"s3-backend-1": &v1alpha1.BackendInfo{
									BucketStatus: v1alpha1.ReadyStatus,
								},
								"s3-backend-2": &v1alpha1.BackendInfo{
									BucketStatus: v1alpha1.ReadyStatus,
								},
							},
						},
					},
				},
			},
			want: want{
				err: errors.Wrap(errors.New("some error"), errDeleteBucket),
				statusDiff: func(mg resource.Managed) string {
					bucket, _ := mg.(*v1alpha1.Bucket)

					return cmp.Diff(
						v1alpha1.Backends{
							"s3-backend-1": &v1alpha1.BackendInfo{
								BucketStatus: v1alpha1.DeletingStatus,
							},
							"s3-backend-2": &v1alpha1.BackendInfo{
								BucketStatus: v1alpha1.DeletingStatus,
							},
						},
						bucket.Status.AtProvider.Backends,
					)
				},
				finalizerDiff: func(mg resource.Managed) string {
					bucket, _ := mg.(*v1alpha1.Bucket)

					return cmp.Diff(
						[]string{inUseFinalizer},
						bucket.Finalizers,
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
						errors.New("some error"),
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
					bs.AddOrUpdateBackend("s3-backend-1", fakeClient, true, apisv1alpha1.HealthStatusHealthy)
					bs.AddOrUpdateBackend("s3-backend-2", fakeClientOK, true, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				mg: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Finalizers: []string{inUseFinalizer},
					},
					Spec: v1alpha1.BucketSpec{
						Providers: []string{
							"s3-backend-1",
							"s3-backend-2",
						},
					},
					Status: v1alpha1.BucketStatus{
						AtProvider: v1alpha1.BucketObservation{
							Backends: v1alpha1.Backends{
								"s3-backend-1": &v1alpha1.BackendInfo{
									BucketStatus: v1alpha1.ReadyStatus,
								},
								"s3-backend-2": &v1alpha1.BackendInfo{
									BucketStatus: v1alpha1.ReadyStatus,
								},
							},
						},
					},
				},
			},
			want: want{
				err: errors.Wrap(errors.New("some error"), errDeleteBucket),
				statusDiff: func(mg resource.Managed) string {
					bucket, _ := mg.(*v1alpha1.Bucket)

					return cmp.Diff(
						v1alpha1.Backends{
							// s3-backend-1 failed so is stuck in Deleting status.
							// s3-backend-2 was successful so was removed from status.
							"s3-backend-1": &v1alpha1.BackendInfo{
								BucketStatus: v1alpha1.DeletingStatus,
							},
						},
						bucket.Status.AtProvider.Backends,
					)
				},
				finalizerDiff: func(mg resource.Managed) string {
					bucket, _ := mg.(*v1alpha1.Bucket)

					return cmp.Diff(
						[]string{inUseFinalizer},
						bucket.Finalizers,
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
				log:          logging.NewNopLogger(),
				kubeClient:   kubeClient,
			}

			err := e.Delete(context.Background(), tc.args.mg)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\ne.Delete(...): -want error, +got error:\n%s\n", tc.reason, diff)
			}

			if tc.want.statusDiff != nil {
				if diff := tc.want.statusDiff(tc.args.mg); diff != "" {
					t.Errorf("\n%s\ne.Delete(...): -want, +got:\n%s\n", tc.reason, diff)
				}
			}
			if tc.want.finalizerDiff != nil {
				if diff := tc.want.finalizerDiff(tc.args.mg); diff != "" {
					t.Errorf("\n%s\ne.Delete(...): -want, +got:\n%s\n", tc.reason, diff)
				}
			}
		})
	}
}
