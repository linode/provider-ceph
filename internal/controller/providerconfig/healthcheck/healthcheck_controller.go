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
	"net/http"
	"time"

	v1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/errors"
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
	"github.com/linode/provider-ceph/internal/consts"
	"github.com/linode/provider-ceph/internal/otel/traces"
	"github.com/linode/provider-ceph/internal/utils"
)

const (
	errUpdateHealthStatus   = "failed to update health status of provider config"
	errFailedHealthCheckReq = "failed to forward health check request"

	healthCheckSuffix = "-health-check"

	True = "true"
)

func (c *Controller) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx, span := otel.Tracer("").Start(ctx, "healthcheck.Controller.Reconcile")
	defer span.End()

	c.log.Info("Reconciling health of s3 backend", consts.KeyBackendName, req.Name)

	bucketName := req.Name + healthCheckSuffix

	providerConfig := &apisv1alpha1.ProviderConfig{}
	if err := c.kubeClientCached.Get(ctx, req.NamespacedName, providerConfig); err != nil {
		if kerrors.IsNotFound(err) {
			// ProviderConfig has been deleted so there is nothing to do and no need to requeue.
			// The backend monitor controller will remove the backend from the backend store.
			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, err
	}

	if providerConfig.Spec.DisableHealthCheck {
		c.log.Debug("Health check is disabled for s3 backend", consts.KeyBackendName, providerConfig.Name)

		c.backendStore.SetBackendHealthStatus(req.Name, apisv1alpha1.HealthStatusUnknown)
		if providerConfig.Status.GetCondition(v1.TypeReady).Equal(v1alpha1.HealthCheckDisabled()) {
			return ctrl.Result{}, nil
		}

		if err := UpdateProviderConfigStatus(ctx, c.kubeClientCached, providerConfig, func(_, pcLatest *apisv1alpha1.ProviderConfig) {
			pcLatest.Status.SetConditions(v1alpha1.HealthCheckDisabled())
		}); err != nil {
			err = errors.Wrap(err, errUpdateHealthStatus)
			traces.SetAndRecordError(span, err)

			return ctrl.Result{}, err
		}

		return ctrl.Result{}, nil
	}

	// Store the condition before the check so that we can compare
	// with the condition after the check.
	conditionBeforeCheck := providerConfig.Status.GetCondition(v1.TypeReady)

	// Assume the backend is unhealthy and set a HealthCheckFail  condition until we can verify otherwise.
	providerConfig.Status.SetConditions(v1alpha1.HealthCheckFail())

	defer func() {
		health := utils.MapConditionToHealthStatus(providerConfig.Status.GetCondition(v1.TypeReady))
		c.backendStore.SetBackendHealthStatus(req.Name, health)

		if providerConfig.Status.GetCondition(v1.TypeReady).Equal(conditionBeforeCheck) {
			return
		}

		if err := UpdateProviderConfigStatus(ctx, c.kubeClientCached, providerConfig, func(pcDeepCopy, pcLatest *apisv1alpha1.ProviderConfig) {
			pcLatest.Status.SetConditions(pcDeepCopy.Status.Conditions...)
		}); err != nil {
			err = errors.Wrap(err, errUpdateHealthStatus)
			traces.SetAndRecordError(span, err)
		}
	}()

	// Perform the health check. By calling this function, we are implicitly updating
	// the health status of the ProviderConfig with whatever the health check reports.
	if err := c.doHealthCheck(ctx, providerConfig); err != nil {
		c.log.Info("Failed to do health check on s3 backend", consts.KeyBucketName, bucketName, consts.KeyBackendName, providerConfig.Name)

		providerConfig.Status.SetConditions(v1alpha1.HealthCheckFail().WithMessage(errNoRequestID(err)))
		traces.SetAndRecordError(span, err)

		return ctrl.Result{}, err
	}

	// Check if the backend is healthy, where prior to the check it was unhealthy.
	// In which case, we need to unpause all Bucket CRs that have buckets stored
	// on this backend. We do this to allow these Bucket CRs be reconciled again.
	conditionAfterCheck := providerConfig.Status.GetCondition(v1.TypeReady)

	if conditionAfterCheck.Equal(v1alpha1.HealthCheckSuccess()) && !conditionBeforeCheck.Equal(conditionAfterCheck) {
		c.log.Info("Backend is healthy where previously it was unhealthy - unpausing all Buckets on backend to allow Observation", consts.KeyBackendName, providerConfig.Name)
		go c.unpauseBuckets(ctx, providerConfig.Name)
	}

	// Health check interval is 30s by default.
	// It is safe to requeue after the same object multiple times,
	// because controller runtime reconcilies only once.
	return ctrl.Result{
		RequeueAfter: time.Duration(providerConfig.Spec.HealthCheckIntervalSeconds) * time.Second,
	}, nil
}

// doHealthCheck performs a basic http request to the hostbase address.
func (c *Controller) doHealthCheck(ctx context.Context, providerConfig *apisv1alpha1.ProviderConfig) error {
	ctx, span := otel.Tracer("").Start(ctx, "Controller.doHealthCheck")
	defer span.End()

	address := utils.ResolveHostBase(providerConfig.Spec.HostBase, providerConfig.Spec.UseHTTPS)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, http.NoBody)
	if err != nil {
		msg := "failed to create request for health check"
		reqErr := errors.Wrap(err, msg)
		traces.SetAndRecordError(span, reqErr)

		return reqErr
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		doErr := errors.Wrap(err, errFailedHealthCheckReq)
		traces.SetAndRecordError(span, doErr)

		return doErr
	}
	// We don't actually check the response code or body for the health check.
	// This is because a HTTP Get request to RGW equates to a ListBuckets S3 request and
	// it is possible that an authorisation error will occur, resulting in a 4XX error.
	// Instead, we assume healthiness by simply making the connection successfully.

	// Health check completed successfully, update status.
	providerConfig.Status.SetConditions(v1alpha1.HealthCheckSuccess())

	return resp.Body.Close()
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
		utils.GetBackendLabel(s3BackendName):   True,
		meta.AnnotationKeyReconciliationPaused: True,
	}))

	buckets := &v1alpha1.BucketList{}
	err := retry.OnError(wait.Backoff{
		Steps:    steps,
		Duration: duration,
		Factor:   factor,
		Jitter:   jitter,
	}, resource.IsAPIError, func() error {
		return c.kubeClientUncached.List(ctx, buckets, &client.ListOptions{
			LabelSelector: listLabels,
		})
	})
	if err != nil {
		c.log.Info("Error attempting to list Buckets on backend", "error", err.Error(), consts.KeyBackendName, s3BackendName)

		return
	}

	for i := range buckets.Items {
		c.log.Debug("Attempting to unpause bucket", consts.KeyBucketName, buckets.Items[i].Name)
		err := retry.OnError(wait.Backoff{
			Steps:    steps,
			Duration: duration,
			Factor:   factor,
			Jitter:   jitter,
		}, resource.IsAPIError, func() error {
			if (c.autoPauseBucket || buckets.Items[i].Spec.AutoPause) &&
				buckets.Items[i].Labels[meta.AnnotationKeyReconciliationPaused] == True {
				buckets.Items[i].Labels[meta.AnnotationKeyReconciliationPaused] = ""

				return c.kubeClientCached.Update(ctx, &buckets.Items[i])
			}

			return nil
		})

		if err != nil {
			c.log.Info("Error attempting to unpause bucket", "error", err.Error(), "bucket", buckets.Items[i].Name)
		}
	}
}
