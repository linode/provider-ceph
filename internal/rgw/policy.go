package rgw

import (
	"context"

	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	"github.com/linode/provider-ceph/internal/otel/traces"
	"go.opentelemetry.io/otel"
)

const (
	errGetBucketPolicy    = "failed to get bucket policy"
	errPutBucketPolicy    = "failed to put bucket policy"
	errDeleteBucketPolicy = "failed to delete bucket policy"
)

func GetBucketPolicy(ctx context.Context, s3Backend backendstore.S3Client, bucketName *string) (*awss3.GetBucketPolicyOutput, error) {
	ctx, span := otel.Tracer("").Start(ctx, "GetBucketPolicy")
	defer span.End()

	resp, err := s3Backend.GetBucketPolicy(ctx, &awss3.GetBucketPolicyInput{Bucket: bucketName})
	if err != nil {
		traces.SetAndRecordError(span, err)

		return resp, errors.Wrap(err, errGetBucketPolicy)
	}

	return resp, nil
}

func PutBucketPolicy(ctx context.Context, s3Backend backendstore.S3Client, b *v1alpha1.Bucket) (*awss3.PutBucketPolicyOutput, error) {
	ctx, span := otel.Tracer("").Start(ctx, "PutBucketPolicy")
	defer span.End()

	resp, err := s3Backend.PutBucketPolicy(ctx, &awss3.PutBucketPolicyInput{Bucket: &b.Name, Policy: &b.Spec.ForProvider.Policy})
	if err != nil {
		traces.SetAndRecordError(span, err)

		return resp, errors.Wrap(err, errPutBucketPolicy)
	}

	return resp, nil
}

func DeleteBucketPolicy(ctx context.Context, s3Backend backendstore.S3Client, bucketName *string) error {
	ctx, span := otel.Tracer("").Start(ctx, "DeleteBucketPolicy")
	defer span.End()

	_, err := s3Backend.DeleteBucketPolicy(ctx, &awss3.DeleteBucketPolicyInput{Bucket: bucketName})
	if err != nil {
		traces.SetAndRecordError(span, err)

		return errors.Wrap(err, errDeleteBucketPolicy)
	}

	return nil
}
