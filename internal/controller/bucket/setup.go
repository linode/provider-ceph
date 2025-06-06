/*
Copyright 2022 The Crossplane Authors.

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
	"time"

	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/crossplane/crossplane-runtime/v2/pkg/controller"
	"github.com/crossplane/crossplane-runtime/v2/pkg/event"
	"github.com/crossplane/crossplane-runtime/v2/pkg/ratelimiter"
	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"github.com/crossplane/crossplane-runtime/v2/pkg/statemetrics"

	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	"github.com/linode/provider-ceph/internal/features"
)

// A NoOpService does nothing.
type NoOpService struct{}

var (
	NewNoOpService = func(_ []byte) (interface{}, error) { return &NoOpService{}, nil }
)

// Setup adds a controller that reconciles Bucket managed resources.
func Setup(mgr ctrl.Manager, o controller.Options, c *Connector) error {
	name := managed.ControllerName(v1alpha1.BucketGroupKind)

	opts := []managed.ReconcilerOption{
		managed.WithCriticalAnnotationUpdater(managed.NewRetryingCriticalAnnotationUpdater(mgr.GetClient())),
		managed.WithTimeout(c.operationTimeout + time.Second),
		managed.WithPollInterval(c.pollInterval),
		managed.WithExternalConnector(c),
		managed.WithLogger(o.Logger.WithValues("bucket reconciler", name)),
		managed.WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorderFor(name))),
		managed.WithCreationGracePeriod(c.creationGracePeriod),
		managed.WithDeterministicExternalName(true),
	}

	if o.Features.Enabled(features.EnableAlphaManagementPolicies) {
		opts = append(opts, managed.WithManagementPolicies())
	}

	if err := mgr.Add(statemetrics.NewMRStateRecorder(
		mgr.GetClient(), o.Logger, o.MetricOptions.MRStateMetrics, &v1alpha1.BucketList{}, o.MetricOptions.PollStateMetricInterval)); err != nil {
		return err
	}

	r := managed.NewReconciler(mgr, resource.ManagedKind(v1alpha1.BucketGroupVersionKind), opts...)

	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		WithOptions(o.ForControllerRuntime()).
		WithEventFilter(resource.DesiredStateChanged()).
		For(&v1alpha1.Bucket{}).
		Complete(ratelimiter.NewReconciler(name, r, o.GlobalRateLimiter))
}
