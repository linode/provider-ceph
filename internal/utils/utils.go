package utils

import "k8s.io/utils/strings/slices"

// MissingStrings returns a slice of all strings that exist
// in sliceA, but not in sliceB.
func MissingStrings(sliceA, sliceB []string) []string {
	return slices.Filter(nil, sliceA, func(s string) bool {
		return !slices.Contains(sliceB, s)
	})
}
