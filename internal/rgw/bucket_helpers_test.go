package rgw

import (
	"testing"

	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/stretchr/testify/assert"
)

func TestIsNotEmpty(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		err      error
		expected bool
	}{
		"true - BucketNotEmpty error": {
			err:      bucketNotEmptyError{},
			expected: true,
		},
		"false - Error not implement AWS API error": {
			err:      errors.New("some error"),
			expected: false,
		},
		"false - Other AWS API error": {
			err:      &s3types.NoSuchBucket{},
			expected: false,
		},
	}

	for name, tt := range testCases {
		tt := tt

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			actual := IsNotEmpty(tt.err)

			assert.Equal(t, tt.expected, actual, "result does not match")
		})
	}
}

// Unlike NoSuchBucket error or others, aws-sdk-go-v2 doesn't have a specific struct definition for BucketNotEmpty error.
// So we should define ourselves for testing.
type bucketNotEmptyError struct{}

func (e bucketNotEmptyError) Error() string {
	return "BucketNotEmpty: some error"
}

func (e bucketNotEmptyError) ErrorCode() string {
	return "BucketNotEmpty"
}

func (e bucketNotEmptyError) ErrorMessage() string {
	return "some error"
}

func (e bucketNotEmptyError) ErrorFault() smithy.ErrorFault {
	return smithy.FaultUnknown
}
