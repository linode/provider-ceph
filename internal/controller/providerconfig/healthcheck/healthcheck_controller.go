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

package healthcheck

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
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/crossplane/crossplane-runtime/pkg/resource"

	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	"github.com/linode/provider-ceph/internal/otel/traces"
	s3internal "github.com/linode/provider-ceph/internal/s3"
)

const (
	errPutHealthCheckFile = "failed to upload health check file"
	errGetHealthCheckFile = "failed to get health check file"
	errDoHealthCheck      = "failed to perform health check"
	errUpdateHealth       = "failed to update health status of provider config"
	errBackendNotStored   = "backend is not stored"
	healthCheckSuffix     = "-health-check"
	healthCheckFile       = "health-check-file"
)

func (c *Controller) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	ctx, span := otel.Tracer("").Start(ctx, "healthcheck.Controllec.Reconcile")
	defer span.End()

	c.log.Info("Reconciling health of s3 backend", "backend_name", req.Name)

	bucketName := req.Name + healthCheckSuffix

	providerConfig := &apisv1alpha1.ProviderConfig{}
	if err = c.kubeClient.Get(ctx, req.NamespacedName, providerConfig); err != nil {
		if kerrors.IsNotFound(err) {
			// ProviderConfig has been deleted, perform cleanup.
			return ctrl.Result{}, c.cleanup(ctx, req, bucketName)
		}

		return
	}

	if providerConfig.Spec.DisableHealthCheck {
		c.log.Info("Health check is disabled for s3 backend", "backend_name", providerConfig.Name)

		c.backendStore.SetBackendHealthStatus(req.Name, apisv1alpha1.HealthStatusUnknown)

		if updateErr := UpdateProviderConfigStatus(ctx, c.kubeClient, providerConfig, func(_, pcLatest *apisv1alpha1.ProviderConfig) {
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
		c.backendStore.SetBackendHealthStatus(req.Name, providerConfig.Status.Health)

		if updateErr := UpdateProviderConfigStatus(ctx, c.kubeClient, providerConfig, func(pcDeepCopy, pcLatest *apisv1alpha1.ProviderConfig) {
			pcLatest.Status.Health = pcDeepCopy.Status.Health
			pcLatest.Status.Reason = pcDeepCopy.Status.Reason
		}); updateErr != nil {
			err = errors.Wrap(updateErr, err.Error())
		}
	}()

	// Create a health check bucket on the backend if one does not already exist.
	if err = c.bucketExists(ctx, req.Name, bucketName); err != nil {
		if err = c.createBucket(ctx, req.Name, bucketName); err != nil {
			c.log.Info("Failed to create bucket for health check on s3 backend", "bucket_name", bucketName, "backend_name", providerConfig.Name)

			providerConfig.Status.Reason = fmt.Sprintf("failed to create health check bucket: %v", err.Error())

			return
		}
	}

	// Perform the health check. By calling this function, we are implicitly updating
	// the health status of the ProviderConfig with whatever the health check reports.
	if err = c.doHealthCheck(ctx, providerConfig, bucketName); err != nil {
		traces.SetAndRecordError(span, err)
		c.log.Info("Failed to do health check on s3 backend", "bucket_name", bucketName, "backend_name", providerConfig.Name)

		providerConfig.Status.Reason = errDoHealthCheck + ": " + err.Error()

		return
	}

	// Check if the backend is healthy, where prior to the check it was unhealthy.
	// In which case, we need to unpause all Bucket CRs that have buckets stored
	// on this backend. We do this to allow these Bucket CRs be reconciled again.
	healthAfterCheck := providerConfig.Status.Health
	if healthAfterCheck == apisv1alpha1.HealthStatusHealthy && healthBeforeCheck != healthAfterCheck {
		c.log.Info("Backend is healthy where previously it was unhealthy - unpausing all Buckets on backend to allow Observation", "backend_name", providerConfig.Name)
		go c.unpauseBuckets(ctx, providerConfig.Name)
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
func (c *Controller) cleanup(ctx context.Context, req ctrl.Request, bucketName string) error {
	backendClient := c.backendStore.GetBackendClient(req.Name)
	if backendClient == nil {
		c.log.Info("Backend client not found during health check bucket cleanup - aborting cleanup", "backend_name", req.Name)

		return nil
	}

	c.log.Info("Deleting health check bucket", "bucket_name", bucketName, "backend_name", req.Name)

	if err := s3internal.DeleteBucket(ctx, backendClient, aws.String(bucketName)); err != nil {
		return err
	}

	c.log.Info("Deleting lifecycle configuration validation bucket", "bucket_name", v1alpha1.LifecycleConfigValidationBucketName, "backend_name", req.Name)

	return s3internal.DeleteBucket(ctx, backendClient, aws.String(v1alpha1.LifecycleConfigValidationBucketName))
}

// doHealthCheck performs a PutObject and GetObject on the health check bucket on the backend.
func (c *Controller) doHealthCheck(ctx context.Context, providerConfig *apisv1alpha1.ProviderConfig, bucketName string) error {
	ctx, span := otel.Tracer("").Start(ctx, "Controller.doHealthCheck")
	defer span.End()

	s3BackendClient := c.backendStore.GetBackendClient(providerConfig.Name)
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

func (c *Controller) bucketExists(ctx context.Context, s3BackendName, bucketName string) error {
	s3BackendClient := c.backendStore.GetBackendClient(s3BackendName)
	if s3BackendClient == nil {
		return errors.New(errBackendNotStored)
	}

	_, err := s3BackendClient.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(bucketName)})

	return err
}

func (c *Controller) createBucket(ctx context.Context, s3BackendName, bucketName string) error {
	s3BackendClient := c.backendStore.GetBackendClient(s3BackendName)
	if s3BackendClient == nil {
		return errors.New(errBackendNotStored)
	}

	_, err := s3BackendClient.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucketName),
	})

	return resource.Ignore(s3internal.IsAlreadyExists, err)
}

