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

import "github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"

const (
	HealthCheckLabelKey = "provider-ceph.crossplane.io"
	HealthCheckLabelVal = "health-check-bucket"
)

func RemoveStringFromSlice(slice []string, str string) []string {
	updated := make([]string, 0)
	for _, s := range slice {
		if s != str {
			updated = append(updated, s)
		}
	}

	return updated
}

func IsHealthCheckBucket(bucket *v1alpha1.Bucket) bool {
	if val, ok := bucket.GetLabels()[HealthCheckLabelKey]; ok {
		if val == HealthCheckLabelVal {
			return true
		}
	}

	return false
}
