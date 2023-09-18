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

package v1alpha1

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

//+kubebuilder:webhook:path=/validate-provider-ceph-crossplane-io-v1alpha1-bucket,mutating=false,failurePolicy=fail,sideEffects=None,groups=provider-ceph.ceph.crossplane.io,resources=buckets,verbs=create;update,versions=v1alpha1,name=bucket.providerceph.crossplane.io,admissionReviewVersions=v1

func ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	warnings := []string{}
	return warnings, nil
}

func ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	warnings := []string{}
	return warnings, nil
}

func ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	warnings := []string{}
	return warnings, nil
}

func init() {
	BucketValidator.CreationChain = append(BucketValidator.CreationChain, ValidateCreate)
	BucketValidator.UpdateChain = append(BucketValidator.UpdateChain, ValidateUpdate)
	BucketValidator.DeletionChain = append(BucketValidator.DeletionChain, ValidateDelete)
}
