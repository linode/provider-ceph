package bucket

import (
	"sync"

	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
)

type backendStatuses struct {
	backends v1alpha1.BackendStatuses
	mu       sync.RWMutex
}

func newBackendStatuses() *backendStatuses {
	return &backendStatuses{
		backends: make(v1alpha1.BackendStatuses),
	}
}

func (b *backendStatuses) setBackendStatus(backendName string, status v1alpha1.BackendStatus) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.backends[backendName] = status
}

func (b *backendStatuses) deleteBackendFromStatuses(backendName string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	delete(b.backends, backendName)
}

func (b *backendStatuses) getBackendStatuses() v1alpha1.BackendStatuses {
	b.mu.RLock()
	defer b.mu.RUnlock()

	be := make(v1alpha1.BackendStatuses)
	for k, v := range b.backends {
		be[k] = v
	}

	return be
}
