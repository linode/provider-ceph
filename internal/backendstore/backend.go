package backendstore

import (
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/linode/provider-ceph/apis/v1alpha1"
)

type backend struct {
	s3Client *s3.Client
	active   bool
	health   v1alpha1.HealthStatus
}

func newBackend(s3Client *s3.Client, active bool, health v1alpha1.HealthStatus) *backend {
	return &backend{
		s3Client: s3Client,
		active:   active,
		health:   health,
	}
}
