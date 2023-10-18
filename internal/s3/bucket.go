package s3

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	"github.com/linode/provider-ceph/internal/s3/cache"
	"github.com/pkg/errors"
	"go.opentelemetry.io/otel"
	"golang.org/x/sync/errgroup"
)

const (
	errListObjects  = "cannot list objects"
	errDeleteObject = "cannot delete object"

	RequestRetries = 5
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

func DeleteBucket(ctx context.Context, s3Backend backendstore.S3Client, bucketName *string) error {
	bucketExists, err := BucketExists(ctx, s3Backend, *bucketName)
	if err != nil {
		return err
	}
	if !bucketExists {
		return nil
	}

	g := new(errgroup.Group)

	// Delete all objects from the bucket. This is sufficient for unversioned buckets.
	g.Go(func() error {
		return deleteBucketObjects(ctx, s3Backend, bucketName)
	})

	// Delete all object versions (required for versioned buckets).
	g.Go(func() error {
		return deleteBucketObjectVersions(ctx, s3Backend, bucketName)
	})

	if err := g.Wait(); err != nil {
		if NoSuchBucket(err) {
			return nil
		}

		return err
	}

	_, err = s3Backend.DeleteBucket(ctx, &s3.DeleteBucketInput{Bucket: bucketName})

	return resource.Ignore(IsNotFound, err)
}

func deleteBucketObjects(ctx context.Context, s3Backend backendstore.S3Client, bucketName *string) error {
	ctx, span := otel.Tracer("").Start(ctx, "deleteBucketObjects")
	defer span.End()

	objectsInput := &s3.ListObjectsV2Input{Bucket: bucketName}
	for {
		objects, err := s3Backend.ListObjectsV2(ctx, objectsInput)
		if err != nil {
			return errors.Wrap(err, errListObjects)
		}

		g := new(errgroup.Group)
		for _, object := range objects.Contents {
			obj := object
			g.Go(func() error {
				return deleteObject(ctx, s3Backend, bucketName, obj.Key, nil)
			})
		}

		if err := g.Wait(); err != nil {
			return errors.Wrap(err, errDeleteObject)
		}

		// If the bucket contains many objects, the ListObjectsV2() call
		// might not return all of the objects in the first listing. Check to
		// see whether the listing was truncated. If so, retrieve the next page
		// of objects and delete them.
		if !objects.IsTruncated {
			break
		}

		objectsInput.ContinuationToken = objects.ContinuationToken
	}

	return nil
}

func deleteBucketObjectVersions(ctx context.Context, s3Backend backendstore.S3Client, bucketName *string) error {
	ctx, span := otel.Tracer("").Start(ctx, "deleteBucketObjectVersions")
	defer span.End()

	objVersionsInput := &s3.ListObjectVersionsInput{Bucket: bucketName}
	for {
		objectVersions, err := s3Backend.ListObjectVersions(ctx, objVersionsInput)
		if err != nil {
			return errors.Wrap(err, errListObjects)
		}

		g := new(errgroup.Group)
		for _, deleteMarkerEntry := range objectVersions.DeleteMarkers {
			delMark := deleteMarkerEntry
			g.Go(func() error {
				return deleteObject(ctx, s3Backend, bucketName, delMark.Key, delMark.VersionId)
			})
		}

		for _, objectVersion := range objectVersions.Versions {
			objVer := objectVersion
			g.Go(func() error {
				return deleteObject(ctx, s3Backend, bucketName, objVer.Key, objVer.VersionId)
			})
		}

		if err := g.Wait(); err != nil {
			return errors.Wrap(err, errDeleteObject)
		}

		// If the bucket contains many objects, the ListObjectVersionsV2() call
		// might not return all of the objects in the first listing. Check to
		// see whether the listing was truncated. If so, retrieve the next page
		// of objects and delete them.
		if !objectVersions.IsTruncated {
			break
		}

		objVersionsInput.VersionIdMarker = objectVersions.NextVersionIdMarker
		objVersionsInput.KeyMarker = objectVersions.NextKeyMarker
	}

	return nil
}

func deleteObject(ctx context.Context, s3Backend backendstore.S3Client, bucket, key, versionId *string) error {
	ctx, span := otel.Tracer("").Start(ctx, "deleteObject")
	defer span.End()

	var err error
	for i := 0; i < RequestRetries; i++ {
		_, err = s3Backend.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket:    bucket,
			Key:       key,
			VersionId: versionId,
		})
		if resource.Ignore(IsNotFound, err) == nil {
			return nil
		}
	}

	return err
}

func CreateBucket(ctx context.Context, s3Backend backendstore.S3Client, bucket *s3.CreateBucketInput) (*s3.CreateBucketOutput, error) {
	resp, err := s3Backend.CreateBucket(ctx, bucket)
	if err == nil || resource.Ignore(IsAlreadyExists, err) == nil {
		cache.Set(*bucket.Bucket)
	}

	return resp, err
}

func BucketExists(ctx context.Context, s3Backend backendstore.S3Client, bucketName string) (bool, error) {
	ctx, span := otel.Tracer("").Start(ctx, "BucketExists")
	defer span.End()

	if cache.Exists(bucketName) {
		return true, nil
	}

	_, err := s3Backend.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(bucketName)})
	if err != nil {
		return false, resource.Ignore(IsNotFound, err)
	}

	cache.Set(bucketName)

	// Bucket exists, return true with no error.
	return true, nil
}

// IsAlreadyExists helper function to test for ErrCodeBucketAlreadyOwnedByYou error
func IsAlreadyExists(err error) bool {
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
