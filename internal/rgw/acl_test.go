package rgw

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go/document"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
)

func TestGenerateGrants(t *testing.T) {
	displayName := "Some User"
	email := "someuser@example.com"
	id := "some-user"
	uri := "www.example.com"

	t.Parallel()

	type args struct {
		grantsIn []v1alpha1.Grant
	}

	type want struct {
		result []types.Grant
	}

	cases := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"Single grant fully populated": {
			args: args{
				grantsIn: []v1alpha1.Grant{
					{
						Grantee: &v1alpha1.Grantee{
							Type:         v1alpha1.TypeCanonicalUser,
							DisplayName:  &displayName,
							EmailAddress: &email,
							ID:           &id,
							URI:          &uri,
						},
						Permission: v1alpha1.PermissionFullControl,
					},
				},
			},
			want: want{
				result: []types.Grant{
					{
						Grantee: &types.Grantee{
							Type:         types.TypeCanonicalUser,
							DisplayName:  &displayName,
							EmailAddress: &email,
							ID:           &id,
							URI:          &uri,
						},
						Permission: types.PermissionFullControl,
					},
				},
			},
		},
		"Multiple grants partially populated": {
			args: args{
				grantsIn: []v1alpha1.Grant{
					{
						Grantee: &v1alpha1.Grantee{
							Type:         v1alpha1.TypeEmail,
							EmailAddress: &email,
						},
						Permission: v1alpha1.PermissionRead,
					},
					{
						Grantee: &v1alpha1.Grantee{
							Type: v1alpha1.TypeGroup,
							URI:  &uri,
						},
						Permission: v1alpha1.PermissionWrite,
					},
					{
						Grantee: &v1alpha1.Grantee{
							Type:        v1alpha1.TypeGroup,
							DisplayName: &displayName,
							ID:          &id,
						},
						Permission: v1alpha1.PermissionReadAcp,
					},
					{
						Grantee: &v1alpha1.Grantee{
							Type:         v1alpha1.TypeEmail,
							DisplayName:  &displayName,
							URI:          &uri,
							EmailAddress: &email,
						},
						Permission: v1alpha1.PermissionWriteAcp,
					},
				},
			},
			want: want{
				result: []types.Grant{
					{
						Grantee: &types.Grantee{
							Type:         types.Type(v1alpha1.TypeEmail),
							EmailAddress: &email,
						},
						Permission: types.PermissionRead,
					},
					{
						Grantee: &types.Grantee{
							Type: types.TypeGroup,
							URI:  &uri,
						},
						Permission: types.PermissionWrite,
					},
					{
						Grantee: &types.Grantee{
							Type:        types.TypeGroup,
							DisplayName: &displayName,
							ID:          &id,
						},
						Permission: types.PermissionReadAcp,
					},
					{
						Grantee: &types.Grantee{
							Type:         types.Type(v1alpha1.TypeEmail),
							DisplayName:  &displayName,
							URI:          &uri,
							EmailAddress: &email,
						},
						Permission: types.PermissionWriteAcp,
					},
				},
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := GenerateGrants(tc.args.grantsIn)
			if diff := cmp.Diff(tc.want.result, got, cmpopts.IgnoreTypes(document.NoSerde{})); diff != "" {
				t.Errorf("\n%s\nGenerateGrants(...): -want, +got:\n%s\n", tc.reason, diff)
			}
		})
	}
}

func TestGenerateOwner(t *testing.T) {
	displayName := "Some User"
	id := "some-user"

	t.Parallel()

	type args struct {
		ownerIn *v1alpha1.Owner
	}

	type want struct {
		result *types.Owner
	}

	cases := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"Owner fully populated": {
			args: args{
				ownerIn: &v1alpha1.Owner{
					DisplayName: &displayName,
					ID:          &id,
				},
			},
			want: want{
				result: &types.Owner{
					DisplayName: &displayName,
					ID:          &id,
				},
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := GenerateOwner(tc.args.ownerIn)
			if diff := cmp.Diff(tc.want.result, got, cmpopts.IgnoreTypes(document.NoSerde{})); diff != "" {
				t.Errorf("\n%s\nGenerateOwner(...): -want, +got:\n%s\n", tc.reason, diff)
			}
		})
	}
}
