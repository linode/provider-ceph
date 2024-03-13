package rgw

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/linode/provider-ceph/internal/backendstore/backendstorefakes"
	"github.com/stretchr/testify/assert"
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

				fake.DeleteBucketReturns(nil, bucketNotEmptyError{})

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
		tt := tt

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
