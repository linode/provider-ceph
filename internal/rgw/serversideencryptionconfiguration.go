//nolint:dupl // Similar to lifecycleconfig.go
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
	errGetBucketEncryption    = "failed to get bucket encryption"
	errPutBucketEncryption    = "failed to put bucket encryption"
	errDeleteBucketEncryption = "failed to delete bucket encryption"
)

func PutBucketEncryption(ctx context.Context, s3Backend backendstore.S3Client, b *v1alpha1.Bucket) (*awss3.PutBucketEncryptionOutput, error) {
	ctx, span := otel.Tracer("").Start(ctx, "PutBucketEncryption")
	defer span.End()

	resp, err := s3Backend.PutBucketEncryption(
		ctx,
		GenerateServerSideEncryptionConfigurationInput(
			b.Name,
			b.Spec.ForProvider.ServerSideEncryptionConfiguration,
		),
	)
	if err != nil {
		err := errors.Wrap(err, errPutBucketEncryption)
		traces.SetAndRecordError(span, err)

		return resp, err
	}

	return resp, nil
}

func DeleteBucketEncryption(ctx context.Context, s3Backend backendstore.S3Client, bucketName *string) error {
	ctx, span := otel.Tracer("").Start(ctx, "DeleteBucketEncryption")
	defer span.End()

	_, err := s3Backend.DeleteBucketEncryption(ctx,
		&awss3.DeleteBucketEncryptionInput{
			Bucket: bucketName,
		},
	)
	if err != nil {
		err := errors.Wrap(err, errDeleteBucketEncryption)
		traces.SetAndRecordError(span, err)

		return err
	}

	return nil
}

func GetBucketEncryption(ctx context.Context, s3Backend backendstore.S3Client, bucketName *string) (*awss3.GetBucketEncryptionOutput, error) {
	ctx, span := otel.Tracer("").Start(ctx, "GetBucketEncryption")
	defer span.End()

	resp, err := s3Backend.GetBucketEncryption(ctx, &awss3.GetBucketEncryptionInput{Bucket: bucketName})
	if resource.IgnoreAny(err, ServerSideEncryptionConfigurationNotFound, IsBucketNotFound) != nil {
		err = errors.Wrap(err, errGetBucketEncryption)
		traces.SetAndRecordError(span, err)

		return resp, err
	}

	return resp, nil
}
