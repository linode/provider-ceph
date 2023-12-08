package bucket

import (
	"sync"

	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
)

type bucketBackends struct {
	// backends maps bucket name to backends on which that bucket exists.
	backends map[string]v1alpha1.Backends
	mu       sync.RWMutex
}

func newBucketBackends() *bucketBackends {
	return &bucketBackends{
		backends: make(map[string]v1alpha1.Backends),
	}
}

func (b *bucketBackends) setBucketCondition(bucketName, backendName string, c xpv1.Condition) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.backends[bucketName] == nil {
		b.backends[bucketName] = make(v1alpha1.Backends)
	}

	if b.backends[bucketName][backendName] == nil {
		b.backends[bucketName][backendName] = &v1alpha1.BackendInfo{}
	}

	b.backends[bucketName][backendName].BucketCondition = c
}

func (b *bucketBackends) setLifecycleConfigCondition(bucketName, backendName string, c xpv1.Condition) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.backends[bucketName] == nil {
		b.backends[bucketName] = make(v1alpha1.Backends)
	}

	if b.backends[bucketName][backendName] == nil {
		b.backends[bucketName][backendName] = &v1alpha1.BackendInfo{}
	}

	b.backends[bucketName][backendName].LifecycleConfigurationCondition = c
}

func (b *bucketBackends) deleteBackend(bucketName, backendName string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.backends[bucketName]; !ok {
		return
	}

	delete(b.backends[bucketName], backendName)
}

func (b *bucketBackends) getBackends(bucketName string, beNames []string) v1alpha1.Backends {
	requestedBackends := map[string]bool{}
	for p := range beNames {
		requestedBackends[beNames[p]] = true
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	be := make(v1alpha1.Backends)
	if _, ok := b.backends[bucketName]; !ok {
		return be
	}

	for k, v := range b.backends[bucketName] {
		if _, ok := requestedBackends[k]; !ok {
			continue
		}

		be[k] = v
	}

	return be
}
