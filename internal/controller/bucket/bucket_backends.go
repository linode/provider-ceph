package bucket

import (
	"sync"

	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"

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

func (b *bucketBackends) getBucketCondition(bucketName, backendName string) *xpv1.Condition {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if _, ok := b.backends[bucketName]; !ok {
		return nil
	}

	if _, ok := b.backends[bucketName][backendName]; !ok {
		return nil
	}

	return &b.backends[bucketName][backendName].BucketCondition
}

func (b *bucketBackends) setLifecycleConfigCondition(bucketName, backendName string, c *xpv1.Condition) {
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

func (b *bucketBackends) getLifecycleConfigCondition(bucketName, backendName string) *xpv1.Condition {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if _, ok := b.backends[bucketName]; !ok {
		return nil
	}

	if _, ok := b.backends[bucketName][backendName]; !ok {
		return nil
	}

	return b.backends[bucketName][backendName].LifecycleConfigurationCondition
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

// countBucketsAvailableOnBackends counts the backends listed in providerNames.
func (b *bucketBackends) countBucketsAvailableOnBackends(bucket *v1alpha1.Bucket, providerNames []string, c map[string]backendstore.S3Client) uint {
	i := uint(0)
	for _, backendName := range providerNames {
		if _, ok := c[backendName]; !ok {
			// This backend does not exist in the list of available backends.
			// The backend may be offline, so it is skipped.
			continue
		}

		bucketCondition := b.getBucketCondition(bucket.Name, backendName)
		if bucketCondition == nil {
			// The bucket has not been created on this backend.
			continue
		}

		if !bucketCondition.Equal(xpv1.Available()) {
			// The bucket is not Available on this backend.
			continue
		}

		i++
	}

	return i
}

// isLifecycleConfigAvailableOnBackends checks the backends listed in Spec.Providers against
// bucketBackends to ensure lifecycle configurations are considered Available on all desired backends.
func (b *bucketBackends) isLifecycleConfigAvailableOnBackends(bucket *v1alpha1.Bucket, c map[string]backendstore.S3Client) bool {
	for _, backendName := range bucket.Spec.Providers {
		if _, ok := c[backendName]; !ok {
			// This backend does not exist in the list of available backends.
			// The backend may be offline, so it is skipped.
			continue
		}

		lcCondition := b.getLifecycleConfigCondition(bucket.Name, backendName)
		if lcCondition == nil {
			// The lifecycleconfig has not been created on this backend.
			return false
		}

		if !lcCondition.Equal(xpv1.Available()) {
			// The lifecycleconfig is not Available on this backend.
			return false
		}
	}

	return true
}

// isLifecycleConfigRemovedFromBackends checks the backends listed in Spec.Providers against
// bucketBackends to ensure lifecycle configurations are removed from all desired backends.
func (b *bucketBackends) isLifecycleConfigRemovedFromBackends(bucket *v1alpha1.Bucket, c map[string]backendstore.S3Client) bool {
	for _, backendName := range bucket.Spec.Providers {
		if _, ok := c[backendName]; !ok {
			// This backend does not exist in the list of available backends.
			// The backend may be offline, so it is skipped.
			continue
		}

		lcCondition := b.getLifecycleConfigCondition(bucket.Name, backendName)
		if lcCondition != nil {
			// The lifecycleconfig has not been created on this backend.
			return false
		}
	}

	return true
}
