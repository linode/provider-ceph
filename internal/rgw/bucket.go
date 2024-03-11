package rgw

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/linode/provider-ceph/internal/backendstore"
	"github.com/linode/provider-ceph/internal/otel/traces"
	"github.com/linode/provider-ceph/internal/rgw/cache"
	"go.opentelemetry.io/otel"
	"golang.org/x/sync/errgroup"
)

const (
	errGetBucket    = "failed to get bucket"
	errListBuckets  = "failed to list buckets"
	errCreateBucket = "failed to create bucket"
	errUpdateBucket = "failed to update bucket"
	errDeleteBucket = "failed to delete bucket"
	errHeadBucket   = "failed to perform head bucket"
)

func CreateBucket(ctx context.Context, s3Backend backendstore.S3Client, bucket *awss3.CreateBucketInput, o ...func(*awss3.Options)) (*awss3.CreateBucketOutput, error) {
	ctx, span := otel.Tracer("").Start(ctx, "CreateBucket")
	defer span.End()

	resp, err := s3Backend.CreateBucket(ctx, bucket, o...)
	if resource.IgnoreAny(err, IsAlreadyOwnedByYou, IsAlreadyExists) != nil {
		traces.SetAndRecordError(span, err)

		return resp, errors.Wrap(err, errCreateBucket)
	}

	cache.Set(*bucket.Bucket)

	return resp, err
}

func BucketExists(ctx context.Context, s3Backend backendstore.S3Client, bucketName string, o ...func(*awss3.Options)) (bool, error) {
	ctx, span := otel.Tracer("").Start(ctx, "BucketExists")
	defer span.End()

	_, err := s3Backend.HeadBucket(ctx, &awss3.HeadBucketInput{Bucket: aws.String(bucketName)}, o...)
	if err != nil {
		// An IsNotFound error means the call was successful
		// and the bucket does not exist so we return no error.
		if resource.Ignore(IsNotFound, err) == nil {
			return false, nil
		}
		traces.SetAndRecordError(span, err)

		return false, errors.Wrap(err, errHeadBucket)
	}

	cache.Set(bucketName)

	// Bucket exists, return true with no error.
	return true, nil
}

func DeleteBucket(ctx context.Context, s3Backend backendstore.S3Client, bucketName *string, healthCheck bool, o ...func(*awss3.Options)) error {
	ctx, span := otel.Tracer("").Start(ctx, "DeleteBucket")
	defer span.End()

	bucketExists, err := BucketExists(ctx, s3Backend, *bucketName, o...)
	if err != nil {
		return err
	}
	if !bucketExists {
		return nil
	}

	if healthCheck {
		g := new(errgroup.Group)

		// Delete all objects from the bucket. This is sufficient for unversioned buckets.
		g.Go(func() error {
			return deleteBucketObjects(ctx, s3Backend, bucketName, o...)
		})

		// Delete all object versions (required for versioned buckets).
		g.Go(func() error {
			return deleteBucketObjectVersions(ctx, s3Backend, bucketName, o...)
		})

		if err := g.Wait(); err != nil {
			if NoSuchBucket(err) {
				return nil
			}
			traces.SetAndRecordError(span, err)

			return errors.Wrap(err, errDeleteBucket)
		}
	}
	_, err = s3Backend.DeleteBucket(ctx, &awss3.DeleteBucketInput{Bucket: bucketName}, o...)
	if resource.Ignore(IsNotFound, err) != nil {
		traces.SetAndRecordError(span, err)

		return errors.Wrap(err, errDeleteBucket)
	}

	return nil
}

func deleteBucketObjects(ctx context.Context, s3Backend backendstore.S3Client, bucketName *string, o ...func(*awss3.Options)) error {
	ctx, span := otel.Tracer("").Start(ctx, "deleteBucketObjects")
	defer span.End()

	objectsInput := &awss3.ListObjectsV2Input{Bucket: bucketName}
	for {
		objects, err := ListObjectsV2(ctx, s3Backend, objectsInput, o...)
		if err != nil {
			err = errors.Wrap(err, errListObjects)
			traces.SetAndRecordError(span, err)

			return err
		}

		g := new(errgroup.Group)
		for _, object := range objects.Contents {
			obj := object
			g.Go(func() error {
				return DeleteObject(ctx, s3Backend, &awss3.DeleteObjectInput{Bucket: bucketName, Key: obj.Key}, o...)
			})
		}

		if err := g.Wait(); err != nil {
			err = errors.Wrap(err, errDeleteObject)
			traces.SetAndRecordError(span, err)

			return err
		}

		// If the bucket contains many objects, the ListObjectsV2() call
		// might not return all of the objects in the first listing. Check to
		// see whether the listing was truncated. If so, retrieve the next page
		// of objects and delete them.
		if objects.IsTruncated != nil && !*objects.IsTruncated {
			break
		}

		objectsInput.ContinuationToken = objects.ContinuationToken
	}

	return nil
}

func deleteBucketObjectVersions(ctx context.Context, s3Backend backendstore.S3Client, bucketName *string, o ...func(*awss3.Options)) error {
	ctx, span := otel.Tracer("").Start(ctx, "deleteBucketObjectVersions")
	defer span.End()

	objVersionsInput := &awss3.ListObjectVersionsInput{Bucket: bucketName}
	for {
		objectVersions, err := ListObjectVersions(ctx, s3Backend, objVersionsInput, o...)
		if err != nil {
			err = errors.Wrap(err, errListObjects)
			traces.SetAndRecordError(span, err)

			return err
		}

		g := new(errgroup.Group)
		for _, deleteMarkerEntry := range objectVersions.DeleteMarkers {
			delMark := deleteMarkerEntry
			g.Go(func() error {
				return DeleteObject(ctx, s3Backend, &awss3.DeleteObjectInput{Bucket: bucketName, Key: delMark.Key, VersionId: delMark.VersionId}, o...)
			})
		}

		for _, objectVersion := range objectVersions.Versions {
			objVer := objectVersion
			g.Go(func() error {
				return DeleteObject(ctx, s3Backend, &awss3.DeleteObjectInput{Bucket: bucketName, Key: objVer.Key, VersionId: objVer.VersionId}, o...)
			})
		}

		if err := g.Wait(); err != nil {
			err = errors.Wrap(err, errDeleteObject)
			traces.SetAndRecordError(span, err)

			return err
		}

		// If the bucket contains many objects, the ListObjectVersionsV2() call
		// might not return all of the objects in the first listing. Check to
		// see whether the listing was truncated. If so, retrieve the next page
		// of objects and delete them.
		if objectVersions.IsTruncated != nil && !*objectVersions.IsTruncated {
			break
		}

		objVersionsInput.VersionIdMarker = objectVersions.NextVersionIdMarker
		objVersionsInput.KeyMarker = objectVersions.NextKeyMarker
	}

	return nil
}
