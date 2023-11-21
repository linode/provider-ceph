package s3

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/aws-sdk-go-v2/service/sts/types"
	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
)

func AssumeRoleInput(ar *v1alpha1.AssumeRole) *sts.AssumeRoleInput {
	return &sts.AssumeRoleInput{
		RoleArn:         ar.RoleArn,
		Policy:          ar.Policy,
		RoleSessionName: ar.RoleSessionName,
		Tags:            copySTSTags(ar.Tags),
	}
}

// copySTSTags converts a list of local v1beta.Tags to S3 Tags
func copySTSTags(tags []v1alpha1.Tag) []types.Tag {
	out := make([]types.Tag, 0)
	for _, one := range tags {
		out = append(out, types.Tag{Key: aws.String(one.Key), Value: aws.String(one.Value)})
	}

	return out
}
