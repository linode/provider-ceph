package rgw

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
)

// GenerateCORSConfigurationInput creates the PutBucketCorsInput for the AWS SDK.
func GenerateCORSConfigurationInput(name string, config *v1alpha1.CORSConfiguration) *awss3.PutBucketCorsInput {
	if config == nil {
		return nil
	}

	return &awss3.PutBucketCorsInput{
		Bucket: aws.String(name),
		CORSConfiguration: &types.CORSConfiguration{
			CORSRules: GenerateCORSRules(config.Rules),
		},
	}
}

// GenerateCORSRules converts the Kubernetes CORSRule types to the AWS SDK CORSRule types.
func GenerateCORSRules(inRules []v1alpha1.CORSRule) []types.CORSRule {
	var outRules []types.CORSRule //nolint:prealloc // prealloc is disabled due to AWS requiring nil instead of 0-length for empty slices.
	for _, inRule := range inRules {
		outRule := types.CORSRule{
			AllowedHeaders: inRule.AllowedHeaders,
			AllowedMethods: inRule.AllowedMethods,
			AllowedOrigins: inRule.AllowedOrigins,
			ExposeHeaders:  inRule.ExposeHeaders,
			ID:             inRule.ID,
		}
		if inRule.MaxAgeSeconds != nil {
			maxAge := *inRule.MaxAgeSeconds
			outRule.MaxAgeSeconds = &maxAge
		}
		outRules = append(outRules, outRule)
	}

	return outRules
}

// CORSConfigurationNotFoundErrCode is the error code sent by Ceph when the CORS
// configuration does not exist.
var CORSConfigurationNotFoundErrCode = "NoSuchCORSConfiguration"

// CORSConfigurationNotFound parses the error and reports whether the CORS
// configuration does not exist.
func CORSConfigurationNotFound(err error) bool {
	var awsErr smithy.APIError

	if !errors.As(err, &awsErr) {
		return false
	}

	return awsErr.ErrorCode() == CORSConfigurationNotFoundErrCode
}
