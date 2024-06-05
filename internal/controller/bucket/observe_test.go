package bucket

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
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
)

var (
	unexpectedItem resource.Managed
)

func TestObserveBasicErrors(t *testing.T) {
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
	}
	for name, tc := range cases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			e := external{backendStore: tc.fields.backendStore, log: logging.NewNopLogger()}
			_, err := e.Observe(context.Background(), tc.args.mg)
			require.EqualError(t, err, tc.want.err.Error(), "unexpected error")
		})
	}
}

//nolint:maintidx // Function requires numerous checks.
func TestObserve(t *testing.T) {
	t.Parallel()

	type fields struct {
		backendStore    *backendstore.BackendStore
		autoPauseBucket bool
	}

	type args struct {
		mg resource.Managed
	}

	type want struct {
		o   managed.ExternalObservation
		err error
	}

	cases := map[string]struct {
		reason string
		fields fields
		args   args
		want   want
	}{
		"Bucket doesn't have any living backend": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", nil, nil, true, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
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
					Status: v1alpha1.BucketStatus{
						AtProvider: v1alpha1.BucketObservation{
							Backends: v1alpha1.Backends{
								"s3-backend-1": &v1alpha1.BackendInfo{
									BucketCondition: v1.Available(),
								},
							},
						},
					},
				},
			},
			want: want{
				o: managed.ExternalObservation{
					ResourceExists:   true,
					ResourceUpToDate: false,
				},
			},
		},
		"Bucket status is not available": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", nil, nil, true, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
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
					Status: v1alpha1.BucketStatus{
						AtProvider: v1alpha1.BucketObservation{
							Backends: v1alpha1.Backends{
								"s3-backend-1": &v1alpha1.BackendInfo{
									BucketCondition: v1.Available(),
								},
							},
						},
						ResourceStatus: v1.ResourceStatus{
							ConditionedStatus: v1.ConditionedStatus{
								Conditions: []v1.Condition{
									v1.Creating(),
								},
							},
						},
					},
				},
			},
			want: want{
				o: managed.ExternalObservation{
					ResourceExists:   true,
					ResourceUpToDate: false,
				},
			},
		},
		"One of the backends is not ready": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", nil, nil, true, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				mg: &v1alpha1.Bucket{
					Spec: v1alpha1.BucketSpec{
						Providers: []string{"s3-backend-1"},
						ResourceSpec: v1.ResourceSpec{
							ProviderConfigReference: &v1.Reference{
								Name: "s3-backend-1",
							},
						},
					},
					Status: v1alpha1.BucketStatus{
						AtProvider: v1alpha1.BucketObservation{
							Backends: v1alpha1.Backends{
								"s3-backend-1": &v1alpha1.BackendInfo{
									BucketCondition: v1.Unavailable(),
								},
							},
						},
						ResourceStatus: v1.ResourceStatus{
							ConditionedStatus: v1.ConditionedStatus{
								Conditions: []v1.Condition{
									v1.Available(),
								},
							},
						},
					},
				},
			},
			want: want{
				o: managed.ExternalObservation{
					ResourceExists:   true,
					ResourceUpToDate: false,
				},
			},
		},
		"Bucket check on external - error": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{
						HeadBucketStub: func(ctx context.Context, hbi *s3.HeadBucketInput, f ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
							return nil, errors.New("some error")
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", &fake, nil, true, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				mg: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: "bucket-check-external-error",
					},
					Spec: v1alpha1.BucketSpec{
						Providers: []string{"s3-backend-1"},
						ResourceSpec: v1.ResourceSpec{
							ProviderConfigReference: &v1.Reference{
								Name: "s3-backend-1",
							},
						},
					},
					Status: v1alpha1.BucketStatus{
						AtProvider: v1alpha1.BucketObservation{
							Backends: v1alpha1.Backends{
								"s3-backend-1": &v1alpha1.BackendInfo{
									BucketCondition: v1.Available(),
								},
							},
						},
						ResourceStatus: v1.ResourceStatus{
							ConditionedStatus: v1.ConditionedStatus{
								Conditions: []v1.Condition{
									v1.Available(),
								},
							},
						},
					},
				},
			},
			want: want{
				o: managed.ExternalObservation{
					ResourceExists:    true,
					ResourceUpToDate:  true,
					ConnectionDetails: managed.ConnectionDetails{},
				},
			},
		},
		"Bucket check on external - not exists": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{
						HeadBucketStub: func(ctx context.Context, hbi *s3.HeadBucketInput, f ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
							return nil, &s3types.NotFound{}
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", &fake, nil, true, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				mg: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: "bucket-check-external-not-exists",
					},
					Spec: v1alpha1.BucketSpec{
						Providers: []string{"s3-backend-1"},
						ResourceSpec: v1.ResourceSpec{
							ProviderConfigReference: &v1.Reference{
								Name: "s3-backend-1",
							},
						},
					},
					Status: v1alpha1.BucketStatus{
						AtProvider: v1alpha1.BucketObservation{
							Backends: v1alpha1.Backends{
								"s3-backend-1": &v1alpha1.BackendInfo{
									BucketCondition: v1.Available(),
								},
							},
						},
						ResourceStatus: v1.ResourceStatus{
							ConditionedStatus: v1.ConditionedStatus{
								Conditions: []v1.Condition{
									v1.Available(),
								},
							},
						},
					},
				},
			},
			want: want{
				o: managed.ExternalObservation{
					ResourceExists:    true,
					ResourceUpToDate:  false,
					ConnectionDetails: managed.ConnectionDetails{},
				},
			},
		},
		"Bucket check on external - ok": {
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
					ObjectMeta: metav1.ObjectMeta{
						Name: "bucket-check-external-ok",
					},
					Spec: v1alpha1.BucketSpec{
						Providers: []string{"s3-backend-1"},
						ResourceSpec: v1.ResourceSpec{
							ProviderConfigReference: &v1.Reference{
								Name: "s3-backend-1",
							},
						},
					},
					Status: v1alpha1.BucketStatus{
						AtProvider: v1alpha1.BucketObservation{
							Backends: v1alpha1.Backends{
								"s3-backend-1": &v1alpha1.BackendInfo{
									BucketCondition: v1.Available(),
								},
							},
						},
						ResourceStatus: v1.ResourceStatus{
							ConditionedStatus: v1.ConditionedStatus{
								Conditions: []v1.Condition{
									v1.Available(),
								},
							},
						},
					},
				},
			},
			want: want{
				o: managed.ExternalObservation{
					ResourceExists:    true,
					ResourceUpToDate:  true,
					ConnectionDetails: managed.ConnectionDetails{},
				},
			},
		},
		"Bucket check on external - Auto pause bucket is not paused": {
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
			},
			args: args{
				mg: &v1alpha1.Bucket{
					Spec: v1alpha1.BucketSpec{
						Providers: []string{"s3-backend-1"},
						ResourceSpec: v1.ResourceSpec{
							ProviderConfigReference: &v1.Reference{
								Name: "s3-backend-1",
							},
						},
					},
					Status: v1alpha1.BucketStatus{
						AtProvider: v1alpha1.BucketObservation{
							Backends: v1alpha1.Backends{
								"s3-backend-1": &v1alpha1.BackendInfo{
									BucketCondition: v1.Available(),
								},
							},
						},
						ResourceStatus: v1.ResourceStatus{
							ConditionedStatus: v1.ConditionedStatus{
								Conditions: []v1.Condition{
									v1.Available(),
								},
							},
						},
					},
				},
			},
			want: want{
				o: managed.ExternalObservation{
					ResourceExists:   true,
					ResourceUpToDate: false,
				},
			},
		},
	}
	for name, tc := range cases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			e := external{backendStore: tc.fields.backendStore, autoPauseBucket: tc.fields.autoPauseBucket, log: logging.NewNopLogger()}
			got, err := e.Observe(context.Background(), tc.args.mg)
			require.ErrorIs(t, err, tc.want.err, "unexpected error")
			assert.Equal(t, got, tc.want.o, "unexpected result")
		})
	}
}
