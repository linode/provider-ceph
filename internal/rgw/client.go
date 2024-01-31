package rgw

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
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

func NewS3Client(ctx context.Context, data map[string][]byte, pcSpec *apisv1alpha1.ProviderConfigSpec, s3Timeout time.Duration) (*s3.Client, error) {
	sessionConfig, err := buildSessionConfig(ctx, data, pcSpec.HostBase, pcSpec.UseHTTPS)
	if err != nil {
		return nil, err
	}

	return s3.NewFromConfig(sessionConfig, func(o *s3.Options) {
		o.UsePathStyle = true
		o.HTTPClient = &http.Client{Timeout: s3Timeout}
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

	sessionConfig, err := buildSessionConfig(ctx, data, *stsAddress, pcSpec.UseHTTPS)
	if err != nil {
		return nil, err
	}

	return sts.NewFromConfig(sessionConfig, func(o *sts.Options) {
		o.HTTPClient = &http.Client{Timeout: s3Timeout}
	}), nil
}

func buildSessionConfig(ctx context.Context, data map[string][]byte, address string, useHTTPS bool) (aws.Config, error) {
	resolvedAddress := resolveHostBase(address, useHTTPS)

	endpointResolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
		return aws.Endpoint{
			URL: resolvedAddress,
		}, nil
	})

	return config.LoadDefaultConfig(ctx,
		config.WithEndpointResolverWithOptions(endpointResolver),
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
