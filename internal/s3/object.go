package s3

import (
	"context"

	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/linode/provider-ceph/internal/backendstore"
	"github.com/linode/provider-ceph/internal/otel/traces"
	"go.opentelemetry.io/otel"
)

const (
	errListObjects        = "failed to list objects"
	errListObjectVersions = "failed to list object versions"
	errDeleteObject       = "failed to delete object"
	errGetObject          = "failed to get object"
	errPutObject          = "failed to put object"
)

func GetObject(ctx context.Context, s3Backend backendstore.S3Client, input *awss3.GetObjectInput) (*awss3.GetObjectOutput, error) {
	ctx, span := otel.Tracer("").Start(ctx, "GetObject")
	defer span.End()

	resp, err := s3Backend.GetObject(ctx, input)
	if err != nil {
		err = errors.Wrap(err, errGetObject)
		traces.SetAndRecordError(span, err)

		return resp, err
	}

	return resp, nil
}

func DeleteObject(ctx context.Context, s3Backend backendstore.S3Client, input *awss3.DeleteObjectInput) error {
	ctx, span := otel.Tracer("").Start(ctx, "DeleteObject")
	defer span.End()

	_, err := s3Backend.DeleteObject(ctx, input)
	if err != nil {
		err = errors.Wrap(err, errDeleteObject)
		traces.SetAndRecordError(span, err)

		return err
	}

	return nil
}

func PutObject(ctx context.Context, s3Backend backendstore.S3Client, input *awss3.PutObjectInput) error {
	ctx, span := otel.Tracer("").Start(ctx, "PutObject")
	defer span.End()

	_, err := s3Backend.PutObject(ctx, input)
	if err != nil {
		err = errors.Wrap(err, errPutObject)
		traces.SetAndRecordError(span, err)

		return err
	}

	return nil
}

func ListObjectsV2(ctx context.Context, s3Backend backendstore.S3Client, input *awss3.ListObjectsV2Input) (*awss3.ListObjectsV2Output, error) {
	ctx, span := otel.Tracer("").Start(ctx, "ListObjectsV2")
	defer span.End()

	resp, err := s3Backend.ListObjectsV2(ctx, input)
	if err != nil {
		err = errors.Wrap(err, errListObjects)
		traces.SetAndRecordError(span, err)

		return resp, err
	}

	return resp, nil
}

func ListObjectVersions(ctx context.Context, s3Backend backendstore.S3Client, input *awss3.ListObjectVersionsInput) (*awss3.ListObjectVersionsOutput, error) {
	ctx, span := otel.Tracer("").Start(ctx, "ListObjectsVersions")
	defer span.End()

	resp, err := s3Backend.ListObjectVersions(ctx, input)
	if err != nil {
		err = errors.Wrap(err, errListObjectVersions)
		traces.SetAndRecordError(span, err)

		return resp, err
	}

	return resp, nil
}
