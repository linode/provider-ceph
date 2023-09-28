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
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/pkg/errors"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	commonv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/controller"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/providerconfig"
	"github.com/crossplane/crossplane-runtime/pkg/resource"

	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	s3internal "github.com/linode/provider-ceph/internal/s3"
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
		kubeClient:   k,
		backendStore: s,
		log:          o.Logger.WithValues("health-check-controller", providerconfig.ControllerName(apisv1alpha1.ProviderConfigGroupKind)),
	}
}

type HealthCheckReconciler struct {
	kubeClient   client.Client
	backendStore *backendstore.BackendStore
	log          logging.Logger
}

func (r *HealthCheckReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	r.log.Info("Reconciling health of s3 backend", "name", req.Name)

	// Build the health check bucket from the provider config.
	hcBucket := &v1alpha1.Bucket{}
	hcBucket.SetName(req.Name + healthCheckSuffix)
	hcBucket.SetNamespace(req.Namespace)
	hcBucket.Spec.Providers = []string{req.Name}

	providerConfig := &apisv1alpha1.ProviderConfig{}
	if err = r.kubeClient.Get(ctx, req.NamespacedName, providerConfig); err != nil {
		if kerrors.IsNotFound(err) {
			// ProviderConfig has been deleted, perform cleanup.
			return ctrl.Result{}, r.cleanup(ctx, req, hcBucket)
		}

		return
	}

	if providerConfig.Spec.DisableHealthCheck {
		r.log.Info("Health check is disabled for s3 backend", "name", req.Name)

		r.backendStore.SetBackendHealthStatus(req.Name, apisv1alpha1.HealthStatusUnknown)

		if updateErr := r.updateConfigStatus(ctx, providerConfig, func(_, pc *apisv1alpha1.ProviderConfig) {
			pc.Status.Health = apisv1alpha1.HealthStatusUnknown
		}); updateErr != nil {
			err = errors.Wrap(updateErr, errUpdateHealth)
		}

		return
	}

	// Assume the status is Unhealthy until we can verify otherwise.
	providerConfig.Status.Health = apisv1alpha1.HealthStatusUnhealthy
	defer func() {
		r.backendStore.SetBackendHealthStatus(req.Name, providerConfig.Status.Health)

		if updateErr := r.updateConfigStatus(ctx, providerConfig, func(orig, pc *apisv1alpha1.ProviderConfig) {
			pc.Status.Health = orig.Status.Health
		}); updateErr != nil {
			err = errors.Wrap(updateErr, err.Error())
		}
	}()

	if err = r.kubeClient.Get(ctx, types.NamespacedName{Namespace: hcBucket.Namespace, Name: hcBucket.Name}, hcBucket); err != nil {
		if !kerrors.IsNotFound(err) {
			r.log.Info("Failed to get bucket for health check on s3 backend", "name", providerConfig.Name, "backend", req.Name)

			return
		}

		// No existing health check bucket for this ProviderConfig, create it.
		if err = r.createHealthCheckBucket(ctx, providerConfig, hcBucket); err != nil {
			r.log.Info("Failed to create bucket resource for health check on s3 backend", "name", providerConfig.Name, "backend", req.Name)

			return
		}
	}

	if err = r.bucketExists(ctx, req.Name, hcBucket.Name); err != nil {
		if err = r.createBucket(ctx, req.Name, hcBucket.Name); err != nil {
			r.log.Info("Failed to create bucket for health check on s3 backend", "name", providerConfig.Name, "backend", req.Name)

			return
		}
	}

	if err = r.doHealthCheck(ctx, providerConfig, hcBucket); err != nil {
		return
	}

	// health check interval is 30s by default.
	res = ctrl.Result{
		RequeueAfter: time.Duration(providerConfig.Spec.HealthCheckIntervalSeconds) * time.Second,
	}

	return
}

