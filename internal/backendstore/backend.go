package backendstore

import (
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type backend struct {
	s3Client *s3.Client
	active   bool
}

func newBackend(s3Client *s3.Client, active bool) *backend {
	return &backend{
		s3Client: s3Client,
		active:   active,
	}
}
