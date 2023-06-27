package backendstore

import (
	"sync"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type backend struct {
	mu       sync.RWMutex
	s3Client *s3.Client
	active   bool
}

func newBackend(s3Client *s3.Client, active bool) *backend {
	return &backend{
		s3Client: s3Client,
		active:   active,
	}
}

func (be *backend) getBackendClient() *s3.Client {
	be.mu.RLock()
	defer be.mu.RUnlock()

	return be.s3Client
}

func (be *backend) isBackendActive() bool {
	be.mu.RLock()
	defer be.mu.RUnlock()

	return be.active
}

func (be *backend) toggleBackendActiveStatus(active bool) {
	be.mu.Lock()
	defer be.mu.Unlock()

	be.active = active
}
