package rgw

import (
	"context"

	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	"github.com/linode/provider-ceph/internal/otel/traces"
	"go.opentelemetry.io/otel"
)

const (
	errGetBucketVersioning = "failed to get bucket versioning"
	errPutBucketVersioning = "failed to put bucket versioning"
)

func PutBucketVersioning(ctx context.Context, s3Backend backendstore.S3Client, b *v1alpha1.Bucket) (*awss3.PutBucketVersioningOutput, error) {
	ctx, span := otel.Tracer("").Start(ctx, "PutBucketVersioning")
	defer span.End()

	resp, err := s3Backend.PutBucketVersioning(ctx, GeneratePutBucketVersioningInput(b.Name, b.Spec.ForProvider.VersioningConfiguration))
	if err != nil {
		err := errors.Wrap(err, errPutBucketVersioning)
		traces.SetAndRecordError(span, err)

		return resp, err
	}

	return resp, nil
}

func GetBucketVersioning(ctx context.Context, s3Backend backendstore.S3Client, bucketName *string) (*awss3.GetBucketVersioningOutput, error) {
	ctx, span := otel.Tracer("").Start(ctx, "GetBucketVersioning")
	defer span.End()

	resp, err := s3Backend.GetBucketVersioning(ctx, &awss3.GetBucketVersioningInput{Bucket: bucketName})
	if resource.IgnoreAny(err, IsBucketNotFound) != nil {
		err = errors.Wrap(err, errGetBucketVersioning)
		traces.SetAndRecordError(span, err)

		return resp, err
	}

	return resp, nil
}