// unpauseBuckets lists all buckets that exist on the given backend by using the custom
// backend label. Then, using retry.OnError(), it attempts to unpause each of these buckets
// by unsetting the Pause label.
func (c *Controller) unpauseBuckets(ctx context.Context, s3BackendName string) {
	const (
		steps    = 4
		duration = time.Second
		factor   = 5
		jitter   = 0.1
	)

	// Only list Buckets that (a) were created on s3BackendName
	// and (b) are already paused.
	listLabels := labels.SelectorFromSet(labels.Set(map[string]string{
		v1alpha1.BackendLabelPrefix + s3BackendName: "true",
		meta.AnnotationKeyReconciliationPaused:      "true",
	}))

	buckets := &v1alpha1.BucketList{}
	err := retry.OnError(wait.Backoff{
		Steps:    steps,
		Duration: duration,
		Factor:   factor,
		Jitter:   jitter,
	}, resource.IsAPIError, func() error {
		return c.kubeClient.List(ctx, buckets, &client.ListOptions{LabelSelector: listLabels})
	})
	if err != nil {
		c.log.Info("Error attempting to list Buckets on backend", "error", err.Error(), "backend_name", s3BackendName)

		return
	}

	for i := range buckets.Items {
		i := i
		c.log.Debug("Attempting to unpause bucket", "bucket_name", buckets.Items[i].Name)
		err := retry.OnError(wait.Backoff{
			Steps:    steps,
			Duration: duration,
			Factor:   factor,
			Jitter:   jitter,
		}, resource.IsAPIError, func() error {
			if (c.autoPauseBucket || buckets.Items[i].Spec.AutoPause) &&
				buckets.Items[i].Labels[meta.AnnotationKeyReconciliationPaused] == "true" {
				buckets.Items[i].Labels[meta.AnnotationKeyReconciliationPaused] = ""

				return c.kubeClient.Update(ctx, &buckets.Items[i])
			}

			return nil
		})

		if err != nil {
			c.log.Info("Error attempting to unpause bucket", "error", err.Error(), "bucket", buckets.Items[i].Name)
		}
	}
}
