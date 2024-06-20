package rgw

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
)

// GeneratePutBucketVersioningInput creates the PutBucketVersioningInput for the AWS SDK
func GeneratePutBucketVersioningInput(name string, config *v1alpha1.VersioningConfiguration) *awss3.PutBucketVersioningInput {
	return &awss3.PutBucketVersioningInput{
		Bucket:                  aws.String(name),
		VersioningConfiguration: GenerateVersioningConfiguration(config),
	}
}

func GenerateVersioningConfiguration(inputConfig *v1alpha1.VersioningConfiguration) *types.VersioningConfiguration {
	if inputConfig == nil {
		return nil
	}

	outputConfig := &types.VersioningConfiguration{}
	if inputConfig.MFADelete != nil {
		outputConfig.MFADelete = types.MFADelete(*inputConfig.MFADelete)
	}
	if inputConfig.Status != nil {
		outputConfig.Status = types.BucketVersioningStatus(*inputConfig.Status)
	}

	return outputConfig
}
