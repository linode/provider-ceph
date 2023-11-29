package bucket

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/meta"
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
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestUpdate(t *testing.T) {
	t.Parallel()

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
		specificDiff func(mg resource.Managed) string
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
				err: errors.New(errNoS3BackendsRegistered),
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
					bs.AddOrUpdateBackend("s3-backend-1", &fake, true, apisv1alpha1.HealthStatusHealthy)

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
		"OK - Two backends are ready": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{
						HeadBucketStub: func(ctx context.Context, hbi *s3.HeadBucketInput, f ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
							return &s3.HeadBucketOutput{}, nil
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
				o: managed.ExternalUpdate{},
				specificDiff: func(mg resource.Managed) string {
					bucket, _ := mg.(*v1alpha1.Bucket)

					return cmp.Diff(
						v1alpha1.Backends{
							"s3-backend-1": &v1alpha1.BackendInfo{
								BucketStatus: v1alpha1.ReadyStatus,
							},
							"s3-backend-2": &v1alpha1.BackendInfo{
								BucketStatus: v1alpha1.ReadyStatus,
							},
						},
						bucket.Status.AtProvider.Backends,
					)
				},
			},
		},
		"OK - Auto pause bucket is paused": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{
						HeadBucketStub: func(ctx context.Context, hbi *s3.HeadBucketInput, f ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
							return &s3.HeadBucketOutput{}, nil
						},
					}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", &fake, true, apisv1alpha1.HealthStatusHealthy)

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
				specificDiff: func(mg resource.Managed) string {
					bucket, _ := mg.(*v1alpha1.Bucket)

					return cmp.Diff(
						map[string]string{
							meta.AnnotationKeyReconciliationPaused: "true",
							"provider-ceph.backends.s3-backend-1":  "",
						},
						bucket.Labels,
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

			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\ne.Update(...): -want error, +got error:\n%s\n", tc.reason, diff)
			}

			if diff := cmp.Diff(tc.want.o, got); diff != "" {
				t.Errorf("\n%s\ne.Update(...): -want, +got:\n%s\n", tc.reason, diff)
			}

			if tc.want.specificDiff != nil {
				if diff := tc.want.specificDiff(tc.args.mg); diff != "" {
					t.Errorf("\n%s\ne.Update(...): -want, +got:\n%s\n", tc.reason, diff)
				}
			}
		})
	}
}
