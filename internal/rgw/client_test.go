package rgw

import (
	"testing"

	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	"github.com/stretchr/testify/assert"
)

func TestResolveRegion(t *testing.T) {
	t.Parallel()

	customRegion := "eu-de-2"
	emptyRegion := ""

	testCases := map[string]struct {
		pcSpec   *apisv1alpha1.ProviderConfigSpec
		expected string
	}{
		"default - region unset": {
			pcSpec:   &apisv1alpha1.ProviderConfigSpec{},
			expected: defaultRegion,
		},
		"default - region empty string": {
			pcSpec:   &apisv1alpha1.ProviderConfigSpec{Region: &emptyRegion},
			expected: defaultRegion,
		},
		"default - nil spec": {
			pcSpec:   nil,
			expected: defaultRegion,
		},
		"override - region set": {
			pcSpec:   &apisv1alpha1.ProviderConfigSpec{Region: &customRegion},
			expected: customRegion,
		},
	}

	for name, tt := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			actual := resolveRegion(tt.pcSpec)

			assert.Equal(t, tt.expected, actual, "result does not match")
			assert.NotEmpty(t, actual, "region must never be empty: the AWS SDK cannot resolve an S3 endpoint without one")
		})
	}
}
