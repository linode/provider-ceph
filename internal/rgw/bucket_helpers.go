package rgw

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/crossplane/crossplane-runtime/pkg/errors"
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
		ObjectLockEnabledForBucket: bucket.Spec.ForProvider.ObjectLockEnabledForBucket,
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

// IsAlreadyExists helper function to test for ErrCodeBucketAlreadyExists error
func IsAlreadyExists(err error) bool {
	var alreadyExists *s3types.BucketAlreadyExists

	return errors.As(err, &alreadyExists)
}

// IsAlreadyOwnedByYou helper function to test for ErrCodeBucketAlreadyOwnedByYou error
func IsAlreadyOwnedByYou(err error) bool {
	var alreadyOwnedByYou *s3types.BucketAlreadyOwnedByYou

	return errors.As(err, &alreadyOwnedByYou)
}

// IsNotFound helper function to test for NotFound error
func IsNotFound(err error) bool {
	var notFoundError *s3types.NotFound

	return errors.As(err, &notFoundError)
}

// NoSuchBucket helper function to test for NoSuchBucket error
func NoSuchBucket(err error) bool {
	var noSuchBucketError *s3types.NoSuchBucket

	return errors.As(err, &noSuchBucketError)
}

func IsNotEmpty(err error) bool {
	var ae smithy.APIError
	if !errors.As(err, &ae) {
		return false
	}

	return ae != nil && ae.ErrorCode() == "BucketNotEmpty"
}

// Unlike NoSuchBucket error or others, aws-sdk-go-v2 doesn't have a specific struct definition for BucketNotEmpty error.
// So we should define ourselves. This is currently only for testing.
type BucketNotEmptyError struct{}

func (e BucketNotEmptyError) Error() string {
	return "BucketNotEmpty: some error"
}

func (e BucketNotEmptyError) ErrorCode() string {
	return "BucketNotEmpty"
}

func (e BucketNotEmptyError) ErrorMessage() string {
	return "some error"
}

func (e BucketNotEmptyError) ErrorFault() smithy.ErrorFault {
	return smithy.FaultUnknown
}
