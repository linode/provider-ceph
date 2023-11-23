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
	"go.opentelemetry.io/otel"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane/crossplane-runtime/pkg/controller"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/providerconfig"
	"github.com/crossplane/crossplane-runtime/pkg/resource"

	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	"github.com/linode/provider-ceph/internal/otel/traces"
	s3internal "github.com/linode/provider-ceph/internal/s3"
)

const (
	errPutHealthCheckFile = "failed to upload health check file"
	errGetHealthCheckFile = "failed to get health check file"
	errUpdateHealth       = "failed to update health status of provider config"
	healthCheckSuffix     = "-health-check"
	healthCheckFile       = "health-check-file"
)

func newHealthCheckReconciler(k client.Client, o controller.Options, s *backendstore.BackendStore, a bool) *HealthCheckReconciler {
	return &HealthCheckReconciler{
		kubeClient:      k,
		backendStore:    s,
		log:             o.Logger.WithValues("health-check-controller", providerconfig.ControllerName(apisv1alpha1.ProviderConfigGroupKind)),
		autoPauseBucket: a,
	}
}

type HealthCheckReconciler struct {
	kubeClient      client.Client
	backendStore    *backendstore.BackendStore
	log             logging.Logger
	autoPauseBucket bool
}

func (r *HealthCheckReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	ctx, span := otel.Tracer("").Start(ctx, "HealthCheckReconciler")
	defer span.End()

	r.log.Info("Reconciling health of s3 backend", "backend_name", req.Name)

	bucketName := req.Name + healthCheckSuffix

	providerConfig := &apisv1alpha1.ProviderConfig{}
	if err = r.kubeClient.Get(ctx, req.NamespacedName, providerConfig); err != nil {
		if kerrors.IsNotFound(err) {
			// ProviderConfig has been deleted, perform cleanup.
			return ctrl.Result{}, r.cleanup(ctx, req, bucketName)
		}

		return
	}

	if providerConfig.Spec.DisableHealthCheck {
		r.log.Info("Health check is disabled for s3 backend", "backend_name", providerConfig.Name)

		r.backendStore.SetBackendHealthStatus(req.Name, apisv1alpha1.HealthStatusUnknown)

		if updateErr := UpdateProviderConfigStatus(ctx, r.kubeClient, providerConfig, func(_, pcLatest *apisv1alpha1.ProviderConfig) {
			pcLatest.Status.Health = apisv1alpha1.HealthStatusUnknown
		}); updateErr != nil {
			err = errors.Wrap(updateErr, errUpdateHealth)
			traces.SetAndRecordError(span, err)
		}

		return
	}

	// Store the health status before the check so that we can compare
	// with the health status after the check.
	healthBeforeCheck := providerConfig.Status.Health

	// Assume the status is Unhealthy until we can verify otherwise.
	providerConfig.Status.Health = apisv1alpha1.HealthStatusUnhealthy
	providerConfig.Status.Reason = ""
	defer func() {
		r.backendStore.SetBackendHealthStatus(req.Name, providerConfig.Status.Health)

		if updateErr := UpdateProviderConfigStatus(ctx, r.kubeClient, providerConfig, func(pcDeepCopy, pcLatest *apisv1alpha1.ProviderConfig) {
			pcLatest.Status.Health = pcDeepCopy.Status.Health
			pcLatest.Status.Reason = pcDeepCopy.Status.Reason
		}); updateErr != nil {
			err = errors.Wrap(updateErr, err.Error())
		}
	}()

	// Create a health check bucket on the backend if one does not already exist.
	if err = r.bucketExists(ctx, req.Name, bucketName); err != nil {
		if err = r.createBucket(ctx, req.Name, bucketName); err != nil {
			r.log.Info("Failed to create bucket for health check on s3 backend", "bucket_name", bucketName, "backend_name", providerConfig.Name)

			providerConfig.Status.Reason = fmt.Sprintf("failed to create health check bucket: %v", err.Error())

			return
		}
	}

	// Perform the health check. By calling this function, we are implicitly updating
	// the health status of the ProviderConfig with whatever the health check reports.
	if err = r.doHealthCheck(ctx, providerConfig, bucketName); err != nil {
		traces.SetAndRecordError(span, err)
		r.log.Info("Failed to do health check on s3 backend", "bucket_name", bucketName, "backend_name", providerConfig.Name)

		providerConfig.Status.Reason = fmt.Sprintf("failed to do health check: %v", err.Error())

		return
	}

	// Check if the backend is no longer healthy. In which case, we need to unpause all
	// Bucket CRs that have buckets stored on this backend. We do this to allow these
	// Bucket CRs be reconciled again.
	healthAfterCheck := providerConfig.Status.Health
	if healthBeforeCheck == apisv1alpha1.HealthStatusHealthy && healthBeforeCheck != healthAfterCheck {
		r.log.Info("Backend is no longer healthy - unpausing all Buckets on backend to allow Observation", "backend_name", providerConfig.Name)
		go r.unpauseBuckets(ctx, providerConfig.Name)
	}

	// Health check interval is 30s by default.
	// It is safe to requeue after the same object multiple times,
	// because controller runtime reconcilies only once.
	res = ctrl.Result{
		RequeueAfter: time.Duration(providerConfig.Spec.HealthCheckIntervalSeconds) * time.Second,
	}

	return
}

