package s3

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
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

func BucketToPutBucketACLInput(bucket *v1alpha1.Bucket) *s3.PutBucketAclInput {
	return &s3.PutBucketAclInput{
		ACL:              s3types.BucketCannedACL(aws.ToString(bucket.Spec.ForProvider.ACL)),
		Bucket:           aws.String(bucket.Name),
		GrantFullControl: bucket.Spec.ForProvider.GrantFullControl,
		GrantRead:        bucket.Spec.ForProvider.GrantRead,
		GrantReadACP:     bucket.Spec.ForProvider.GrantReadACP,
		GrantWrite:       bucket.Spec.ForProvider.GrantWrite,
		GrantWriteACP:    bucket.Spec.ForProvider.GrantWriteACP,
	}
}

func BucketToPutBucketOwnershipControlsInput(bucket *v1alpha1.Bucket) *s3.PutBucketOwnershipControlsInput {
	return &s3.PutBucketOwnershipControlsInput{
		Bucket: aws.String(bucket.Name),
		OwnershipControls: &s3types.OwnershipControls{
			Rules: []s3types.OwnershipControlsRule{
				{
					ObjectOwnership: s3types.ObjectOwnership(aws.ToString(bucket.Spec.ForProvider.ObjectOwnership)),
				},
			},
		},
	}
}
