package rgw

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore/backendstorefakes"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const corsMethodGet = "GET"

func TestGenerateCORSConfigurationInput(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		name   string
		config *v1alpha1.CORSConfiguration
		want   bool
	}{
		"nil config returns nil": {
			name:   "my-bucket",
			config: nil,
			want:   false,
		},
		"valid config returns input": {
			name: "my-bucket",
			config: &v1alpha1.CORSConfiguration{
				Rules: []v1alpha1.CORSRule{
					{
						AllowedOrigins: []string{"*"},
						AllowedMethods: []string{corsMethodGet},
					},
				},
			},
			want: true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			result := GenerateCORSConfigurationInput(tc.name, tc.config)
			if tc.want {
				require.NotNil(t, result)
				assert.Equal(t, aws.String(tc.name), result.Bucket)
				assert.NotNil(t, result.CORSConfiguration)
			} else {
				assert.Nil(t, result)
			}
		})
	}
}

func TestGenerateCORSRules(t *testing.T) {
	t.Parallel()

	maxAge := int32(3600)

	testCases := map[string]struct {
		input    []v1alpha1.CORSRule
		expected []s3types.CORSRule
	}{
		"nil input returns nil": {
			input:    nil,
			expected: nil,
		},
		"empty input returns nil": {
			input:    []v1alpha1.CORSRule{},
			expected: nil,
		},
		"single rule with all fields": {
			input: []v1alpha1.CORSRule{
				{
					AllowedHeaders: []string{"*"},
					AllowedMethods: []string{corsMethodGet, "PUT"},
					AllowedOrigins: []string{"https://example.com"},
					ExposeHeaders:  []string{"ETag"},
					ID:             aws.String("rule-1"),
					MaxAgeSeconds:  &maxAge,
				},
			},
			expected: []s3types.CORSRule{
				{
					AllowedHeaders: []string{"*"},
					AllowedMethods: []string{corsMethodGet, "PUT"},
					AllowedOrigins: []string{"https://example.com"},
					ExposeHeaders:  []string{"ETag"},
					ID:             aws.String("rule-1"),
					MaxAgeSeconds:  &maxAge,
				},
			},
		},
		"rule without optional fields": {
			input: []v1alpha1.CORSRule{
				{
					AllowedMethods: []string{corsMethodGet},
					AllowedOrigins: []string{"*"},
				},
			},
			expected: []s3types.CORSRule{
				{
					AllowedMethods: []string{corsMethodGet},
					AllowedOrigins: []string{"*"},
				},
			},
		},
		"multiple rules": {
			input: []v1alpha1.CORSRule{
				{
					AllowedMethods: []string{corsMethodGet},
					AllowedOrigins: []string{"https://foo.com"},
				},
				{
					AllowedMethods: []string{"PUT"},
					AllowedOrigins: []string{"https://bar.com"},
					MaxAgeSeconds:  &maxAge,
				},
			},
			expected: []s3types.CORSRule{
				{
					AllowedMethods: []string{corsMethodGet},
					AllowedOrigins: []string{"https://foo.com"},
				},
				{
					AllowedMethods: []string{"PUT"},
					AllowedOrigins: []string{"https://bar.com"},
					MaxAgeSeconds:  &maxAge,
				},
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			result := GenerateCORSRules(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestCORSConfigurationNotFound(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		err      error
		expected bool
	}{
		"true - CORS not found error": {
			err:      &smithy.GenericAPIError{Code: CORSConfigurationNotFoundErrCode},
			expected: true,
		},
		"false - non-AWS error": {
			err:      errors.New("some error"),
			expected: false,
		},
		"false - different AWS error code": {
			err:      &smithy.GenericAPIError{Code: "SomeOtherError"},
			expected: false,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			actual := CORSConfigurationNotFound(tc.err)
			assert.Equal(t, tc.expected, actual)
		})
	}
}

func TestPutBucketCors(t *testing.T) {
	t.Parallel()

	errSome := errors.New("put error")

	testCases := map[string]struct {
		fake    *backendstorefakes.FakeS3Client
		bucket  *v1alpha1.Bucket
		wantErr bool
	}{
		"success": {
			fake: &backendstorefakes.FakeS3Client{
				PutBucketCorsStub: func(ctx context.Context, input *awss3.PutBucketCorsInput, f ...func(*awss3.Options)) (*awss3.PutBucketCorsOutput, error) {
					return &awss3.PutBucketCorsOutput{}, nil
				},
			},
			bucket: &v1alpha1.Bucket{
				ObjectMeta: metav1.ObjectMeta{Name: "my-bucket"},
				Spec: v1alpha1.BucketSpec{
					ForProvider: v1alpha1.BucketParameters{
						CORSConfiguration: &v1alpha1.CORSConfiguration{
							Rules: []v1alpha1.CORSRule{
								{AllowedMethods: []string{corsMethodGet}, AllowedOrigins: []string{"*"}},
							},
						},
					},
				},
			},
			wantErr: false,
		},
		"error": {
			fake: &backendstorefakes.FakeS3Client{
				PutBucketCorsStub: func(ctx context.Context, input *awss3.PutBucketCorsInput, f ...func(*awss3.Options)) (*awss3.PutBucketCorsOutput, error) {
					return nil, errSome
				},
			},
			bucket: &v1alpha1.Bucket{
				ObjectMeta: metav1.ObjectMeta{Name: "my-bucket"},
				Spec: v1alpha1.BucketSpec{
					ForProvider: v1alpha1.BucketParameters{
						CORSConfiguration: &v1alpha1.CORSConfiguration{
							Rules: []v1alpha1.CORSRule{
								{AllowedMethods: []string{corsMethodGet}, AllowedOrigins: []string{"*"}},
							},
						},
					},
				},
			},
			wantErr: true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := PutBucketCors(context.Background(), tc.fake, tc.bucket)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestDeleteBucketCors(t *testing.T) {
	t.Parallel()

	errSome := errors.New("delete error")

	testCases := map[string]struct {
		fake    *backendstorefakes.FakeS3Client
		wantErr bool
	}{
		"success": {
			fake: &backendstorefakes.FakeS3Client{
				DeleteBucketCorsStub: func(ctx context.Context, input *awss3.DeleteBucketCorsInput, f ...func(*awss3.Options)) (*awss3.DeleteBucketCorsOutput, error) {
					return &awss3.DeleteBucketCorsOutput{}, nil
				},
			},
			wantErr: false,
		},
		"error": {
			fake: &backendstorefakes.FakeS3Client{
				DeleteBucketCorsStub: func(ctx context.Context, input *awss3.DeleteBucketCorsInput, f ...func(*awss3.Options)) (*awss3.DeleteBucketCorsOutput, error) {
					return nil, errSome
				},
			},
			wantErr: true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := DeleteBucketCors(context.Background(), tc.fake, aws.String("my-bucket"))
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestGetBucketCors(t *testing.T) {
	t.Parallel()

	errSome := errors.New("unexpected error")

	testCases := map[string]struct {
		fake     *backendstorefakes.FakeS3Client
		wantErr  bool
		wantResp bool
	}{
		"success with rules": {
			fake: &backendstorefakes.FakeS3Client{
				GetBucketCorsStub: func(ctx context.Context, input *awss3.GetBucketCorsInput, f ...func(*awss3.Options)) (*awss3.GetBucketCorsOutput, error) {
					return &awss3.GetBucketCorsOutput{
						CORSRules: []s3types.CORSRule{
							{AllowedMethods: []string{corsMethodGet}, AllowedOrigins: []string{"*"}},
						},
					}, nil
				},
			},
			wantErr:  false,
			wantResp: true,
		},
		"not found error is ignored": {
			fake: &backendstorefakes.FakeS3Client{
				GetBucketCorsStub: func(ctx context.Context, input *awss3.GetBucketCorsInput, f ...func(*awss3.Options)) (*awss3.GetBucketCorsOutput, error) {
					return nil, &smithy.GenericAPIError{Code: CORSConfigurationNotFoundErrCode}
				},
			},
			wantErr:  false,
			wantResp: false,
		},
		"unexpected error is returned": {
			fake: &backendstorefakes.FakeS3Client{
				GetBucketCorsStub: func(ctx context.Context, input *awss3.GetBucketCorsInput, f ...func(*awss3.Options)) (*awss3.GetBucketCorsOutput, error) {
					return nil, errSome
				},
			},
			wantErr:  true,
			wantResp: false,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			resp, err := GetBucketCors(context.Background(), tc.fake, aws.String("my-bucket"))
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			if tc.wantResp {
				require.NotNil(t, resp)
				assert.NotEmpty(t, resp.CORSRules)
			}
		})
	}
}
