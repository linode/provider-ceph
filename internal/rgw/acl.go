package rgw

import (
	"context"

	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/linode/provider-ceph/internal/backendstore"
	"github.com/linode/provider-ceph/internal/otel/traces"
	"go.opentelemetry.io/otel"
)

const (
	errGetBucketACL = "failed to get bucket acl"
	errPutBucketACL = "failed to put bucket acl"
)

func GetBucketAcl(ctx context.Context, s3Backend backendstore.S3Client, in *awss3.GetBucketAclInput, o ...func(*awss3.Options)) (*awss3.GetBucketAclOutput, error) {
	ctx, span := otel.Tracer("").Start(ctx, "GetBucketAcl")
	defer span.End()

	resp, err := s3Backend.GetBucketAcl(ctx, in, o...)
	if err != nil {
		traces.SetAndRecordError(span, err)

		return resp, errors.Wrap(err, errGetBucketACL)
	}

	return resp, err
}

func PutBucketAcl(ctx context.Context, s3Backend backendstore.S3Client, in *awss3.PutBucketAclInput, o ...func(*awss3.Options)) (*awss3.PutBucketAclOutput, error) {
	ctx, span := otel.Tracer("").Start(ctx, "PutBucketAcl")
	defer span.End()

	resp, err := s3Backend.PutBucketAcl(ctx, in, o...)
	if err != nil {
		traces.SetAndRecordError(span, err)

		return resp, errors.Wrap(err, errPutBucketACL)
	}

	return resp, err
}
