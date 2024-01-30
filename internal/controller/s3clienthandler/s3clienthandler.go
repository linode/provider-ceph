package s3clienthandler

import (
	"time"

	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/linode/provider-ceph/internal/backendstore"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Handler struct {
	kubeClient    client.Client
	assumeRoleArn *string
	backendStore  *backendstore.BackendStore
	s3Timeout     time.Duration
	log           logging.Logger
}

func NewHandler(options ...func(*Handler)) *Handler {
	h := &Handler{}
	for _, o := range options {
		o(h)
	}

	return h
}

func WithKubeClient(k client.Client) func(*Handler) {
	return func(h *Handler) {
		h.kubeClient = k
	}
}

func WithAssumeRoleArn(a *string) func(*Handler) {
	return func(h *Handler) {
		h.assumeRoleArn = a
	}
}

func WithBackendStore(s *backendstore.BackendStore) func(*Handler) {
	return func(h *Handler) {
		h.backendStore = s
	}
}

func WithS3Timeout(t time.Duration) func(*Handler) {
	return func(h *Handler) {
		h.s3Timeout = t
	}
}

func WithLog(l logging.Logger) func(*Handler) {
	return func(h *Handler) {
		h.log = l
	}
}

func (h *Handler) GetS3Client(backendName string) (backendstore.S3Client, error) {
	// TODO: We should only return the existing backend s3 client if the user has not
	// specified --assume-role-arn. Otherwise, we should use the backend's STS client
	// perform AssumeRole and use the response credentials to buld a temporary S3 client
	// for the operation being undertaken.
	return h.backendStore.GetBackendS3Client(backendName), nil
}