// cleanup deletes the health check bucket and the lifecycle configuration validation bucket
// from the backend. This function is only called when a ProviderConfig has been deleted.
func (r *HealthCheckReconciler) cleanup(ctx context.Context, req ctrl.Request, bucketName string) error {
	backendClient := r.backendStore.GetBackendClient(req.Name)
	if backendClient == nil {
		r.log.Info("Backend client not found during health check bucket cleanup - aborting cleanup", "backend_name", req.Name)

		return nil
	}

	r.log.Info("Deleting health check bucket", "bucket_name", bucketName, "backend_name", req.Name)

	if err := s3internal.DeleteBucket(ctx, backendClient, aws.String(bucketName)); err != nil {
		return err
	}

	r.log.Info("Deleting lifecycle configuration validation bucket", "bucket_name", v1alpha1.LifecycleConfigValidationBucketName, "backend_name", req.Name)

	return s3internal.DeleteBucket(ctx, backendClient, aws.String(v1alpha1.LifecycleConfigValidationBucketName))
}

// doHealthCheck performs a PutObject and GetObject on the health check bucket on the backend.
func (r *HealthCheckReconciler) doHealthCheck(ctx context.Context, providerConfig *apisv1alpha1.ProviderConfig, bucketName string) error {
	ctx, span := otel.Tracer("").Start(ctx, "HealthCheckReconciler.doHealthCheck")
	defer span.End()

	s3BackendClient := r.backendStore.GetBackendClient(providerConfig.Name)
	if s3BackendClient == nil {
		err := errors.New(errBackendNotStored)
		traces.SetAndRecordError(span, err)

		return err
	}

	_, putErr := s3BackendClient.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(healthCheckFile),
		Body:   strings.NewReader(time.Now().Format(time.RFC850)),
	})
	if putErr != nil {
		return errors.Wrap(putErr, errPutHealthCheckFile)
	}

	_, getErr := s3BackendClient.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucketName),
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

func (r *HealthCheckReconciler) setupWithManager(mgr ctrl.Manager) error {
	const maxReconciles = 5

	return ctrl.NewControllerManagedBy(mgr).
		For(&apisv1alpha1.ProviderConfig{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: maxReconciles,
		}.ForControllerRuntime()).
		Complete(r)
}

// unpauseBuckets lists all buckets that exist on the given backend by using the custom
// backend label. Then, using retry.OnError(), it attempts to unpause each of these buckets
// by unsetting the Pause label.
func (r *HealthCheckReconciler) unpauseBuckets(ctx context.Context, s3BackendName string) {
	const (
		steps    = 4
		duration = time.Second
		factor   = 5
		jitter   = 0.1
	)

	buckets := &v1alpha1.BucketList{}
	beLabel := v1alpha1.BackendLabelPrefix + s3BackendName
	hasBackendName := client.HasLabels{beLabel}
	err := retry.OnError(wait.Backoff{
		Steps:    steps,
		Duration: duration,
		Factor:   factor,
		Jitter:   jitter,
	}, resource.IsAPIError, func() error {
		return r.kubeClient.List(ctx, buckets, hasBackendName)
	})
	if err != nil {
		r.log.Info("Error attempting to list Buckets on backend", "error", err.Error(), "backend_name", s3BackendName)

		return
	}

	for i := range buckets.Items {
		i := i
		r.log.Debug("Attempting to unpause bucket", "bucket_name", buckets.Items[i].Name)
		err := retry.OnError(wait.Backoff{
			Steps:    steps,
			Duration: duration,
			Factor:   factor,
			Jitter:   jitter,
		}, resource.IsAPIError, func() error {
			if (r.autoPauseBucket || buckets.Items[i].Spec.AutoPause) &&
				buckets.Items[i].Labels[meta.AnnotationKeyReconciliationPaused] == "true" {
				buckets.Items[i].Labels[meta.AnnotationKeyReconciliationPaused] = ""

				return r.kubeClient.Update(ctx, &buckets.Items[i])
			}

			return nil
		})

		if err != nil {
			r.log.Info("Error attempting to unpause bucket", "error", err.Error(), "bucket", buckets.Items[i].Name)
		}
	}
}
