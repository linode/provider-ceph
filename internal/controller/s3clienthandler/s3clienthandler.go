package s3clienthandler

import (
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/linode/provider-ceph/internal/backendstore"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Handler struct {
	kubeClient    client.Client
	assumeRoleArn *string
	backendStore  *backendstore.BackendStore
	log           logging.Logger
}

func NewHandler(options ...func(*Handler)) *Handler {
	c := &Handler{}
	for _, o := range options {
		o(c)
	}

	return c
}

func WithKubeClient(k client.Client) func(*Handler) {
	return func(c *Handler) {
		c.kubeClient = k
	}
}

func WithAssumeRoleArn(a *string) func(*Handler) {
	return func(c *Handler) {
		c.assumeRoleArn = a
	}
}

func WithBackendStore(s *backendstore.BackendStore) func(*Handler) {
	return func(c *Handler) {
		c.backendStore = s
	}
}

func WithLog(l logging.Logger) func(*Handler) {
	return func(c *Handler) {
		c.log = l
	}
}

func (c *Handler) GetS3Client(backendName string) (backendstore.S3Client, error) {
	// TODO: We should only return the existing backend s3 client if the user has not
	// specified --assume-role-arn. Otherwise, we should use the backend's STS client
	// perform AssumeRole and use the response credentials to buld a temporary S3 client
	// for the operation being undertaken.
	return c.backendStore.GetBackendS3Client(backendName), nil
}
