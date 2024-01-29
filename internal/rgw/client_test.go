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

package rgw

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

func TestResolveHostBase(t *testing.T) {
	t.Parallel()

	type args struct {
		hostBase string
		useHTTPS bool
	}

	cases := map[string]struct {
		args args
		want string
	}{
		"Use https without prefix": {
			args: args{
				hostBase: "localhost",
				useHTTPS: true,
			},
			want: "https://localhost",
		},
		"Use http without prefix": {
			args: args{
				hostBase: "localhost",
				useHTTPS: false,
			},
			want: "http://localhost",
		},
		"Use https with prefix": {
			args: args{
				hostBase: "http://localhost",
				useHTTPS: true,
			},
			want: "https://localhost",
		},
		"Use http with prefix": {
			args: args{
				hostBase: "http://localhost",
				useHTTPS: false,
			},
			want: "http://localhost",
		},
	}
	for name, tc := range cases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := resolveHostBase(tc.args.hostBase, tc.args.useHTTPS)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("\n%s\nresolveHostBase(...): -want, +got:\n%s\n", tc.want, diff)
			}
		})
	}
}
