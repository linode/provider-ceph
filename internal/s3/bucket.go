package s3

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	"github.com/pkg/errors"
)

const (
	errListObjects  = "cannot list objects"
	errDeleteObject = "cannot delete object"
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

func DeleteBucket(ctx context.Context, s3Backend *s3.Client, bucketName *string) error {
	bucketExists, err := BucketExists(ctx, s3Backend, *bucketName)
	if err != nil {
		return err
	}
	if !bucketExists {
		return nil
	}

	// Delete all objects from the bucket. This is sufficient for unversioned buckets.
	objectsInput := &s3.ListObjectsV2Input{Bucket: bucketName}
	for {
		objects, err := s3Backend.ListObjectsV2(ctx, objectsInput)
		if err != nil {
			return errors.Wrap(err, errListObjects)
		}

		for _, object := range objects.Contents {
			if err := DeleteObject(ctx, s3Backend, bucketName, object.Key, nil); err != nil {
				return errors.Wrap(err, errDeleteObject)
			}
		}

		// If the bucket contains many objects, the ListObjectsV2() call
		// might not return all of the objects in the first listing. Check to
		// see whether the listing was truncated. If so, retrieve the next page
		// of objects and delete them.
		if objects.IsTruncated {
			objectsInput.ContinuationToken = objects.ContinuationToken
		} else {
			break
		}
	}

	// Delete all object versions (required for versioned buckets).
	objVersionsInput := &s3.ListObjectVersionsInput{Bucket: bucketName}
	for {
		objectVersions, err := s3Backend.ListObjectVersions(ctx, objVersionsInput)
		if err != nil {
			return errors.Wrap(err, errListObjects)
		}

		for _, objectVersion := range objectVersions.DeleteMarkers {
			if err := DeleteObject(ctx, s3Backend, bucketName, objectVersion.Key, objectVersion.VersionId); err != nil {
				return errors.Wrap(err, errDeleteObject)
			}
		}

		for _, objectVersion := range objectVersions.Versions {
			if err := DeleteObject(ctx, s3Backend, bucketName, objectVersion.Key, objectVersion.VersionId); err != nil {
				return errors.Wrap(err, errDeleteObject)
			}
		}
		if objectVersions.IsTruncated {
			objVersionsInput.VersionIdMarker = objectVersions.NextVersionIdMarker
			objVersionsInput.KeyMarker = objectVersions.NextKeyMarker
		} else {
			break
		}

	}

	_, err = s3Backend.DeleteBucket(ctx, &s3.DeleteBucketInput{Bucket: bucketName})

	return resource.Ignore(isNotFound, err)
}

func DeleteObject(ctx context.Context, s3Backend *s3.Client, bucket, key, versionId *string) error {
	_, err := s3Backend.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket:    bucket,
		Key:       key,
		VersionId: versionId,
	})

	return resource.Ignore(isNotFound, err)
}

func BucketExists(ctx context.Context, s3Backend *s3.Client, bucketName string) (bool, error) {
	_, err := s3Backend.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(bucketName)})
	if err != nil {
		if isNotFound(err) {
			return false, nil
		}
		// Some other error occurred, return false with error
		// as we cannot verify the bucket exists.
		return false, err
	}
	// Bucket exists, return true with no error.
	return true, nil
}

// isNotFound helper function to test for NotFound error
func isNotFound(err error) bool {
	var notFoundError *s3types.NotFound

	return errors.As(err, &notFoundError)
}
