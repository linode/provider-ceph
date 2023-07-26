package backendstore

import (
	"sync"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	rgw "github.com/myENA/radosgwadmin"
)

// s3Backends is a map of S3 backend name (eg ceph cluster name) to backend.
type s3Backends map[string]*backend

// BackendStore stores the active s3 backends.
type BackendStore struct {
	s3Backends s3Backends
	mu         sync.RWMutex
}

func NewBackendStore() *BackendStore {
	return &BackendStore{
		s3Backends: make(s3Backends),
	}
}

func (b *BackendStore) GetBackendS3Client(backendName string) *s3.Client {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if _, ok := b.s3Backends[backendName]; ok {
		return b.s3Backends[backendName].s3Client
	}

	return nil
}

func (b *BackendStore) GetAllBackendS3Clients() []*s3.Client {
	b.mu.Lock()
	defer b.mu.Unlock()
	// Create a new clients slice hold a copy of the backend s3 clients
	clients := make([]*s3.Client, 0)
	for _, v := range b.s3Backends {
		clients = append(clients, v.s3Client)
	}

	return clients
}

func (b *BackendStore) GetBackendRgwAdminClient(backendName string) *rgw.AdminAPI {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if _, ok := b.s3Backends[backendName]; ok {
		return b.s3Backends[backendName].rgwAdminClient
	}

	return nil
}

func (b *BackendStore) GetAllBackendRgwAdminClients() []*rgw.AdminAPI {
	b.mu.Lock()
	defer b.mu.Unlock()
	// Create a new clients slice hold a copy of the backend rgw admin clients
	clients := make([]*rgw.AdminAPI, 0)
	for _, v := range b.s3Backends {
		clients = append(clients, v.rgwAdminClient)
	}

	return clients
}

func (b *BackendStore) IsBackendActive(backendName string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if _, ok := b.s3Backends[backendName]; ok {
		return b.s3Backends[backendName].active
	}

	return false
}

func (b *BackendStore) ToggleBackendActiveStatus(backendName string, active bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.s3Backends[backendName]; ok {
		b.s3Backends[backendName].active = active
	}
}

func (b *BackendStore) DeleteBackend(backendName string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	delete(b.s3Backends, backendName)
}

func (b *BackendStore) AddOrUpdateBackend(backendName string, backendClient *s3.Client, rgwAdminClient *rgw.AdminAPI, active bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.s3Backends[backendName] = newBackend(backendClient, rgwAdminClient, active)
}

func (b *BackendStore) GetBackend(backendName string) *backend {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if backend, ok := b.s3Backends[backendName]; ok {
		return backend
	}

	return nil
}

func (b *BackendStore) GetAllBackends() s3Backends {
	b.mu.RLock()
	defer b.mu.RUnlock()
	// Create a new s3Backends to hold a copy of the backends
	backends := make(s3Backends, len(b.s3Backends))
	for k, v := range b.s3Backends {
		backends[k] = v
	}

	return backends
}

func (b *BackendStore) GetBackendStore() *BackendStore {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return b
}

func (b *BackendStore) BackendsAreStored() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return len(b.s3Backends) != 0
}
