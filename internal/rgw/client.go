package rgw

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/smithy-go/middleware"
	smithyhttp "github.com/aws/smithy-go/transport/http"

	"k8s.io/client-go/util/retry"

	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	"github.com/linode/provider-ceph/internal/consts"
)

const (
	defaultRegion = "us-east-1"
)

func NewS3Client(ctx context.Context, data map[string][]byte, pcSpec *apisv1alpha1.ProviderConfigSpec, s3Timeout time.Duration, sessionToken *string) (*s3.Client, error) {
	sessionConfig, err := buildSessionConfig(ctx, data)
	if err != nil {
		return nil, err
	}

	resolvedAddress := resolveHostBase(pcSpec.HostBase, pcSpec.UseHTTPS)

	return s3.NewFromConfig(sessionConfig, func(o *s3.Options) {
		o.UsePathStyle = true
		o.HTTPClient = &http.Client{Timeout: s3Timeout}
		o.BaseEndpoint = &resolvedAddress
		if sessionToken != nil {
			o.APIOptions = []func(*middleware.Stack) error{
				smithyhttp.AddHeaderValue(consts.KeySecurityToken, *sessionToken),
			}
		}
	}), nil
}

func NewSTSClient(ctx context.Context, data map[string][]byte, pcSpec *apisv1alpha1.ProviderConfigSpec, s3Timeout time.Duration) (*sts.Client, error) {
	// If an STSAddress has not been set in the ProviderConfig Spec, use the HostBase.
	// The STSAddress is only necessary if we wish to contact an STS compliant authentication
	// service separate to the HostBase (i.e RGW address).
	stsAddress := pcSpec.STSAddress
	if stsAddress == nil {
		stsAddress = &pcSpec.HostBase
	}

	sessionConfig, err := buildSessionConfig(ctx, data)
	if err != nil {
		return nil, err
	}

	resolvedAddress := resolveHostBase(*stsAddress, pcSpec.UseHTTPS)

	return sts.NewFromConfig(sessionConfig, func(o *sts.Options) {
		o.HTTPClient = &http.Client{Timeout: s3Timeout}
		o.BaseEndpoint = &resolvedAddress
	}), nil
}

func buildSessionConfig(ctx context.Context, data map[string][]byte) (aws.Config, error) {
	return config.LoadDefaultConfig(ctx,
		config.WithRetryMaxAttempts(retry.DefaultRetry.Steps),
		config.WithRetryMode(aws.RetryModeStandard),
		config.WithRegion(defaultRegion),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			string(data[consts.KeyAccessKey]),
			string(data[consts.KeySecretKey]),
			"",
		)))
}

func resolveHostBase(hostBase string, useHTTPS bool) string {
	httpsPrefix := "https://"
	httpPrefix := "http://"
	// Remove prefix in either case if it has been specified.
	// Let useHTTPS option take precedence.
	hostBase = strings.TrimPrefix(hostBase, httpPrefix)
	hostBase = strings.TrimPrefix(hostBase, httpsPrefix)

	if useHTTPS {
		return httpsPrefix + hostBase
	}

	return httpPrefix + hostBase
}
