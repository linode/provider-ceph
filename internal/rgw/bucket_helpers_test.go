package rgw

import (
	"testing"

	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
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
			err:      BucketNotEmptyError{},
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
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			actual := IsNotEmpty(tt.err)

			assert.Equal(t, tt.expected, actual, "result does not match")
		})
	}
}
