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

	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	"github.com/linode/provider-ceph/internal/rgw"
	"github.com/linode/provider-ceph/internal/utils"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const errValidatingLifecycleConfig = "unable to validate lifecycle configuration"

type BucketValidator struct {
	backendStore *backendstore.BackendStore
}

func NewBucketValidator(b *backendstore.BackendStore) *BucketValidator {
	bucketValidator := &BucketValidator{
		backendStore: b,
	}

	return bucketValidator
}

//+kubebuilder:webhook:path=/validate-provider-ceph-ceph-crossplane-io-v1alpha1-bucket,mutating=false,failurePolicy=fail,sideEffects=None,groups=provider-ceph.ceph.crossplane.io,resources=buckets,verbs=create;update,versions=v1alpha1,name=bucket-validation.providerceph.crossplane.io,admissionReviewVersions=v1

func (b *BucketValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	bucket, ok := obj.(*v1alpha1.Bucket)
	if !ok {
		return nil, errors.New(errNotBucket)
	}

	return nil, b.validateCreateOrUpdate(ctx, bucket)
}

func (b *BucketValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	bucket, ok := newObj.(*v1alpha1.Bucket)
	if !ok {
		return nil, errors.New(errNotBucket)
	}

	if bucket.ObjectMeta.DeletionTimestamp != nil {
		return nil, nil
	}

	return nil, b.validateCreateOrUpdate(ctx, bucket)
}

func (b *BucketValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func (b *BucketValidator) validateCreateOrUpdate(ctx context.Context, bucket *v1alpha1.Bucket) error {
	if len(bucket.Spec.Providers) != 0 {
		missingProviders := utils.MissingStrings(bucket.Spec.Providers, b.backendStore.GetAllBackendNames())
		if len(missingProviders) != 0 {
			return errors.New(fmt.Sprintf("providers %v listed in bucket.Spec.Providers cannot be found", missingProviders))
		}
	}

	if !bucket.Spec.LifecycleConfigurationDisabled && bucket.Spec.ForProvider.LifecycleConfiguration != nil {
		if err := b.validateLifecycleConfiguration(ctx, bucket); err != nil {
			return errors.Wrap(err, errValidatingLifecycleConfig)
		}
	}

	return nil
}

func (b *BucketValidator) validateLifecycleConfiguration(ctx context.Context, bucket *v1alpha1.Bucket) error {
	s3Client := b.backendStore.GetAllBackends().GetFirst()
	if s3Client == nil {
		return errors.New(errNoS3BackendsStored)
	}

	dummyBucket := &v1alpha1.Bucket{}
	validationBucketName := v1alpha1.LifecycleConfigValidationBucketName
	dummyBucket.SetName(validationBucketName)

	// Create dummy bucket 'life-cycle-configuration-validation-bucket' for the lifecycle config validation.
	// Cleanup of this bucket is performed by the health-check controller on deletion of the ProviderConfig.
	_, err := rgw.CreateBucket(ctx, s3Client, rgw.BucketToCreateBucketInput(dummyBucket))
	if err != nil {
		return err
	}

	// Attempt to Put the lifecycle config.
	dummyBucket.Spec.ForProvider.LifecycleConfiguration = bucket.Spec.ForProvider.LifecycleConfiguration
	_, err = rgw.PutBucketLifecycleConfiguration(ctx, s3Client, dummyBucket)
	if err != nil {
		return err
	}

	return nil
}
