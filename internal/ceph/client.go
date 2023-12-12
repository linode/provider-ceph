package ceph

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
)

const (
	defaultRegion = "us-east-1"

	AccessKey = "access_key"
	SecretKey = "secret_key"
)

func NewS3Client(ctx context.Context, data map[string][]byte, pcSpec *apisv1alpha1.ProviderConfigSpec, s3Timeout time.Duration) (*s3.Client, error) {
	sessionConfig, err := buildSessionConfig(ctx, data, pcSpec)
	if err != nil {
		return nil, err
	}

	return s3.NewFromConfig(sessionConfig, func(o *s3.Options) {
		o.UsePathStyle = true
		o.HTTPClient = &http.Client{Timeout: s3Timeout}
	}), nil
}

func NewSTSClient(ctx context.Context, data map[string][]byte, pcSpec *apisv1alpha1.ProviderConfigSpec, s3Timeout time.Duration) (*sts.Client, error) {
	sessionConfig, err := buildSessionConfig(ctx, data, pcSpec)
	if err != nil {
		return nil, err
	}

	return sts.NewFromConfig(sessionConfig, func(o *sts.Options) {
		o.HTTPClient = &http.Client{Timeout: s3Timeout}
	}), nil
}

func buildSessionConfig(ctx context.Context, data map[string][]byte, pcSpec *apisv1alpha1.ProviderConfigSpec) (aws.Config, error) {
	resolvedAddress := resolveHostBase(pcSpec.HostBase, pcSpec.UseHTTPS)

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
			string(data[AccessKey]),
			string(data[SecretKey]),
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
