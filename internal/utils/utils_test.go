package utils

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestMissingStrings(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		sliceA  []string
		sliceB  []string
		missing []string
	}{
		"All spec.sliceA found": {
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
		"No spec.sliceA found": {
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
		"No ProviderConfigs exist": {
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
		"Some spec.sliceA found": {
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
				t.Errorf("\n%s\nmissing(...): -want, +got:\n%s\n", n, diff)
			}
		})
	}
}
