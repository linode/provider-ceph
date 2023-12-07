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

package healthcheck

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	"github.com/linode/provider-ceph/internal/backendstore/backendstorefakes"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

//nolint:maintidx // Function requires numerous checks.
func TestReconcile(t *testing.T) {
	t.Parallel()
	backendName := "test-backend"
	putObjErr := errors.New("failed to put object")
	getObjErr := errors.New("failed to get object")

	type fields struct {
		fakeS3Client   func(*backendstorefakes.FakeS3Client)
		providerConfig *apisv1alpha1.ProviderConfig
		bucketList     *v1alpha1.BucketList
		autopause      bool
	}

	type args struct {
		req ctrl.Request
	}

	type want struct {
		res        ctrl.Result
		err        error
		pc         *apisv1alpha1.ProviderConfig
		bucketList *v1alpha1.BucketList
	}

	cases := map[string]struct {
		reason string
		fields fields
		args   args
		want   want
	}{
		"ProviderConfig has been deleted": {
			fields: fields{
				fakeS3Client: func(fake *backendstorefakes.FakeS3Client) {
					// cleanup calls HeadBucket
					var notFoundError *s3types.NotFound
					fake.HeadBucketReturns(
						&s3.HeadBucketOutput{},
						notFoundError,
					)
				},
				providerConfig: &apisv1alpha1.ProviderConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name: "some-other-pc",
					},
				},
			},
			args: args{
				req: ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name: backendName,
					},
				},
			},
			want: want{
				res: ctrl.Result{},
				err: nil,
				pc:  nil,
			},
		},
		"ProviderConfig health check disabled": {
			fields: fields{
				providerConfig: &apisv1alpha1.ProviderConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name: backendName,
					},
					Spec: apisv1alpha1.ProviderConfigSpec{
						DisableHealthCheck: true,
					},
				},
			},
			args: args{
				req: ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name: backendName,
					},
				},
			},
			want: want{
				res: ctrl.Result{},
				err: nil,
				pc: &apisv1alpha1.ProviderConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name: backendName,
					},
					Spec: apisv1alpha1.ProviderConfigSpec{
						DisableHealthCheck: true,
					},
					Status: apisv1alpha1.ProviderConfigStatus{
						ProviderConfigStatus: xpv1.ProviderConfigStatus{
							ConditionedStatus: xpv1.ConditionedStatus{
								Conditions: []xpv1.Condition{
									v1alpha1.HealthCheckDisabled(),
								},
							},
						},
					},
				},
			},
		},
		"ProviderConfig goes from healthy to unhealthy due to failed put object": {
			fields: fields{
				fakeS3Client: func(fake *backendstorefakes.FakeS3Client) {
					// fail the health check with a PutObject error
					fake.PutObjectReturns(
						&s3.PutObjectOutput{},
						putObjErr,
					)
				},
				providerConfig: &apisv1alpha1.ProviderConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name: backendName,
					},
					Spec: apisv1alpha1.ProviderConfigSpec{
						DisableHealthCheck: false,
					},
					Status: apisv1alpha1.ProviderConfigStatus{
						ProviderConfigStatus: xpv1.ProviderConfigStatus{
							ConditionedStatus: xpv1.ConditionedStatus{
								Conditions: []xpv1.Condition{
									v1alpha1.HealthCheckSuccess(),
								},
							},
						},
					},
				},
			},
			args: args{
				req: ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name: backendName,
					},
				},
			},
			want: want{
				res: ctrl.Result{},
				err: putObjErr,
				pc: &apisv1alpha1.ProviderConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name: backendName,
					},
					Spec: apisv1alpha1.ProviderConfigSpec{
						DisableHealthCheck: false,
					},
					Status: apisv1alpha1.ProviderConfigStatus{
						ProviderConfigStatus: xpv1.ProviderConfigStatus{
							ConditionedStatus: xpv1.ConditionedStatus{
								Conditions: []xpv1.Condition{
									v1alpha1.HealthCheckFail().
										WithMessage(errDoHealthCheck + ": " + errors.Wrap(putObjErr, errPutHealthCheckFile).Error()),
								},
							},
						},
					},
				},
			},
		},
		"ProviderConfig goes from healthy to unhealthy due to failed get object": {
			fields: fields{
				fakeS3Client: func(fake *backendstorefakes.FakeS3Client) {
					// fail the health check with a GetObject error
					fake.GetObjectReturns(
						&s3.GetObjectOutput{},
						getObjErr,
					)
				},
				providerConfig: &apisv1alpha1.ProviderConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name: backendName,
					},
					Spec: apisv1alpha1.ProviderConfigSpec{
						DisableHealthCheck: false,
					},
					Status: apisv1alpha1.ProviderConfigStatus{
						ProviderConfigStatus: xpv1.ProviderConfigStatus{
							ConditionedStatus: xpv1.ConditionedStatus{
								Conditions: []xpv1.Condition{
									v1alpha1.HealthCheckSuccess(),
								},
							},
						},
					},
				},
			},
			args: args{
				req: ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name: backendName,
					},
				},
			},
			want: want{
				res: ctrl.Result{},
				err: getObjErr,
				pc: &apisv1alpha1.ProviderConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name: backendName,
					},
					Spec: apisv1alpha1.ProviderConfigSpec{
						DisableHealthCheck: false,
					},
					Status: apisv1alpha1.ProviderConfigStatus{
						ProviderConfigStatus: xpv1.ProviderConfigStatus{
							ConditionedStatus: xpv1.ConditionedStatus{
								Conditions: []xpv1.Condition{
									v1alpha1.HealthCheckFail().
										WithMessage(errDoHealthCheck + ": " + errors.Wrap(getObjErr, errGetHealthCheckFile).Error()),
								},
							},
						},
					},
				},
			},
		},
		"ProviderConfig goes from unhealthy to healthy so its buckets should be unpaused": {
			fields: fields{
				providerConfig: &apisv1alpha1.ProviderConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name: backendName,
					},
					Spec: apisv1alpha1.ProviderConfigSpec{
						DisableHealthCheck: false,
					},
					Status: apisv1alpha1.ProviderConfigStatus{
						ProviderConfigStatus: xpv1.ProviderConfigStatus{
							ConditionedStatus: xpv1.ConditionedStatus{
								Conditions: []xpv1.Condition{
									v1alpha1.HealthCheckFail(),
								},
							},
						},
					},
				},
				autopause: true,
				bucketList: &v1alpha1.BucketList{
					Items: []v1alpha1.Bucket{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "bucket-1",
								Labels: map[string]string{
									v1alpha1.BackendLabelPrefix + backendName: "true",
									meta.AnnotationKeyReconciliationPaused:    "true",
								},
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "bucket-2",
								Labels: map[string]string{
									v1alpha1.BackendLabelPrefix + backendName: "true",
									meta.AnnotationKeyReconciliationPaused:    "true",
								},
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "bucket-3",
								Labels: map[string]string{
									v1alpha1.BackendLabelPrefix + backendName: "true",
									meta.AnnotationKeyReconciliationPaused:    "",
								},
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "bucket-4",
								Labels: map[string]string{
									meta.AnnotationKeyReconciliationPaused: "true",
								},
							},
						},
					},
				},
			},
			args: args{
				req: ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name: backendName,
					},
				},
			},
			want: want{
				res: ctrl.Result{},
				err: nil,
				pc: &apisv1alpha1.ProviderConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name: backendName,
					},
					Spec: apisv1alpha1.ProviderConfigSpec{
						DisableHealthCheck: false,
					},
					Status: apisv1alpha1.ProviderConfigStatus{
						ProviderConfigStatus: xpv1.ProviderConfigStatus{
							ConditionedStatus: xpv1.ConditionedStatus{
								Conditions: []xpv1.Condition{
									v1alpha1.HealthCheckSuccess(),
								},
							},
						},
					},
				},
				bucketList: &v1alpha1.BucketList{
					Items: []v1alpha1.Bucket{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "bucket-1",
								Labels: map[string]string{
									v1alpha1.BackendLabelPrefix + backendName: "true",
									meta.AnnotationKeyReconciliationPaused:    "",
								},
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "bucket-2",
								Labels: map[string]string{
									v1alpha1.BackendLabelPrefix + backendName: "true",
									meta.AnnotationKeyReconciliationPaused:    "",
								},
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "bucket-3",
								Labels: map[string]string{
									v1alpha1.BackendLabelPrefix + backendName: "true",
									meta.AnnotationKeyReconciliationPaused:    "",
								},
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "bucket-4",
								Labels: map[string]string{
									meta.AnnotationKeyReconciliationPaused: "true",
								},
							},
						},
					},
				},
			},
		},
		"ProviderConfig goes from health check disabled to healthy so its buckets should be unpaused": {
			fields: fields{
				providerConfig: &apisv1alpha1.ProviderConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name: backendName,
					},
					Spec: apisv1alpha1.ProviderConfigSpec{
						DisableHealthCheck: false,
					},
					Status: apisv1alpha1.ProviderConfigStatus{
						ProviderConfigStatus: xpv1.ProviderConfigStatus{
							ConditionedStatus: xpv1.ConditionedStatus{
								Conditions: []xpv1.Condition{
									v1alpha1.HealthCheckDisabled(),
								},
							},
						},
					},
				},
				autopause: true,
				bucketList: &v1alpha1.BucketList{
					Items: []v1alpha1.Bucket{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "bucket-1",
								Labels: map[string]string{
									v1alpha1.BackendLabelPrefix + backendName: "true",
									meta.AnnotationKeyReconciliationPaused:    "true",
								},
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "bucket-2",
								Labels: map[string]string{
									v1alpha1.BackendLabelPrefix + backendName: "true",
									meta.AnnotationKeyReconciliationPaused:    "true",
								},
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "bucket-3",
								Labels: map[string]string{
									v1alpha1.BackendLabelPrefix + backendName: "true",
									meta.AnnotationKeyReconciliationPaused:    "",
								},
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "bucket-4",
								Labels: map[string]string{
									meta.AnnotationKeyReconciliationPaused: "true",
								},
							},
						},
					},
				},
			},
			args: args{
				req: ctrl.Request{
					NamespacedName: types.NamespacedName{
						Name: backendName,
					},
				},
			},
			want: want{
				res: ctrl.Result{},
				err: nil,
				pc: &apisv1alpha1.ProviderConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name: backendName,
					},
					Spec: apisv1alpha1.ProviderConfigSpec{
						DisableHealthCheck: false,
					},
					Status: apisv1alpha1.ProviderConfigStatus{
						ProviderConfigStatus: xpv1.ProviderConfigStatus{
							ConditionedStatus: xpv1.ConditionedStatus{
								Conditions: []xpv1.Condition{
									v1alpha1.HealthCheckSuccess(),
								},
							},
						},
					},
				},
				bucketList: &v1alpha1.BucketList{
					Items: []v1alpha1.Bucket{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "bucket-1",
								Labels: map[string]string{
									v1alpha1.BackendLabelPrefix + backendName: "true",
									meta.AnnotationKeyReconciliationPaused:    "",
								},
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "bucket-2",
								Labels: map[string]string{
									v1alpha1.BackendLabelPrefix + backendName: "true",
									meta.AnnotationKeyReconciliationPaused:    "",
								},
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "bucket-3",
								Labels: map[string]string{
									v1alpha1.BackendLabelPrefix + backendName: "true",
									meta.AnnotationKeyReconciliationPaused:    "",
								},
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "bucket-4",
								Labels: map[string]string{
									meta.AnnotationKeyReconciliationPaused: "true",
								},
							},
						},
					},
				},
			},
		},
	}

	for name, tc := range cases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			scheme := runtime.NewScheme()
			scheme.AddKnownTypes(apisv1alpha1.SchemeGroupVersion,
				&apisv1alpha1.ProviderConfig{},
				&apisv1alpha1.ProviderConfigList{})
			scheme.AddKnownTypes(v1alpha1.SchemeGroupVersion,
				&v1alpha1.Bucket{},
				&v1alpha1.BucketList{})

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tc.fields.providerConfig).
				WithStatusSubresource(tc.fields.providerConfig)

			if tc.fields.bucketList != nil {
				fakeClient.WithLists(tc.fields.bucketList)
			}
			c := fakeClient.Build()

			fakeS3Client := backendstorefakes.FakeS3Client{}
			if tc.fields.fakeS3Client != nil {
				tc.fields.fakeS3Client(&fakeS3Client)
			}
			bs := backendstore.NewBackendStore()
			bs.AddOrUpdateBackend(backendName, &fakeS3Client, tc.fields.autopause, apisv1alpha1.HealthStatusHealthy)

			r := NewController(
				WithAutoPause(&tc.fields.autopause),
				WithBackendStore(bs),
				WithKubeClient(c),
				WithLogger(logging.NewNopLogger()))

			got, err := r.Reconcile(context.Background(), tc.args.req)
			assert.Equal(t, tc.want.res, got, "unexpected result")
			assert.ErrorIs(t, err, tc.want.err, "unexpected error")

			// Now check that the reconciled ProviderConfig was updated correctly.
			if tc.want.pc == nil {
				return
			}
			pc := &apisv1alpha1.ProviderConfig{}
			err = c.Get(context.TODO(), types.NamespacedName{Name: backendName}, pc)
			if err != nil {
				t.Fatalf("failed to get ProviderConfig after reconcile: %s", err.Error())
			}
			assert.True(t, tc.want.pc.Status.ConditionedStatus.Equal(&pc.Status.ConditionedStatus), "unexpected condition")

			// Now check that the correct buckets have been unpaused.
			if tc.want.bucketList == nil {
				return
			}
			// We need to wait for the unpause go routine to complete.
			time.Sleep(time.Millisecond * 500)
			bl := &v1alpha1.BucketList{}
			err = c.List(context.TODO(), bl)
			if err != nil {
				t.Fatalf("failed to list Buckets after reconcile: %s", err.Error())
			}
			for i := range bl.Items {
				assert.Equal(t, tc.want.bucketList.Items[i].Labels, bl.Items[i].Labels, "unexpected result")
			}
		})
	}
}
