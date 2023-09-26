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
	"fmt"

	"github.com/crossplane/crossplane-runtime/pkg/webhook"
	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	"github.com/linode/provider-ceph/internal/utils"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

type BucketValidator struct {
	validator    *webhook.Validator
	backendStore *backendstore.BackendStore
}

func NewBucketValidator(b *backendstore.BackendStore) *BucketValidator {
	bucketValidator := &BucketValidator{}
	validator := webhook.NewValidator()

	validator.CreationChain = append(validator.CreationChain, bucketValidator.ValidateCreate)
	validator.UpdateChain = append(validator.UpdateChain, bucketValidator.ValidateUpdate)
	validator.DeletionChain = append(validator.DeletionChain, bucketValidator.ValidateDelete)

	bucketValidator.validator = validator
	bucketValidator.backendStore = b

	return bucketValidator
}

//+kubebuilder:webhook:path=/validate-provider-ceph-ceph-crossplane-io-v1alpha1-bucket,mutating=false,failurePolicy=fail,sideEffects=None,groups=provider-ceph.ceph.crossplane.io,resources=buckets,verbs=create;update,versions=v1alpha1,name=bucket.providerceph.crossplane.io,admissionReviewVersions=v1

func (b *BucketValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	bucket, ok := obj.(*v1alpha1.Bucket)
	if !ok {
		return nil, errors.New(errNotBucket)
	}

	return nil, b.validateCreateOrUpdate(bucket)
}

func (b *BucketValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	bucket, ok := newObj.(*v1alpha1.Bucket)
	if !ok {
		return nil, errors.New(errNotBucket)
	}

	return nil, b.validateCreateOrUpdate(bucket)
}

func (b *BucketValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func (b *BucketValidator) validateCreateOrUpdate(bucket *v1alpha1.Bucket) error {
	// Ignore validation for health check buckets as they do not
	// behave as 'normal' buckets. For example, health check buckets
	// need to be updated after their owning ProviderConfig has been deleted.
	// This is to remove a finalizer and enable garbage collection.
	if v1alpha1.IsHealthCheckBucket(bucket) {
		return nil
	}

	if len(bucket.Spec.Providers) == 0 {
		return nil
	}

	missingProviders := utils.MissingStrings(bucket.Spec.Providers, b.backendStore.GetAllActiveBackendNames())
	if len(missingProviders) != 0 {
		return errors.New(fmt.Sprintf("providers %v listed in bucket.Spec.Providers cannot be found", missingProviders))
	}

	return nil
}
