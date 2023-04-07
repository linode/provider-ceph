package s3

import (
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/endpoints"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"

	"fmt"
)

func CreateBucket(client *s3.S3, bucketName string) error {
	_, err := client.CreateBucket(&s3.CreateBucketInput{
		Bucket: aws.String(bucketName),
	})

	return err
}

func ListBuckets(client *s3.S3) error {
	resp, err := client.ListBuckets(nil)
	if err != nil {
		return err
	}
	for _, bucket := range resp.Buckets {
		fmt.Println(*bucket.Name)
	}
	return nil
}

const (
	defaultRegion = "us-east-1"

	accessKey = "access_key"
	secretKey = "secret_key"
)

func NewClient(data map[string][]byte, hostBase string) *s3.S3 {
	// By default make sure a region is specified, this is required for S3 operations
	sessionConfig := aws.Config{Region: aws.String(defaultRegion)}

	sessionConfig.Credentials = credentials.NewStaticCredentials(string(data[accessKey]), string(data[secretKey]), "")

	sessionConfig.EndpointResolver = buildEndpointResolver(hostBase)

	// This setting is necessary to interact with ceph
	// see https://github.com/aws/aws-sdk-go/issues/1585
	sessionConfig.WithS3ForcePathStyle(true)

	return s3.New(session.Must(session.NewSessionWithOptions(session.Options{
		Config:            sessionConfig,
		SharedConfigState: session.SharedConfigEnable,
	})))
}

func buildEndpointResolver(hostname string) endpoints.Resolver {
	defaultResolver := endpoints.DefaultResolver()

	fixedHost := hostname
	// TODO: Using http for testing for now as ceph clusters
	// deployed by ceph-deploy are not https by default AFAIK.
	if !strings.HasPrefix(hostname, "http") {
		fixedHost = "http://" + hostname
	}

	return endpoints.ResolverFunc(func(service, region string, optFns ...func(*endpoints.Options)) (endpoints.ResolvedEndpoint, error) {
		if service == endpoints.S3ServiceID {
			return endpoints.ResolvedEndpoint{
				URL: fixedHost,
			}, nil
		}

		return defaultResolver.EndpointFor(service, region, optFns...)
	})
}
