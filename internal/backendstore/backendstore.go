package backendstore

import (
	"sync"

	"github.com/linode/provider-ceph/apis/v1alpha1"
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

func (b *BackendStore) GetBackendSTSClient(backendName string) STSClient {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if _, ok := b.s3Backends[backendName]; ok {
		return b.s3Backends[backendName].stsClient
	}

	return nil
}

func (b *BackendStore) GetBackendS3Client(backendName string) S3Client {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if _, ok := b.s3Backends[backendName]; ok {
		return b.s3Backends[backendName].s3Client
	}

	return nil
}

func (b *BackendStore) GetAllBackendS3Clients() []S3Client {
	b.mu.RLock()
	defer b.mu.RUnlock()

	// Create a new clients slice hold a copy of the backend clients
	clients := make([]S3Client, 0)
	for _, v := range b.s3Backends {
		clients = append(clients, v.s3Client)
	}

	return clients
}

func (b *BackendStore) GetBackendS3Clients(beNames []string) map[string]S3Client {
	requestedBackends := map[string]bool{}
	for p := range beNames {
		requestedBackends[beNames[p]] = true
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	// Create a new clients slice hold a copy of the backend clients
	clients := map[string]S3Client{}
	for k, v := range b.s3Backends {
		if _, ok := requestedBackends[k]; !ok {
			continue
		}
		clients[k] = v.s3Client
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

func (b *BackendStore) GetBackendHealthStatus(backendName string) v1alpha1.HealthStatus {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if _, ok := b.s3Backends[backendName]; ok {
		return b.s3Backends[backendName].health
	}

	return v1alpha1.HealthStatusUnknown
}

func (b *BackendStore) SetBackendHealthStatus(backendName string, health v1alpha1.HealthStatus) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.s3Backends[backendName]; ok {
		b.s3Backends[backendName].health = health
	}
}

func (b *BackendStore) DeleteBackend(backendName string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	delete(b.s3Backends, backendName)
}

func (b *BackendStore) AddOrUpdateBackend(backendName string, s3Client S3Client, stsClient STSClient, active bool, health v1alpha1.HealthStatus) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.s3Backends[backendName] = newBackend(s3Client, stsClient, active, health)
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

func (b *BackendStore) GetActiveBackends(beNames []string) s3Backends {
	requestedBackends := map[string]bool{}
	for p := range beNames {
		requestedBackends[beNames[p]] = true
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	// Create a new s3Backends to hold a copy of the backends
	backends := make(s3Backends, 0)
	for k, v := range b.s3Backends {
		if _, ok := requestedBackends[k]; !ok || !v.active || v.health == v1alpha1.HealthStatusUnhealthy {
			continue
		}

		backends[k] = v
	}

	return backends
}

func (b *BackendStore) GetAllActiveBackendNames() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()

	backends := make([]string, 0)
	for k, v := range b.s3Backends {
		if !v.active || v.health == v1alpha1.HealthStatusUnhealthy {
			continue
		}

		backends = append(backends, k)
	}

	return backends
}

func (b *BackendStore) BackendsAreStored() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return len(b.s3Backends) != 0
}

func (s s3Backends) GetFirst() S3Client {
	for _, v := range s {
		return v.s3Client
	}

	return nil
}
