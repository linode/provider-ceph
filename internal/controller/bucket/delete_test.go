package bucket

import (
	"context"
	"testing"

	v1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/google/go-cmp/cmp"
	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	"github.com/pkg/errors"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestDelete(t *testing.T) {
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

			kubeClient := fake.NewClientBuilder().Build()

			e := external{backendStore: tc.fields.backendStore, log: logging.NewNopLogger(), kubeClient: kubeClient}
			err := e.Delete(context.Background(), tc.args.mg)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\ne.Delete(...): -want error, +got error:\n%s\n", tc.reason, diff)
			}
		})
	}
}
