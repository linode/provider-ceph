package s3clienthandler

import (
	"testing"
	"time"

	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/linode/provider-ceph/internal/utils/randomstring/randomstringfakes"

	"github.com/stretchr/testify/require"
)

func TestRoleSessionNameGenerator_Generate(t *testing.T) {
	t.Parallel()

	errRandom := errors.New("random error")
	now := func() time.Time {
		return time.Date(2023, 12, 11, 13, 39, 51, 0, time.UTC)
	}

	tests := map[string]struct {
		randomStringGenerator           *randomstringfakes.FakeGenerator
		randomStringGeneratorAssertions func(t *testing.T, fake *randomstringfakes.FakeGenerator)

		serviceName string

		expected    string
		expectedErr error
	}{
		"good session name": {
			randomStringGenerator: func() *randomstringfakes.FakeGenerator {
				fake := &randomstringfakes.FakeGenerator{}
				fake.GenerateReturns("randomstring", nil)

				return fake
			}(),
			randomStringGeneratorAssertions: func(t *testing.T, fake *randomstringfakes.FakeGenerator) {
				t.Helper()
				require.Equal(t, 1, fake.GenerateCallCount())
				prefix, length, charset := fake.GenerateArgsForCall(0)
				require.Equal(t, "", prefix)
				require.Equal(t, roleSessionNameSuffixLength, length)
				require.Same(t, charset, roleSessionNameSuffixCharset)
			},
			serviceName: "bucketcrud",
			expected:    "bucketcrud-20231211T133951Z-randomstring",
		},
		"service name too short": {
			serviceName: "",
			expectedErr: errServiceNameRequired,
		},
		"service name too long": {
			serviceName: string(make([]byte, roleSessionNameServiceNameMaxLength+1)),
			expectedErr: errServiceNameTooLong,
		},
		"random suffix generation failed": {
			randomStringGenerator: func() *randomstringfakes.FakeGenerator {
				fake := &randomstringfakes.FakeGenerator{}
				fake.GenerateReturns("", errRandom)

				return fake
			}(),
			serviceName: "bucketcrud",
			expectedErr: errSuffixGenerationFailed,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			generator := newRoleSessionNameGenerator()
			generator.now = now
			if tt.randomStringGenerator != nil {
				generator.randomStringGenerator = tt.randomStringGenerator
			}

			roleSessionName, err := generator.generate(tt.serviceName)

			if tt.randomStringGeneratorAssertions != nil {
				tt.randomStringGeneratorAssertions(t, tt.randomStringGenerator)
			}

			switch {
			case tt.expectedErr != nil:
				require.ErrorContains(t, err, tt.expectedErr.Error())
			default:
				require.Equal(t, tt.expected, roleSessionName)
			}
		})
	}
}
