package backendstore

import (
	"github.com/aws/aws-sdk-go-v2/service/s3"
	rgw "github.com/myENA/radosgwadmin"
)

type backend struct {
	s3Client       *s3.Client
	rgwAdminClient *rgw.AdminAPI
	active         bool
}

func newBackend(s3Client *s3.Client, rgwAdminClient *rgw.AdminAPI, active bool) *backend {
	return &backend{
		s3Client:       s3Client,
		rgwAdminClient: rgwAdminClient,
		active:         active,
	}
}
