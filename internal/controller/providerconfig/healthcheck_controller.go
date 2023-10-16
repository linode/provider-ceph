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
	"k8s.io/apimachinery/pkg/types"
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
	r.log.Info("Reconciling health of s3 backend", "name", req.Name)

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
		r.log.Info("Health check is disabled for s3 backend", "name", req.Name)

		r.backendStore.SetBackendHealthStatus(req.Name, apisv1alpha1.HealthStatusUnknown)

		if updateErr := r.updateConfigStatus(ctx, providerConfig, func(_, pc *apisv1alpha1.ProviderConfig) {
			pc.Status.Health = apisv1alpha1.HealthStatusUnknown
		}); updateErr != nil {
			err = errors.Wrap(updateErr, errUpdateHealth)
		}

		return
	}

	// Keep the previous health status to compare the current one later
	previousHealth := providerConfig.Status.Health

	// Assume the status is Unhealthy until we can verify otherwise.
	providerConfig.Status.Health = apisv1alpha1.HealthStatusUnhealthy
	providerConfig.Status.Reason = ""
	defer func() {
		r.backendStore.SetBackendHealthStatus(req.Name, providerConfig.Status.Health)

		if updateErr := r.updateConfigStatus(ctx, providerConfig, func(orig, pc *apisv1alpha1.ProviderConfig) {
			pc.Status.Health = orig.Status.Health
			pc.Status.Reason = orig.Status.Reason
		}); updateErr != nil {
			err = errors.Wrap(updateErr, err.Error())
		}
	}()

	if err = r.bucketExists(ctx, req.Name, bucketName); err != nil {
		if err = r.createBucket(ctx, req.Name, bucketName); err != nil {
			r.log.Info("Failed to create bucket for health check on s3 backend", "name", providerConfig.Name, "backend", req.Name)

			providerConfig.Status.Reason = fmt.Sprintf("failed to create bucket: %v", err.Error())

			return
		}
	}

	if err = r.doHealthCheck(ctx, providerConfig, bucketName); err != nil {
		r.log.Info("Failed to do health check on s3 backend", "name", providerConfig.Name, "backend", req.Name)

		providerConfig.Status.Reason = fmt.Sprintf("failed to do health check: %v", err.Error())

		return
	}

	currentHealth := providerConfig.Status.Health

	if currentHealth == apisv1alpha1.HealthStatusHealthy && previousHealth != currentHealth {
		r.log.Info("Backend becomes online again", "provider", providerConfig.Name)
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

func (r *HealthCheckReconciler) cleanup(ctx context.Context, req ctrl.Request, bucketName string) error {
	// The ProviderConfig representing an s3 backend has been deleted,
	// therefore we need to delete the health check bucket from the s3 backend.
	backendClient := r.backendStore.GetBackendClient(req.Name)
	if backendClient == nil {
		r.log.Info("Backend client not found", "name", bucketName)

		return nil
	}

	r.log.Info("Deleting health check bucket", "name", bucketName)

	return s3internal.DeleteBucket(ctx, backendClient, aws.String(bucketName))
}

func (r *HealthCheckReconciler) doHealthCheck(ctx context.Context, providerConfig *apisv1alpha1.ProviderConfig, bucketName string) error {
	s3BackendClient := r.backendStore.GetBackendClient(providerConfig.Name)
	if s3BackendClient == nil {
		return errors.New(errBackendNotStored)
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

// Callbacks have two parameters, first config is the original, the second is the new version of config.
func (r *HealthCheckReconciler) updateConfigStatus(ctx context.Context, pc *apisv1alpha1.ProviderConfig, callbacks ...func(*apisv1alpha1.ProviderConfig, *apisv1alpha1.ProviderConfig)) error {
	origPC := pc.DeepCopy()

	nn := types.NamespacedName{Name: pc.GetName(), Namespace: pc.Namespace}

	const (
		steps  = 4
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

			return errors.Wrap(err, "unable to update object")
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
		r.log.Info(err.Error(), "backend", s3BackendName)

		return
	}

	for i := range buckets.Items {
		i := i
		r.log.Debug("unpause bucket", "bucket", buckets.Items[i].Name)
		err := retry.OnError(wait.Backoff{
			Steps:    steps,
			Duration: duration,
			Factor:   factor,
			Jitter:   jitter,
		}, resource.IsAPIError, func() error {
			if !v1alpha1.IsHealthCheckBucket(&buckets.Items[i]) &&
				(r.autoPauseBucket || buckets.Items[i].Spec.AutoPause) &&
				buckets.Items[i].Annotations[meta.AnnotationKeyReconciliationPaused] == "true" {
				buckets.Items[i].Annotations[meta.AnnotationKeyReconciliationPaused] = ""

				return r.kubeClient.Update(ctx, &buckets.Items[i])
			}

			return nil
		})

		if err != nil {
			r.log.Info(err.Error(), "bucket", buckets.Items[i].Name)
		}
	}
}