func (r *HealthCheckReconciler) cleanup(ctx context.Context, req ctrl.Request, hcBucket *v1alpha1.Bucket) error {
	// The ProviderConfig representing an s3 backend has been deleted,
	// therefore we need to:
	// 1. delete the health check bucket from the s3 backend.
	backendClient := r.backendStore.GetBackendClient(req.Name)
	if backendClient != nil {
		r.log.Info("Deleting health check bucket", "name", hcBucket.Name)
		if err := s3internal.DeleteBucket(ctx, backendClient, aws.String(hcBucket.Name)); err != nil {
			return err
		}
	}
	// 2. Get the latest version of the health check bucket (in order to
	// complete 3).
	if err := r.kubeClient.Get(ctx, types.NamespacedName{Namespace: hcBucket.Namespace, Name: hcBucket.Name}, hcBucket); err != nil {
		return resource.Ignore(kerrors.IsNotFound, err)
	}
	// 3. Remove the bucket's finalizers so that it can be garbage collected. These are the healthcheck
	// finalizer added at creation time, and the managed-resource finalizer added by crossplane.
	// 'Normal' buckets are given the managed-resource finalizer in order to prevent the associated
	// provider config from being deleted whilst it is still in use. However, health check buckets are
	// treated differently as they are owned by the provider config.
	controllerutil.RemoveFinalizer(hcBucket, managed.FinalizerName)
	controllerutil.RemoveFinalizer(hcBucket, healthCheckFinalizer)

	hcBucket.Annotations[meta.AnnotationKeyReconciliationPaused] = "false"

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
	bucketLabels[v1alpha1.HealthCheckLabelKey] = v1alpha1.HealthCheckLabelVal
	hcBucket.SetLabels(bucketLabels)
	hcBucket.SetAnnotations(map[string]string{meta.AnnotationKeyReconciliationPaused: "true"})

	return r.kubeClient.Create(ctx, hcBucket)
}

func (r *HealthCheckReconciler) doHealthCheck(ctx context.Context, providerConfig *apisv1alpha1.ProviderConfig, hcBucket *v1alpha1.Bucket) error {
	s3BackendClient := r.backendStore.GetBackendClient(providerConfig.Name)
	if s3BackendClient == nil {
		return errors.New(errBackendNotStored)
	}

	_, putErr := s3BackendClient.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(hcBucket.Name),
		Key:    aws.String(healthCheckFile),
		Body:   strings.NewReader(time.Now().Format(time.RFC850)),
	})
	if putErr != nil {
		return errors.Wrap(putErr, errPutHealthCheckFile)
	}

	_, getErr := s3BackendClient.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(hcBucket.Name),
		Key:    aws.String(healthCheckFile),
	})
	if getErr != nil {
		return errors.Wrap(getErr, errGetHealthCheckFile)
	}

	// Health check completed successfully, update status.
	providerConfig.Status.Health = apisv1alpha1.HealthStatusHealthy

	return nil
}

func (r *HealthCheckReconciler) bucketExists(ctx context.Context, s3BackendName, bucketName string) error {
	s3BackendClient := r.backendStore.GetBackendClient(s3BackendName)
	if s3BackendClient == nil {
		return errors.New(errBackendNotStored)
	}

	_, err := s3BackendClient.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(bucketName)})

	return err
}

func (r *HealthCheckReconciler) createBucket(ctx context.Context, s3BackendName, bucketName string) error {
	s3BackendClient := r.backendStore.GetBackendClient(s3BackendName)
	if s3BackendClient == nil {
		return errors.New(errBackendNotStored)
	}

	_, err := s3BackendClient.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucketName),
	})

	return resource.Ignore(s3internal.IsAlreadyExists, err)
}

// Callbacks have two parameters, first config is the original, the second is the new version of config.
func (r *HealthCheckReconciler) updateConfigStatus(ctx context.Context, pc *apisv1alpha1.ProviderConfig, callbacks ...func(*apisv1alpha1.ProviderConfig, *apisv1alpha1.ProviderConfig)) error {
	origPC := pc.DeepCopy()

	nn := types.NamespacedName{Name: pc.GetName(), Namespace: pc.Namespace}

	const (
		steps  = 4
		divide = 2
		factor = 0.5
		jitter = 0.1
	)

	for _, cb := range callbacks {
		err := retry.OnError(wait.Backoff{
			Steps:    steps,
			Duration: (time.Duration(pc.Spec.HealthCheckIntervalSeconds) * time.Second) - time.Second,
			Factor:   factor,
			Jitter:   jitter,
		}, resource.IsAPIError, func() error {
			if err := r.kubeClient.Get(ctx, nn, pc); err != nil {
				return err
			}

			cb(origPC, pc)

			return r.kubeClient.Status().Update(ctx, pc)
		})

		if err != nil {
			if kerrors.IsNotFound(err) {
				break
			}

			return fmt.Errorf("unable to update object: %w", err)
		}
	}

	return nil
}

func (r *HealthCheckReconciler) setupWithManager(mgr ctrl.Manager) error {
	const maxReconciles = 5

	return ctrl.NewControllerManagedBy(mgr).
		For(&apisv1alpha1.ProviderConfig{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: maxReconciles,
		}.ForControllerRuntime()).
		Complete(r)
}
