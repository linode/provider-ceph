package randomstring

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStandardGenerator_Generate(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		prefix  string
		length  int
		charset *Charset

		assertions  func(t *testing.T, str string)
		expectedErr error
	}{
		"good generation": {
			prefix:  "AK",
			length:  3,
			charset: NewCharset("Z"),
			assertions: func(t *testing.T, str string) {
				t.Helper()
				require.Equal(t, "AKZ", str)
			},
		},
		"no prefix": {
			prefix:  "",
			length:  3,
			charset: NewCharset("Z"),
			assertions: func(t *testing.T, str string) {
				t.Helper()
				require.Equal(t, "ZZZ", str)
			},
		},
		"length too short": {
			prefix:      "AK",
			length:      2,
			expectedErr: errRandomStringLengthTooShort,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			str, err := StandardGenerator{}.Generate(tt.prefix, tt.length, tt.charset)

			switch {
			case tt.expectedErr != nil:
				require.ErrorIs(t, err, tt.expectedErr)
			default:
				require.NoError(t, err)
				tt.assertions(t, str)
			}
		})
	}
}
