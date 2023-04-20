package s3

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/crossplane/provider-ceph/apis/provider-ceph/v1alpha1"
)

func BucketToCreateBucketInput(bucket *v1alpha1.Bucket) *s3.CreateBucketInput {
	createBucketInput := &s3.CreateBucketInput{

		ACL:                        s3types.BucketCannedACL(aws.ToString(bucket.Spec.ForProvider.ACL)),
		Bucket:                     aws.String(bucket.Name),
		GrantFullControl:           bucket.Spec.ForProvider.GrantFullControl,
		GrantRead:                  bucket.Spec.ForProvider.GrantRead,
		GrantReadACP:               bucket.Spec.ForProvider.GrantReadACP,
		GrantWrite:                 bucket.Spec.ForProvider.GrantWrite,
		GrantWriteACP:              bucket.Spec.ForProvider.GrantWriteACP,
		ObjectLockEnabledForBucket: aws.ToBool(bucket.Spec.ForProvider.ObjectLockEnabledForBucket),
		ObjectOwnership:            s3types.ObjectOwnership(aws.ToString(bucket.Spec.ForProvider.ObjectOwnership)),
	}

	if bucket.Spec.ForProvider.LocationConstraint != "" {
		createBucketInput.CreateBucketConfiguration = &s3types.CreateBucketConfiguration{
			LocationConstraint: s3types.BucketLocationConstraint(bucket.Spec.ForProvider.LocationConstraint),
		}
	}

	return createBucketInput
}
