package utils

import (
	"testing"

	v1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/google/go-cmp/cmp"
	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	"github.com/stretchr/testify/assert"
)

func TestMissingStrings(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		sliceA  []string
		sliceB  []string
		missing []string
	}{
		"All strings in sliceA found in sliceB": {
			sliceA: []string{
				"cluster-1",
				"cluster-2",
			},
			sliceB: []string{
				"cluster-1",
				"cluster-2",
				"cluster-3",
			},
			missing: nil,
		},
		"All strings in sliceA missing from sliceB": {
			sliceA: []string{
				"cluster-1",
				"cluster-2",
			},
			sliceB: []string{
				"cluster-3",
				"cluster-4",
				"cluster-5",
			},
			missing: []string{
				"cluster-1",
				"cluster-2",
			},
		},
		"All strings in sliceA missing from empty sliceB": {
			sliceA: []string{
				"cluster-1",
				"cluster-2",
			},
			sliceB: []string{},
			missing: []string{
				"cluster-1",
				"cluster-2",
			},
		},
		"One string in sliceA is missing from sliceB, others are found": {
			sliceA: []string{
				"cluster-1",
				"cluster-2",
				"cluster-3",
			},
			sliceB: []string{
				"cluster-1",
				"cluster-2",
				"cluster-5",
			},
			missing: []string{
				"cluster-3",
			},
		},
	}
	for name, tc := range cases {
		tc := tc
		n := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			missing := MissingStrings(tc.sliceA, tc.sliceB)
			if diff := cmp.Diff(tc.missing, missing); diff != "" {
				t.Errorf("\n%s\nMissingStrings(...): -want, +got:\n%s\n", n, diff)
			}
		})
	}
}

func TestMapConditionToHealthStatus(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		c v1.Condition
		s apisv1alpha1.HealthStatus
	}{
		"HealthCheckSuccess condition": {
			c: v1alpha1.HealthCheckSuccess(),
			s: apisv1alpha1.HealthStatusHealthy,
		},
		"HealthCheckFail condition": {
			c: v1alpha1.HealthCheckFail(),
			s: apisv1alpha1.HealthStatusUnhealthy,
		},
		"HealthCheckDisabled condition": {
			c: v1alpha1.HealthCheckDisabled(),
			s: apisv1alpha1.HealthStatusUnknown,
		},
		"Unavailable condition": {
			c: v1.Unavailable(),
			s: apisv1alpha1.HealthStatusUnknown,
		},
		"Available condition": {
			c: v1.Available(),
			s: apisv1alpha1.HealthStatusUnknown,
		},
	}
	for name, tc := range cases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := MapConditionToHealthStatus(tc.c)
			assert.Equal(t, s, tc.s)
		})
	}
}
