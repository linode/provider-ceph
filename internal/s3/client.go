package s3

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"k8s.io/client-go/util/retry"

	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

const (
	defaultRegion = "us-east-1"

	accessKey = "access_key"
	secretKey = "secret_key"
)

func NewS3Client(ctx context.Context, data map[string][]byte, address string, useHTTPS bool) (*s3.Client, error) {
	sessionConfig, err := buildSessionConfig(ctx, data, address, useHTTPS)
	if err != nil {
		return nil, err
	}

	return s3.NewFromConfig(*sessionConfig, func(o *s3.Options) {
		o.UsePathStyle = true
	}), nil
}

func NewSTSClient(ctx context.Context, data map[string][]byte, address string, useHTTPS bool) (*sts.Client, error) {
	sessionConfig, err := buildSessionConfig(ctx, data, address, useHTTPS)
	if err != nil {
		return nil, err
	}

	return sts.NewFromConfig(*sessionConfig, func(o *sts.Options) {}), nil
}

func buildSessionConfig(ctx context.Context, data map[string][]byte, address string, useHTTPS bool) (*aws.Config, error) {
	resolvedAddress := resolveHostBase(address, useHTTPS)

	endpointResolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
		return aws.Endpoint{
			URL: resolvedAddress,
		}, nil
	})

	sessionConfig, err := config.LoadDefaultConfig(ctx,
		config.WithEndpointResolverWithOptions(endpointResolver),
		config.WithRetryMaxAttempts(retry.DefaultRetry.Steps),
		config.WithRetryMode(aws.RetryModeStandard))
	if err != nil {
		return nil, err
	}

	// By default make sure a region is specified, this is required for S3 operations
	region := defaultRegion
	sessionConfig.Region = aws.ToString(&region)

	sessionConfig.Credentials = aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(string(data[accessKey]), string(data[secretKey]), ""))

	return &sessionConfig, nil
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
