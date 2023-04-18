package s3

import (
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/endpoints"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	apisv1alpha1 "github.com/crossplane/provider-ceph/apis/v1alpha1"
)

const (
	defaultRegion = "us-east-1"

	accessKey = "access_key"
	secretKey = "secret_key"
)

func NewClient(data map[string][]byte, pcSpec *apisv1alpha1.ProviderConfigSpec) *s3.S3 {
	// By default make sure a region is specified, this is required for S3 operations
	sessionConfig := aws.Config{Region: aws.String(defaultRegion)}

	sessionConfig.Credentials = credentials.NewStaticCredentials(string(data[accessKey]), string(data[secretKey]), "")

	sessionConfig.EndpointResolver = buildEndpointResolver(pcSpec)

	// This setting is necessary to interact with ceph
	// see https://github.com/aws/aws-sdk-go/issues/1585
	sessionConfig.WithS3ForcePathStyle(true)

	return s3.New(session.Must(session.NewSessionWithOptions(session.Options{
		Config:            sessionConfig,
		SharedConfigState: session.SharedConfigEnable,
	})))
}

func buildEndpointResolver(pcSpec *apisv1alpha1.ProviderConfigSpec) endpoints.Resolver {
	defaultResolver := endpoints.DefaultResolver()

	hostBase := pcSpec.HostBase
	if !strings.HasPrefix(hostBase, "http") {
		if pcSpec.UseHTTPS {
			hostBase = "https://" + hostBase
		} else {
			hostBase = "http://" + hostBase
		}
	}

	return endpoints.ResolverFunc(func(service, region string, optFns ...func(*endpoints.Options)) (endpoints.ResolvedEndpoint, error) {
		if service == endpoints.S3ServiceID {
			return endpoints.ResolvedEndpoint{
				URL: hostBase,
			}, nil
		}

		return defaultResolver.EndpointFor(service, region, optFns...)
	})
}
