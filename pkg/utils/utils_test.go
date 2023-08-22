/*
Copyright 2022 The Crossplane Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package utils

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

// Unlike many Kubernetes projects Crossplane does not use third party testing
// libraries, per the common Go test review comments. Crossplane encourages the
// use of table driven unit tests. The tests of the crossplane-runtime project
// are representative of the testing style Crossplane encourages.
//
// https://github.com/golang/go/wiki/TestComments
// https://github.com/crossplane/crossplane/blob/master/CONTRIBUTING.md#contributing-code

func TestRemoveStringFromSlice(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		slice []string
		str   string
		want  []string
	}{
		"Remove single string": {
			slice: []string{"abc", "def", "ghi"},
			str:   "abc",
			want:  []string{"def", "ghi"},
		},
		"Remove multiple strings": {
			slice: []string{"abc", "def", "abc", "ghi", "abc", "jkl", "abc"},
			str:   "abc",
			want:  []string{"def", "ghi", "jkl"},
		},
		"String does not exist": {
			slice: []string{"abc", "def", "abc", "ghi", "abc", "jkl"},
			str:   "xyz",
			want:  []string{"abc", "def", "abc", "ghi", "abc", "jkl"},
		},
		"Single entry found": {
			slice: []string{"abc"},
			str:   "abc",
			want:  []string{},
		},
		"Empty slice": {
			slice: []string{},
			str:   "abc",
			want:  []string{},
		},
		"Nil slice": {
			slice: nil,
			str:   "abc",
			want:  []string{},
		},
	}

	for name, tc := range cases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := RemoveStringFromSlice(tc.slice, tc.str)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("\n%s\nRemoveStringFromSlice(...): -want, +got:\n%s\n", tc.want, diff)
			}
		})
	}
}
