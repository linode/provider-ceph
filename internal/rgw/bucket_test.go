package rgw

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	"github.com/linode/provider-ceph/internal/backendstore/backendstorefakes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeleteBucket(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		s3BackendFunc func(err error) *backendstorefakes.FakeS3Client
		healthCheck   bool
		expectedErr   error
	}{
		"ok - non-healthcheck bucket": {
			s3BackendFunc: func(err error) *backendstorefakes.FakeS3Client {
				fake := &backendstorefakes.FakeS3Client{}
				fake.HeadBucketReturns(nil, nil)

				fake.DeleteBucketReturns(nil, nil)

				return fake
			},
		},
		"ok - healthcheck bucket": {
			s3BackendFunc: func(err error) *backendstorefakes.FakeS3Client {
				fake := &backendstorefakes.FakeS3Client{}
				fake.HeadBucketReturns(nil, nil)

				isTruncated := false
				fake.ListObjectsV2Returns(&s3.ListObjectsV2Output{IsTruncated: &isTruncated}, nil)
				fake.ListObjectVersionsReturns(&s3.ListObjectVersionsOutput{IsTruncated: &isTruncated}, nil)

				fake.DeleteBucketReturns(nil, nil)

				return fake
			},
			healthCheck: true,
		},
		"ok - bucket does not exist": {
			s3BackendFunc: func(err error) *backendstorefakes.FakeS3Client {
				fake := &backendstorefakes.FakeS3Client{}
				fake.HeadBucketReturns(nil, &s3types.NotFound{})

				return fake
			},
		},
		"bucketExists returns unexpected error": {
			s3BackendFunc: func(err error) *backendstorefakes.FakeS3Client {
				fake := &backendstorefakes.FakeS3Client{}
				fake.HeadBucketReturns(nil, err)

				return fake
			},
			expectedErr: errors.New(errHeadBucket),
		},
		"delete objects returns error": {
			s3BackendFunc: func(err error) *backendstorefakes.FakeS3Client {
				fake := &backendstorefakes.FakeS3Client{}
				fake.HeadBucketReturns(nil, nil)

				fake.ListObjectsV2Returns(nil, err)
				isTruncated := false
				fake.ListObjectVersionsReturns(&s3.ListObjectVersionsOutput{IsTruncated: &isTruncated}, nil)

				return fake
			},
			healthCheck: true,
			expectedErr: errors.New(errListObjects),
		},
		"delete object versions returns error": {
			s3BackendFunc: func(err error) *backendstorefakes.FakeS3Client {
				fake := &backendstorefakes.FakeS3Client{}
				fake.HeadBucketReturns(nil, nil)

				isTruncated := false
				fake.ListObjectsV2Returns(&s3.ListObjectsV2Output{IsTruncated: &isTruncated}, nil)
				fake.ListObjectVersionsReturns(nil, err)

				return fake
			},
			healthCheck: true,
			expectedErr: errors.New(errListObjectVersions),
		},
		"bucket not empty error": {
			s3BackendFunc: func(err error) *backendstorefakes.FakeS3Client {
				fake := &backendstorefakes.FakeS3Client{}
				fake.HeadBucketReturns(nil, nil)

				fake.DeleteBucketReturns(nil, BucketNotEmptyError{})

				return fake
			},
			expectedErr: ErrBucketNotEmpty,
		},
		"other error during backend bucket deletion": {
			s3BackendFunc: func(err error) *backendstorefakes.FakeS3Client {
				fake := &backendstorefakes.FakeS3Client{}
				fake.HeadBucketReturns(nil, nil)

				fake.DeleteBucketReturns(nil, err)

				return fake
			},
			expectedErr: errors.New(errDeleteBucket),
		},
	}

	bucketName := "test-bucket"

	for name, tt := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			client := tt.s3BackendFunc(tt.expectedErr)

			err := DeleteBucket(context.Background(), client, &bucketName, tt.healthCheck)

			if tt.expectedErr != nil {
				assert.ErrorIs(t, err, tt.expectedErr, "error does not match")
			} else {
				assert.NoError(t, err, "unexpected error")
			}
		})
	}
}

func TestCreateBucket(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		createBucketErr error
		expectErr       bool
	}{
		"ok - bucket created": {
			createBucketErr: nil,
		},
		"ok - bucket already owned by you": {
			createBucketErr: &s3types.BucketAlreadyOwnedByYou{},
		},
		// Ceph is multi-tenant, so this means another tenant owns the name.
		"error - bucket already exists": {
			createBucketErr: &s3types.BucketAlreadyExists{},
			expectErr:       true,
		},
		"error - unexpected failure is surfaced": {
			createBucketErr: errors.New("boom"),
			expectErr:       true,
		},
	}

	for name, tt := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fake := &backendstorefakes.FakeS3Client{}
			fake.CreateBucketReturns(nil, tt.createBucketErr)

			_, err := CreateBucket(context.Background(), fake, &s3.CreateBucketInput{Bucket: aws.String("test-bucket")})

			if tt.expectErr {
				require.Error(t, err, "expected an error")
				assert.Contains(t, err.Error(), errCreateBucket, "error must be wrapped for context")

				return
			}

			require.NoError(t, err, "already-exists conditions must not be reported as errors")
		})
	}
}
