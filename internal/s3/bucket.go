package s3

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/crossplane/provider-ceph/apis/provider-ceph/v1alpha1"
)

func BucketToCreateBucketInput(bucket *v1alpha1.Bucket) *s3.CreateBucketInput {
	createBucketInput := &s3.CreateBucketInput{}
	createBucketInput.Bucket = aws.String(bucket.Name)

	createBucketInput.GrantFullControl = bucket.Spec.ForProvider.GrantFullControl
	createBucketInput.GrantRead = bucket.Spec.ForProvider.GrantRead
	createBucketInput.GrantReadACP = bucket.Spec.ForProvider.GrantReadACP
	createBucketInput.GrantWrite = bucket.Spec.ForProvider.GrantWrite
	createBucketInput.GrantWriteACP = bucket.Spec.ForProvider.GrantWriteACP
	createBucketInput.ObjectLockEnabledForBucket = bucket.Spec.ForProvider.ObjectLockEnabledForBucket
	createBucketInput.ObjectOwnership = bucket.Spec.ForProvider.ObjectOwnership

	createBucketCfg := &s3.CreateBucketConfiguration{}
	createBucketCfg.LocationConstraint = bucket.Spec.ForProvider.LocationConstraint
	createBucketInput.CreateBucketConfiguration = createBucketCfg

	return createBucketInput
}
