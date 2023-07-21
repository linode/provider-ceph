/*
Copyright 2020 The Crossplane Authors.

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

package providerconfig

import (
	"context"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/pkg/errors"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/controller"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/providerconfig"
	"github.com/crossplane/crossplane-runtime/pkg/resource"

	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	s3internal "github.com/linode/provider-ceph/internal/s3"
	"github.com/linode/provider-ceph/internal/utils"
)

const (
	errPutHealthCheckFile = "failed to upload health check file"
	errGetHealthCheckFile = "failed to get health check file"
	errUpdateHealth       = "failed to update health status of provider config"
	healthCheckSuffix     = "-health-check"
	healthCheckFile       = "health-check-file"
	// retryInterval is the interval used when checking
	// for an existing bucket.
	retryInterval        = 5
	healthCheckFinalizer = "health-check.provider-ceph.crossplane.io"
)

func newHealthCheckReconciler(k client.Client, o controller.Options, s *backendstore.BackendStore) *HealthCheckReconciler {
	return &HealthCheckReconciler{
		onceMap:      newOnceMap(),
		kubeClient:   k,
		backendStore: s,
		log:          o.Logger.WithValues("health-check-controller", providerconfig.ControllerName(apisv1alpha1.ProviderConfigGroupKind)),
	}
}

type HealthCheckReconciler struct {
	onceMap      *onceMap
	kubeClient   client.Client
	backendStore *backendstore.BackendStore
	log          logging.Logger
}

//nolint:gocyclo,cyclop // Reconcile functions are inherently complex.
func (r *HealthCheckReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.log.Info("Reconciling health of s3 backend", "name", req.Name)

	// Build the health check bucket from the provider config.
	hcBucket := &v1alpha1.Bucket{}
	hcBucket.SetName(req.Name + healthCheckSuffix)
	hcBucket.SetNamespace(req.Namespace)

	providerConfig := &apisv1alpha1.ProviderConfig{}
	if err := r.kubeClient.Get(ctx, req.NamespacedName, providerConfig); err != nil {
		if kerrors.IsNotFound(err) {
			// ProviderConfig has been deleted, perform cleanup.
			return ctrl.Result{}, r.cleanup(ctx, req, hcBucket)
		}

		return ctrl.Result{}, err
	}

	if providerConfig.Spec.DisableHealthCheck {
		if err := r.cleanup(ctx, req, hcBucket); err != nil {
			return ctrl.Result{}, err
		}

		// Delete the bucket directly as cleanup only removes a finalizer.
		if err := r.kubeClient.Delete(ctx, hcBucket); resource.Ignore(kerrors.IsNotFound, err) != nil {
			return ctrl.Result{}, err
		}

		providerConfig.Status.Health = apisv1alpha1.HealthStatusDisabled
		if err := r.kubeClient.Status().Update(ctx, providerConfig); err != nil {
			return ctrl.Result{}, errors.Wrap(err, errGetHealthCheckFile)
		}

		return ctrl.Result{}, nil
	}

	if err := r.kubeClient.Get(ctx, types.NamespacedName{Namespace: hcBucket.Namespace, Name: hcBucket.Name}, hcBucket); err != nil {
		if kerrors.IsNotFound(err) {
			// No existing health check bucket for this ProviderConfig, create it.
			if err := r.createHealthCheckBucket(ctx, providerConfig, hcBucket); err != nil {
				r.log.Info("Failed to create bucket for health check on s3 backend", "name", providerConfig.Name)

				return ctrl.Result{}, err
			}

			r.log.Info("Failed to get bucket for health check on s3 backend", "name", providerConfig.Name)
		}
	}

	var err error
	// Perform an initial check once on each provider config for the health check bucket.
	// This is done because the backend store reconciler and bucket controller need to
	// complete before we can write to the health check bucket.
	r.onceMap.addEntryWithOnce(req.Name).Do(func() {
		err = r.bucketExistsRetry(ctx, providerConfig.Name, hcBucket.Name)
	})
	if err != nil {
		r.onceMap.deleteEntry(providerConfig.Name)

		return ctrl.Result{}, err
	}

	if err := r.doHealthCheck(ctx, providerConfig, hcBucket); err != nil {
		return ctrl.Result{}, err
	}

	// health check interval is 30s by default.
	interval := time.Duration(providerConfig.Spec.HealthCheckIntervalSeconds) * time.Second

	return ctrl.Result{RequeueAfter: interval}, nil
}

func (r *HealthCheckReconciler) cleanup(ctx context.Context, req ctrl.Request, hcBucket *v1alpha1.Bucket) error {
	// The ProviderConfig representing an s3 backend has been deleted,
	// therefore we need to:
	// 1. Delete the ProviderConfig's entry in the reconciler's onceMap.
	r.onceMap.deleteEntry(req.Name)
	// 2. Delete the health check bucket from the s3 backend.
	backendClient := r.backendStore.GetBackendClient(req.Name)
	if backendClient != nil {
		r.log.Info("Deleting health check bucket", "name", hcBucket.Name)
		if err := s3internal.DeleteBucket(ctx, backendClient, aws.String(hcBucket.Name)); err != nil {
			return err
		}
	}
	// 3. Get the latest version of the health check bucket (in order to
	// complete 4).
	if err := r.kubeClient.Get(ctx, types.NamespacedName{Namespace: hcBucket.Namespace, Name: hcBucket.Name}, hcBucket); err != nil {
		return resource.Ignore(kerrors.IsNotFound, err)
	}
	// 4. Remove the bucket's finalizers so that it can be garbage collected. These are the healthcheck
	// finalizer added at creation time, and the managed-resource finalizer added by crossplane.
	// 'Normal' buckets are given the managed-resource finalizer in order to prevent the associated
	// provider config from being deleted whilst it is still in use. However, health check buckets are
	// treated differently as they are owned by the provider config.
	finalizers := utils.RemoveStringFromSlice(hcBucket.GetFinalizers(), managed.FinalizerName)
	finalizers = utils.RemoveStringFromSlice(finalizers, healthCheckFinalizer)
	hcBucket.SetFinalizers(finalizers)

	return r.kubeClient.Update(ctx, hcBucket)
}

func (r *HealthCheckReconciler) createHealthCheckBucket(ctx context.Context, providerConfig *apisv1alpha1.ProviderConfig, hcBucket *v1alpha1.Bucket) error {
	r.log.Info("Creating bucket for health check on s3 backend", "name", providerConfig.Name)
	// Add the ProviderConfig to the Bucket's owner reference for garbage collection.
	ownerRef := metav1.OwnerReference{
		APIVersion: providerConfig.APIVersion,
		Kind:       providerConfig.Kind,
		Name:       providerConfig.Name,
		UID:        providerConfig.UID,
	}
	hcBucket.SetOwnerReferences([]metav1.OwnerReference{ownerRef})
	// Give the health check bucket a finalizer so that it cannot be deleted mistakenly.
	hcBucket.SetFinalizers([]string{healthCheckFinalizer})
	// Set the ProviderConfigReference so that the bucket is created on the correct backend.
	hcBucket.Spec.ProviderConfigReference = &commonv1.Reference{Name: providerConfig.Name}
	// Add health-check label so that bucket controller knows to ignore Update/Delete calls.
	bucketLabels := make(map[string]string)
	bucketLabels[s3internal.HealthCheckLabelKey] = s3internal.HealthCheckLabelVal
	hcBucket.SetLabels(bucketLabels)

	return r.kubeClient.Create(ctx, hcBucket)
}

func (r *HealthCheckReconciler) doHealthCheck(ctx context.Context, providerConfig *apisv1alpha1.ProviderConfig, hcBucket *v1alpha1.Bucket) error {
	s3BackendClient := r.backendStore.GetBackendClient(providerConfig.Name)
	if s3BackendClient == nil {
		return errors.New(errBackendNotStored)
	}

	// Assume the status is Unhealthy until we can verify otherwise.
	providerConfig.Status.Health = apisv1alpha1.HealthStatusUnhealthy

	_, putErr := s3BackendClient.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(hcBucket.Name),
		Key:    aws.String(healthCheckFile),
		Body:   strings.NewReader(time.Now().Format(time.RFC850)),
	})
	if putErr != nil {
		if err := r.kubeClient.Status().Update(ctx, providerConfig); err != nil {
			return errors.Wrap(err, putErr.Error())
		}

		return errors.Wrap(putErr, errPutHealthCheckFile)
	}

	_, getErr := s3BackendClient.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(hcBucket.Name),
		Key:    aws.String(healthCheckFile),
	})
	if getErr != nil {
		if err := r.kubeClient.Status().Update(ctx, providerConfig); err != nil {
			return errors.Wrap(err, getErr.Error())
		}

		return errors.Wrap(getErr, errGetHealthCheckFile)
	}

	// Health check completed successfully, update status.
	providerConfig.Status.Health = apisv1alpha1.HealthStatusHealthy

	return r.kubeClient.Status().Update(ctx, providerConfig)
}

func (r *HealthCheckReconciler) bucketExistsRetry(ctx context.Context, s3BackendName, bucketName string) error {
	if err := r.bucketExists(ctx, s3BackendName, bucketName); err == nil {
		return nil
	}

	ticker := time.NewTicker(retryInterval * time.Second)
	var errStr string
	for {
		select {
		case <-ticker.C:
			if err := r.bucketExists(ctx, s3BackendName, bucketName); err != nil {
				errStr = err.Error()

				continue
			}

			return nil

		case <-ctx.Done():
			// Wrap the ctx done error with the last received error
			// from above.
			return errors.Wrap(ctx.Err(), errStr)
		}
	}
}

func (r *HealthCheckReconciler) bucketExists(ctx context.Context, s3BackendName, bucketName string) error {
	s3BackendClient := r.backendStore.GetBackendClient(s3BackendName)
	if s3BackendClient == nil {
		return errors.New(errBackendNotStored)
	}
	_, err := s3BackendClient.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(bucketName)})
	if err != nil {
		return err
	}

	return nil
}

func (r *HealthCheckReconciler) setupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&apisv1alpha1.ProviderConfig{}).
		Complete(r)
}
