package bucket

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	v1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/google/go-cmp/cmp"
	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	"github.com/linode/provider-ceph/internal/backendstore/backendstorefakes"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

//nolint:maintidx,paralleltest // Function requires numerous checks. Running in parallel causes issues with client.
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
		statusDiff func(mg resource.Managed) string
		err        error
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
				err: errors.New(errNoS3BackendsRegistered),
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
				statusDiff: func(mg resource.Managed) string {
					bucket, _ := mg.(*v1alpha1.Bucket)

					return cmp.Diff(
						v1alpha1.BucketStatus{
							ResourceStatus: v1.ResourceStatus{
								ConditionedStatus: v1.ConditionedStatus{
									Conditions: []v1.Condition{
										{
											Type:   "Ready",
											Status: "True",
											Reason: "Available",
										},
									},
								},
							},
							AtProvider: v1alpha1.BucketObservation{
								Backends: v1alpha1.Backends{
									"s3-backend-1": &v1alpha1.BackendInfo{
										BucketStatus: v1alpha1.ReadyStatus,
									},
								},
							},
						},
						bucket.Status,
					)
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
				statusDiff: func(mg resource.Managed) string {
					bucket, _ := mg.(*v1alpha1.Bucket)

					return cmp.Diff(
						v1alpha1.BucketStatus{
							ResourceStatus: v1.ResourceStatus{
								ConditionedStatus: v1.ConditionedStatus{
									Conditions: []v1.Condition{
										{
											Type:   "Ready",
											Status: "True",
											Reason: "Available",
										},
									},
								},
							},
							AtProvider: v1alpha1.BucketObservation{
								Backends: v1alpha1.Backends{
									"s3-backend-3": &v1alpha1.BackendInfo{
										BucketStatus: v1alpha1.ReadyStatus,
									},
								},
							},
						},
						bucket.Status,
					)
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
				statusDiff: func(mg resource.Managed) string {
					bucket, _ := mg.(*v1alpha1.Bucket)

					return cmp.Diff(
						v1alpha1.BucketStatus{
							ResourceStatus: v1.ResourceStatus{
								ConditionedStatus: v1.ConditionedStatus{
									Conditions: []v1.Condition{
										{
											Type:   "Ready",
											Status: "False",
											Reason: "Unavailable",
										},
									},
								},
							},
						},
						bucket.Status,
					)
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

			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\ne.Create(...): -want error, +got error:\n%s\n", tc.reason, diff)
			}
			if diff := cmp.Diff(tc.want.o, got); diff != "" {
				t.Errorf("\n%s\ne.Create(...): -want, +got:\n%s\n", tc.reason, diff)
			}
			if tc.want.statusDiff != nil {
				if diff := tc.want.statusDiff(tc.args.mg); diff != "" {
					t.Errorf("\n%s\ne.Create(...): -want, +got:\n%s\n", tc.reason, diff)
				}
			}
		})
	}
}
