package rgw

import (
	"context"

	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	"github.com/linode/provider-ceph/internal/otel/traces"
	"go.opentelemetry.io/otel"
)

const (
	errGetObjectLockConfiguration = "failed to get object lock configuration"
	errPutObjectLockConfiguration = "failed to put object lock configuration"
)

func PutObjectLockConfiguration(ctx context.Context, s3Backend backendstore.S3Client, b *v1alpha1.Bucket) (*awss3.PutObjectLockConfigurationOutput, error) {
	ctx, span := otel.Tracer("").Start(ctx, "PutObjectLockConfiguration")
	defer span.End()

	resp, err := s3Backend.PutObjectLockConfiguration(ctx, GeneratePutObjectLockConfigurationInput(b.Name, b.Spec.ForProvider.ObjectLockConfiguration))
	if err != nil {
		err := errors.Wrap(err, errPutObjectLockConfiguration)
		traces.SetAndRecordError(span, err)

		return resp, err
	}

	return resp, nil
}

func GetObjectLockConfiguration(ctx context.Context, s3Backend backendstore.S3Client, bucketName *string) (*awss3.GetObjectLockConfigurationOutput, error) {
	ctx, span := otel.Tracer("").Start(ctx, "GetObjectLockConfiguration")
	defer span.End()

	resp, err := s3Backend.GetObjectLockConfiguration(ctx, &awss3.GetObjectLockConfigurationInput{Bucket: bucketName})
	if resource.IgnoreAny(err, ObjectLockConfigurationNotFound, IsBucketNotFound) != nil {
		err = errors.Wrap(err, errGetObjectLockConfiguration)
		traces.SetAndRecordError(span, err)

		return resp, err
	}

	return resp, nil
}
