package rgw

import (
	"context"

	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/crossplane/crossplane-runtime/pkg/resource"
	"go.opentelemetry.io/otel"

	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	"github.com/linode/provider-ceph/internal/otel/traces"
)

const (
	errGetLifecycleConfig = "failed to get bucket lifecycle configuration"
	errPutLifecycleConfig = "failed to put bucket lifecycle configuration"
	errDeleteLifecycle    = "failed to delete bucket lifecycle"
)

func PutBucketLifecycleConfiguration(ctx context.Context, s3Backend backendstore.S3Client, b *v1alpha1.Bucket) (*awss3.PutBucketLifecycleConfigurationOutput, error) {
	ctx, span := otel.Tracer("").Start(ctx, "PutBucketLifecycleConfiguration")
	defer span.End()

	resp, err := s3Backend.PutBucketLifecycleConfiguration(ctx, GenerateLifecycleConfigurationInput(b.Name, b.Spec.ForProvider.LifecycleConfiguration))
	if err != nil {
		err := errors.Wrap(err, errPutLifecycleConfig)
		traces.SetAndRecordError(span, err)

		return resp, err
	}

	return resp, nil
}

func DeleteBucketLifecycle(ctx context.Context, s3Backend backendstore.S3Client, bucketName *string) error {
	ctx, span := otel.Tracer("").Start(ctx, "DeleteBucketLifecycle")
	defer span.End()

	_, err := s3Backend.DeleteBucketLifecycle(ctx,
		&awss3.DeleteBucketLifecycleInput{
			Bucket: bucketName,
		},
	)
	if err != nil {
		err := errors.Wrap(err, errDeleteLifecycle)
		traces.SetAndRecordError(span, err)

		return err
	}

	return nil
}

func GetBucketLifecycleConfiguration(ctx context.Context, s3Backend backendstore.S3Client, bucketName *string) (*awss3.GetBucketLifecycleConfigurationOutput, error) {
	ctx, span := otel.Tracer("").Start(ctx, "GetBucketLifecycleConfiguration")
	defer span.End()

	resp, err := s3Backend.GetBucketLifecycleConfiguration(ctx, &awss3.GetBucketLifecycleConfigurationInput{Bucket: bucketName})
	if resource.IgnoreAny(err, LifecycleConfigurationNotFound, IsBucketNotFound) != nil {
		err = errors.Wrap(err, errGetLifecycleConfig)
		traces.SetAndRecordError(span, err)

		return resp, err
	}

	return resp, nil
}
