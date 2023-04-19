package s3

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"

	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	apisv1alpha1 "github.com/crossplane/provider-ceph/apis/v1alpha1"
)

const (
	defaultRegion = "us-east-1"

	accessKey = "access_key"
	secretKey = "secret_key"
)

func NewClient(ctx context.Context, data map[string][]byte, pcSpec *apisv1alpha1.ProviderConfigSpec) (*s3.Client, error) {
	hostBase := resolveHostBase(pcSpec.HostBase, pcSpec.UseHTTPS)

	endpointResolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
		return aws.Endpoint{
			URL: hostBase,
		}, nil
	})

	sessionConfig, err := config.LoadDefaultConfig(ctx, config.WithEndpointResolverWithOptions(endpointResolver))
	if err != nil {
		return nil, err
	}

	// By default make sure a region is specified, this is required for S3 operations
	region := defaultRegion
	sessionConfig.Region = aws.ToString(&region)

	sessionConfig.Credentials = aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(string(data[accessKey]), string(data[secretKey]), ""))

	return s3.NewFromConfig(sessionConfig, func(o *s3.Options) {
		o.UsePathStyle = true
	}), nil
}

func resolveHostBase(hostBase string, useHTTPS bool) string {
	if !strings.HasPrefix(hostBase, "http") {
		if useHTTPS {
			hostBase = "https://" + hostBase
		} else {
			hostBase = "http://" + hostBase
		}
	}

	return hostBase
}
