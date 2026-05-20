package rgw

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
)

// GenerateServerSideEncryptionConfigurationInput creates the PutBucketEncryptionInput for the AWS SDK
func GenerateServerSideEncryptionConfigurationInput(name string, config *v1alpha1.ServerSideEncryptionConfiguration) *awss3.PutBucketEncryptionInput {
	if config == nil {
		return nil
	}

	return &awss3.PutBucketEncryptionInput{
		Bucket: aws.String(name),
		ServerSideEncryptionConfiguration: &types.ServerSideEncryptionConfiguration{
			Rules: GenerateServerSideEncryptionRules(config.Rules),
		},
	}
}

// GenerateServerSideEncryptionRules creates the list of ServerSideEncryptionRules for the AWS SDK
func GenerateServerSideEncryptionRules(inRules []v1alpha1.ServerSideEncryptionRule) []types.ServerSideEncryptionRule {
	outRules := make([]types.ServerSideEncryptionRule, 0, len(inRules))
	for _, inRule := range inRules {
		sseByDefault := types.ServerSideEncryptionByDefault{
			SSEAlgorithm:   types.ServerSideEncryption(inRule.ApplyServerSideEncryptionByDefault.SSEAlgorithm),
			KMSMasterKeyID: inRule.ApplyServerSideEncryptionByDefault.KMSMasterKeyID,
		}
		outRule := types.ServerSideEncryptionRule{
			BucketKeyEnabled:                   inRule.BucketKeyEnabled,
			ApplyServerSideEncryptionByDefault: &sseByDefault,
		}
		outRules = append(outRules, outRule)
	}

	return outRules
}

// ServerSideEncryptionConfigurationNotFoundErrCode is the error code sent by Ceph when the server-side
// encryption configuration does not exist.
var ServerSideEncryptionConfigurationNotFoundErrCode = "ServerSideEncryptionConfigurationNotFoundError"

// ServerSideEncryptionConfigurationNotFound parses the error and reports whether the server-side
// encryption configuration does not exist.
func ServerSideEncryptionConfigurationNotFound(err error) bool {
	var awsErr smithy.APIError

	if !errors.As(err, &awsErr) {
		return false
	}

	return awsErr.ErrorCode() == ServerSideEncryptionConfigurationNotFoundErrCode
}
