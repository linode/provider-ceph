package bucket

import (
	"sync"

	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
)

type bucketBackends struct {
	// bucketBackendStatuses maps bucket names to backend statuses
	// for backends on which the bucket exists.
	bucketBackendStatuses map[string]v1alpha1.BackendStatuses
	mu                    sync.RWMutex
}

func newBucketBackends() *bucketBackends {
	return &bucketBackends{
		bucketBackendStatuses: make(map[string]v1alpha1.BackendStatuses),
	}
}

func (b *bucketBackends) setBucketBackendStatus(bucketName, backendName string, status v1alpha1.BackendStatus) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.bucketBackendStatuses[bucketName] == nil {
		b.bucketBackendStatuses[bucketName] = make(v1alpha1.BackendStatuses)
	}

	b.bucketBackendStatuses[bucketName][backendName] = status
}

func (b *bucketBackends) deleteBucketBackend(bucketName, backendName string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.bucketBackendStatuses[bucketName]; !ok {
		return
	}

	delete(b.bucketBackendStatuses[bucketName], backendName)
}

func (b *bucketBackends) getBucketBackendStatuses(bucketName string, beNames []string) v1alpha1.BackendStatuses {
	requestedBackends := map[string]bool{}
	for p := range beNames {
		requestedBackends[beNames[p]] = true
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	be := make(v1alpha1.BackendStatuses)
	if _, ok := b.bucketBackendStatuses[bucketName]; !ok {
		return be
	}

	for k, v := range b.bucketBackendStatuses[bucketName] {
		if _, ok := requestedBackends[k]; !ok {
			continue
		}

		be[k] = v
	}

	return be
}
