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
