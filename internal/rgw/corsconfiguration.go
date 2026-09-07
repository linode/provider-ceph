//nolint:dupl // Similar to serversideencryptionconfiguration.go
package rgw

import (
	"context"

	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"go.opentelemetry.io/otel"

	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	"github.com/linode/provider-ceph/internal/otel/traces"
)

const (
	errGetBucketCors    = "failed to get bucket CORS configuration"
	errPutBucketCors    = "failed to put bucket CORS configuration"
	errDeleteBucketCors = "failed to delete bucket CORS configuration"
)

func PutBucketCors(ctx context.Context, s3Backend backendstore.S3Client, b *v1alpha1.Bucket) (*awss3.PutBucketCorsOutput, error) {
	ctx, span := otel.Tracer("").Start(ctx, "PutBucketCors")
	defer span.End()

	resp, err := s3Backend.PutBucketCors(
		ctx,
		GenerateCORSConfigurationInput(
			b.Name,
			b.Spec.ForProvider.CORSConfiguration,
		),
	)
	if err != nil {
		err := errors.Wrap(err, errPutBucketCors)
		traces.SetAndRecordError(span, err)

		return resp, err
	}

	return resp, nil
}

func DeleteBucketCors(ctx context.Context, s3Backend backendstore.S3Client, bucketName *string) error {
	ctx, span := otel.Tracer("").Start(ctx, "DeleteBucketCors")
	defer span.End()

	_, err := s3Backend.DeleteBucketCors(ctx,
		&awss3.DeleteBucketCorsInput{
			Bucket: bucketName,
		},
	)
	if err != nil {
		err := errors.Wrap(err, errDeleteBucketCors)
		traces.SetAndRecordError(span, err)

		return err
	}

	return nil
}

func GetBucketCors(ctx context.Context, s3Backend backendstore.S3Client, bucketName *string) (*awss3.GetBucketCorsOutput, error) {
	ctx, span := otel.Tracer("").Start(ctx, "GetBucketCors")
	defer span.End()

	resp, err := s3Backend.GetBucketCors(ctx, &awss3.GetBucketCorsInput{Bucket: bucketName})
	if resource.IgnoreAny(err, CORSConfigurationNotFound, IsBucketNotFound) != nil {
		err = errors.Wrap(err, errGetBucketCors)
		traces.SetAndRecordError(span, err)

		return resp, err
	}

	return resp, nil
}
