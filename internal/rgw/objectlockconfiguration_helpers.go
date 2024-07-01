package rgw

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
)

// GeneratePutObjectLockConfigurationInput creates the PutObjectLockConfiguration for the AWS SDK
func GeneratePutObjectLockConfigurationInput(name string, config *v1alpha1.ObjectLockConfiguration) *awss3.PutObjectLockConfigurationInput {
	return &awss3.PutObjectLockConfigurationInput{
		Bucket:                  aws.String(name),
		ObjectLockConfiguration: GenerateObjectLockConfiguration(config),
	}
}

func GenerateObjectLockConfiguration(inputConfig *v1alpha1.ObjectLockConfiguration) *types.ObjectLockConfiguration {
	if inputConfig == nil {
		return nil
	}

	outputConfig := &types.ObjectLockConfiguration{}
	if inputConfig.ObjectLockEnabled != nil {
		outputConfig.ObjectLockEnabled = types.ObjectLockEnabled(*inputConfig.ObjectLockEnabled)
	}
	if inputConfig.Rule != nil {
		outputConfig.Rule = &types.ObjectLockRule{}
		if inputConfig.Rule.DefaultRetention != nil {
			outputConfig.Rule.DefaultRetention = &types.DefaultRetention{}
			outputConfig.Rule.DefaultRetention.Mode = types.ObjectLockRetentionMode(inputConfig.Rule.DefaultRetention.Mode)
			if inputConfig.Rule.DefaultRetention.Days != nil {
				outputConfig.Rule.DefaultRetention.Days = inputConfig.Rule.DefaultRetention.Days
			}
			if inputConfig.Rule.DefaultRetention.Years != nil {
				outputConfig.Rule.DefaultRetention.Years = inputConfig.Rule.DefaultRetention.Years
			}
		}
	}

	return outputConfig
}
