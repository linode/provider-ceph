/*
Copyright 2022 The Crossplane Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package bucket

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	v1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
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

// Unlike many Kubernetes projects Crossplane does not use third party testing
// libraries, per the common Go test review comments. Crossplane encourages the
// use of table driven unit tests. The tests of the crossplane-runtime project
// are representative of the testing style Crossplane encourages.
//
// https://github.com/golang/go/wiki/TestComments
// https://github.com/crossplane/crossplane/blob/master/CONTRIBUTING.md#contributing-code

var (
	unexpectedItem resource.Managed
)

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
		"Bucket doesn't have any living backend": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", nil, true, apisv1alpha1.HealthStatusHealthy)

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
				},
			},
			want: want{
				o: managed.ExternalObservation{
					ResourceExists:   false,
					ResourceUpToDate: true,
				},
			},
		},
		"Bucket doesn't have finalizer": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", nil, true, apisv1alpha1.HealthStatusHealthy)

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
				},
			},
			want: want{
				o: managed.ExternalObservation{
					ResourceExists:   false,
					ResourceUpToDate: true,
				},
			},
		},
		"Bucket status is not available": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", nil, true, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				mg: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Finalizers: []string{inUseFinalizer},
					},
					Spec: v1alpha1.BucketSpec{
						ResourceSpec: v1.ResourceSpec{
							ProviderConfigReference: &v1.Reference{
								Name: "s3-backend-1",
							},
						},
					},
					Status: v1alpha1.BucketStatus{
						AtProvider: v1alpha1.BucketObservation{
							BackendStatuses: v1alpha1.BackendStatuses{
								"s3-backend-1": v1alpha1.BackendReadyStatus,
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
					bs.AddOrUpdateBackend("s3-backend-1", nil, true, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				mg: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Finalizers: []string{inUseFinalizer},
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
							BackendStatuses: v1alpha1.BackendStatuses{
								"s3-backend-1": v1alpha1.BackendNotReadyStatus,
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
					bs.AddOrUpdateBackend("s3-backend-1", &fake, true, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				mg: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "bucket-check-external-error",
						Finalizers: []string{inUseFinalizer},
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
							BackendStatuses: v1alpha1.BackendStatuses{
								"s3-backend-1": v1alpha1.BackendReadyStatus,
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
					bs.AddOrUpdateBackend("s3-backend-1", &fake, true, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				mg: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "bucket-check-external-not-exists",
						Finalizers: []string{inUseFinalizer},
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
							BackendStatuses: v1alpha1.BackendStatuses{
								"s3-backend-1": v1alpha1.BackendReadyStatus,
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
					bs.AddOrUpdateBackend("s3-backend-1", &fake, true, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				mg: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "bucket-check-external-ok",
						Finalizers: []string{inUseFinalizer},
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
							BackendStatuses: v1alpha1.BackendStatuses{
								"s3-backend-1": v1alpha1.BackendReadyStatus,
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
					bs.AddOrUpdateBackend("s3-backend-1", &fake, true, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
				autoPauseBucket: true,
			},
			args: args{
				mg: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Finalizers: []string{inUseFinalizer},
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
							BackendStatuses: v1alpha1.BackendStatuses{
								"s3-backend-1": v1alpha1.BackendReadyStatus,
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
		"Bucket check on external - OK, Health check bucket ignores auto pause": {
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
			},
			args: args{
				mg: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Finalizers: []string{inUseFinalizer},
						Labels: map[string]string{
							v1alpha1.HealthCheckLabelKey: v1alpha1.HealthCheckLabelVal,
						},
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
							BackendStatuses: v1alpha1.BackendStatuses{
								"s3-backend-1": v1alpha1.BackendReadyStatus,
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
	}
	for name, tc := range cases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			e := external{backendStore: tc.fields.backendStore, autoPauseBucket: tc.fields.autoPauseBucket, log: logging.NewNopLogger()}
			got, err := e.Observe(context.Background(), tc.args.mg)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\ne.Observe(...): -want error, +got error:\n%s\n", tc.reason, diff)
			}
			if diff := cmp.Diff(tc.want.o, got); diff != "" {
				t.Errorf("\n%s\ne.Observe(...): -want, +got:\n%s\n", tc.reason, diff)
			}
		})
	}
}

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
		"OK - Health check bucket": {
			fields: fields{
				backendStore: backendstore.NewBackendStore(),
			},
			args: args{
				mg: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							v1alpha1.HealthCheckLabelKey: v1alpha1.HealthCheckLabelVal,
						},
					},
				},
			},
			want: want{
				o: managed.ExternalUpdate{},
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
						v1alpha1.BackendStatuses{
							"s3-backend-1": v1alpha1.BackendReadyStatus,
							"s3-backend-2": v1alpha1.BackendReadyStatus,
						},
						bucket.Status.AtProvider.BackendStatuses,
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
					t.Log(bucket)
					return cmp.Diff(
						map[string]string{
							"test":                                 "test",
							meta.AnnotationKeyReconciliationPaused: "true",
						},
						bucket.Annotations,
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
