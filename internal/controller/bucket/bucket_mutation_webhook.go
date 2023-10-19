/*
Copyright 2023.

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

package bucket

import (
	"context"

	"github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime"
)

type BucketMutator struct{}

func NewBucketMutator() *BucketMutator {
	return &BucketMutator{}
}

//+kubebuilder:webhook:path=/mutate-provider-ceph-ceph-crossplane-io-v1alpha1-bucket,mutating=true,failurePolicy=fail,sideEffects=None,groups=provider-ceph.ceph.crossplane.io,resources=buckets,verbs=update,versions=v1alpha1,name=bucket-mutation.providerceph.crossplane.io,admissionReviewVersions=v1

func (b *BucketMutator) Default(ctx context.Context, obj runtime.Object) error {
	bucket, ok := obj.(*v1alpha1.Bucket)
	if !ok {
		return errors.New(errNotBucket)
	}

	if bucket.Labels == nil {
		bucket.Labels = map[string]string{}
	}

	bucket.Labels[meta.AnnotationKeyReconciliationPaused] = bucket.Annotations[meta.AnnotationKeyReconciliationPaused]

	return nil
}
