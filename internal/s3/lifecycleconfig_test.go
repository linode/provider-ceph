package s3

import (
	"testing"

	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go/document"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
)

func TestGenerateLifecycleConfigurationInput(t *testing.T) {
	bucketname := "bucket"
	prefix := "someprefix/"
	days90 := int32(90)
	days365 := int32(365)
	days3650 := int32(3650)
	t.Parallel()

	type args struct {
		name   string
		config *v1alpha1.BucketLifecycleConfiguration
	}

	type want struct {
		result *awss3.PutBucketLifecycleConfigurationInput
	}

	cases := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"Config with one rule": {
			args: args{
				name: bucketname,
				config: &v1alpha1.BucketLifecycleConfiguration{
					Rules: []v1alpha1.LifecycleRule{
						{
							Status: "Enabled",
							Prefix: &prefix,
							Expiration: &v1alpha1.LifecycleExpiration{
								Days: &days365,
							},
						},
					},
				},
			},
			want: want{
				result: &awss3.PutBucketLifecycleConfigurationInput{
					Bucket: &bucketname,
					LifecycleConfiguration: &types.BucketLifecycleConfiguration{
						Rules: []types.LifecycleRule{
							{
								Status: "Enabled",
								Prefix: &prefix,
								Expiration: &types.LifecycleExpiration{
									Days: &days365,
								},
								Filter: &types.LifecycleRuleFilterMemberPrefix{},
							},
						},
					},
				},
			},
		},
		"Config with multiple rules": {
			args: args{
				name: bucketname,
				config: &v1alpha1.BucketLifecycleConfiguration{
					Rules: []v1alpha1.LifecycleRule{
						{
							Status: "Enabled",
							Prefix: &prefix,
							Expiration: &v1alpha1.LifecycleExpiration{
								Days: &days3650,
							},
							Filter: &v1alpha1.LifecycleRuleFilter{
								Prefix: &prefix,
							},
							Transitions: []v1alpha1.Transition{
								{
									Days:         &days365,
									StorageClass: "STANDARD_IA",
								},
							},
						},
						{
							Status: "Enabled",
							Prefix: &prefix,

							Expiration: &v1alpha1.LifecycleExpiration{
								Days: &days3650,
							},
							Transitions: []v1alpha1.Transition{
								{
									Days:         &days365,
									StorageClass: "GLACIER",
								},
							},
						},
						{
							Status: "Enabled",
							Prefix: &prefix,
							Expiration: &v1alpha1.LifecycleExpiration{
								Days: &days365,
							},
							Filter: &v1alpha1.LifecycleRuleFilter{
								Prefix: &prefix,
							},
							Transitions: []v1alpha1.Transition{
								{
									Days:         &days90,
									StorageClass: "DEEP_ARCHIVE",
								},
							},
						},
					},
				},
			},
			want: want{
				result: &awss3.PutBucketLifecycleConfigurationInput{
					Bucket: &bucketname,
					LifecycleConfiguration: &types.BucketLifecycleConfiguration{
						Rules: []types.LifecycleRule{
							{
								Status: "Enabled",
								Prefix: &prefix,
								Expiration: &types.LifecycleExpiration{
									Days: &days3650,
								},
								Filter: &types.LifecycleRuleFilterMemberPrefix{
									Value: prefix,
								},
								Transitions: []types.Transition{
									{
										Days:         &days365,
										StorageClass: types.TransitionStorageClassStandardIa,
									},
								},
							},
							{
								Status: "Enabled",
								Prefix: &prefix,
								Expiration: &types.LifecycleExpiration{
									Days: &days3650,
								},
								Filter: &types.LifecycleRuleFilterMemberPrefix{},
								Transitions: []types.Transition{
									{
										Days:         &days365,
										StorageClass: types.TransitionStorageClassGlacier,
									},
								},
							},
							{
								Status: "Enabled",
								Prefix: &prefix,
								Expiration: &types.LifecycleExpiration{
									Days: &days365,
								},
								Filter: &types.LifecycleRuleFilterMemberPrefix{
									Value: prefix,
								},
								Transitions: []types.Transition{
									{
										Days:         &days90,
										StorageClass: types.TransitionStorageClassDeepArchive,
									},
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

			got := GenerateLifecycleConfigurationInput(tc.args.name, tc.args.config)
			if diff := cmp.Diff(tc.want.result, got, cmpopts.IgnoreTypes(document.NoSerde{})); diff != "" {
				t.Errorf("\n%s\nGeneratLifecycleConfigurationInput(...): -want, +got:\n%s\n", tc.reason, diff)
			}
		})
	}
}
