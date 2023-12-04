package bucket

import (
	"context"
	"testing"

	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/google/go-cmp/cmp"
	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	"github.com/pkg/errors"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestCreate(t *testing.T) {
	t.Parallel()

	type fields struct {
		backendStore *backendstore.BackendStore
	}

	type args struct {
		mg resource.Managed
	}

	type want struct {
		o   managed.ExternalCreation
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
	}

	pc := &apisv1alpha1.ProviderConfig{}
	s := scheme.Scheme
	s.AddKnownTypes(apisv1alpha1.SchemeGroupVersion, pc)

	for name, tc := range cases {
		tc := tc

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cl := fake.NewClientBuilder().WithScheme(s).Build()
			e := external{
				kubeClient:   cl,
				backendStore: tc.fields.backendStore,
				log:          logging.NewNopLogger(),
			}

			got, err := e.Create(context.Background(), tc.args.mg)

			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\ne.Create(...): -want error, +got error:\n%s\n", tc.reason, diff)
			}
			if diff := cmp.Diff(tc.want.o, got); diff != "" {
				t.Errorf("\n%s\ne.Create(...): -want, +got:\n%s\n", tc.reason, diff)
			}
		})
	}
}
