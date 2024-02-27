package utils

import (
	commonv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	"k8s.io/utils/strings/slices"
)

// MissingStrings returns a slice of all strings that exist
// in sliceA, but not in sliceB.
func MissingStrings(sliceA, sliceB []string) []string {
	return slices.Filter(nil, sliceA, func(s string) bool {
		return !slices.Contains(sliceB, s)
	})
}

// MapConditionToHealthStatus takes a crossplane condition and returns the
// corresponding health status, returning Unknown if the condition does not
// map to any health status.
func MapConditionToHealthStatus(condition commonv1.Condition) apisv1alpha1.HealthStatus {
	if condition.Equal(v1alpha1.HealthCheckSuccess()) {
		return apisv1alpha1.HealthStatusHealthy
	} else if condition.Equal(v1alpha1.HealthCheckFail()) {
		return apisv1alpha1.HealthStatusUnhealthy
	}

	return apisv1alpha1.HealthStatusUnknown
}

// GetBucketProvidersFilterDisabledLabel returns the specified providers or default providers,
// and filters out providers disabledby label.
func GetBucketProvidersFilterDisabledLabel(bucket *v1alpha1.Bucket, backends []string) []string {
	providers := bucket.Spec.Providers
	if len(providers) == 0 {
		providers = backends
	}

	okProviders := []string{}
	for i := range providers {
		if status, ok := bucket.Labels[GetBackendLabel(providers[i])]; ok && status != "true" {
			continue
		}

		okProviders = append(okProviders, providers[i])
	}

	return okProviders
}

// GetBackendLabel renders label key for provider.
func GetBackendLabel(provider string) string {
	return v1alpha1.BackendLabelPrefix + provider
}
