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
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/stretchr/testify/assert"

	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	"github.com/linode/provider-ceph/internal/backendstore/backendstorefakes"
	"github.com/linode/provider-ceph/internal/controller/s3clienthandler"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestACLObserveBackend(t *testing.T) {
	grantId := "id=abcd"
	publicReadWriteACL := "public-read-write"
	t.Parallel()

	type fields struct {
		backendStore *backendstore.BackendStore
	}

	type args struct {
		bucket      *v1alpha1.Bucket
		backendName string
	}

	type want struct {
		status ResourceStatus
	}

	cases := map[string]struct {
		reason string
		fields fields
		args   args
		want   want
	}{
		"Attempt to observe acl on unhealthy backend": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", &fake, nil, true, apisv1alpha1.HealthStatusUnhealthy)

					return bs
				}(),
			},
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: "bucket",
					},
				},
				backendName: "s3-backend-1",
			},
			want: want{
				status: Updated,
			},
		},
		"Object ownership is enforced for the bucket": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", &fake, nil, true, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: "bucket",
					},
					Spec: v1alpha1.BucketSpec{
						ForProvider: v1alpha1.BucketParameters{
							ObjectOwnership: aws.String(string(s3types.ObjectOwnershipBucketOwnerEnforced)),
						},
					},
				},
				backendName: "s3-backend-1",
			},
			want: want{
				status: Updated,
			},
		},
		"No acl or policy or grants specified for the bucket": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", &fake, nil, true, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: "bucket",
					},
					Spec: v1alpha1.BucketSpec{
						ForProvider: v1alpha1.BucketParameters{},
					},
				},
				backendName: "s3-backend-1",
			},
			want: want{
				status: Updated,
			},
		},
		"ACL specified for the bucket": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", &fake, nil, true, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: "bucket",
					},
					Spec: v1alpha1.BucketSpec{
						ForProvider: v1alpha1.BucketParameters{
							ACL: &publicReadWriteACL,
						},
					},
				},
				backendName: "s3-backend-1",
			},
			want: want{
				status: NeedsUpdate,
			},
		},
		"Policy specified for the bucket": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", &fake, nil, true, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: "bucket",
					},
					Spec: v1alpha1.BucketSpec{
						ForProvider: v1alpha1.BucketParameters{
							AccessControlPolicy: &v1alpha1.AccessControlPolicy{
								Grants: []v1alpha1.Grant{
									{
										Grantee: &v1alpha1.Grantee{
											Type: v1alpha1.TypeCanonicalUser,
										},
										Permission: v1alpha1.PermissionFullControl,
									},
								},
							},
						},
					},
				},
				backendName: "s3-backend-1",
			},
			want: want{
				status: NeedsUpdate,
			},
		},
		"Grant specified for the bucket": {
			fields: fields{
				backendStore: func() *backendstore.BackendStore {
					fake := backendstorefakes.FakeS3Client{}

					bs := backendstore.NewBackendStore()
					bs.AddOrUpdateBackend("s3-backend-1", &fake, nil, true, apisv1alpha1.HealthStatusHealthy)

					return bs
				}(),
			},
			args: args{
				bucket: &v1alpha1.Bucket{
					ObjectMeta: metav1.ObjectMeta{
						Name: "bucket",
					},
					Spec: v1alpha1.BucketSpec{
						ForProvider: v1alpha1.BucketParameters{
							GrantFullControl: &grantId,
						},
					},
				},
				backendName: "s3-backend-1",
			},
			want: want{
				status: NeedsUpdate,
			},
		},
	}
	for name, tc := range cases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			c := NewACLClient(
				tc.fields.backendStore,
				s3clienthandler.NewHandler(
					s3clienthandler.WithAssumeRoleArn(nil),
					s3clienthandler.WithBackendStore(tc.fields.backendStore)),
				logging.NewNopLogger())

			got := c.observeBackend(tc.args.bucket, tc.args.backendName)
			assert.Equal(t, tc.want.status, got, "unexpected status")
		})
	}
}
