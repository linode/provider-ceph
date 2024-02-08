package healthcheck

import (
	"testing"

	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/stretchr/testify/assert"
)

func TestErrNoRequestID(t *testing.T) {
	t.Parallel()
	type args struct {
		err error
	}

	type want struct {
		result string
	}

	cases := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"Request ID removed": {
			args: args{
				err: errors.New("failed to perform head bucket: operation error S3: HeadBucket, https response error StatusCode: 403, RequestID: tx00000f31fc4ad88d76e2b-0065c4d7b8-43d9-default, HostID: , api error Forbidden: Forbidden"),
			},
			want: want{
				result: "failed to perform head bucket: operation error S3: HeadBucket, https response error StatusCode: 403, HostID: , api error Forbidden: Forbidden",
			},
		},
		"No Request ID to remove so no change": {
			args: args{
				err: errors.New("failed to perform head bucket: operation error S3: HeadBucket, https response error StatusCode: 403, HostID: , api error Forbidden: Forbidden"),
			},
			want: want{
				result: "failed to perform head bucket: operation error S3: HeadBucket, https response error StatusCode: 403, HostID: , api error Forbidden: Forbidden",
			},
		},
		"Error not comma separated so no change": {
			args: args{
				err: errors.New("failed to perform head bucket: operation error S3: HeadBucket"),
			},
			want: want{
				result: "failed to perform head bucket: operation error S3: HeadBucket",
			},
		},
	}
	for name, tc := range cases {
		tc := tc

		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want.result, errNoRequestID(tc.args.err), "unexpected string")
		})
	}
}
