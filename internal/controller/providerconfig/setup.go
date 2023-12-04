package providerconfig

import (
	"github.com/crossplane/crossplane-runtime/pkg/controller"
	"github.com/crossplane/crossplane-runtime/pkg/event"
	"github.com/crossplane/crossplane-runtime/pkg/ratelimiter"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/providerconfig"
	"github.com/crossplane/crossplane-runtime/pkg/resource"
	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	"github.com/linode/provider-ceph/internal/controller/providerconfig/backendmonitor"
	"github.com/linode/provider-ceph/internal/controller/providerconfig/healthcheck"
	"github.com/pkg/errors"
	ctrl "sigs.k8s.io/controller-runtime"
)

// Setup adds controllers to reconcile the backend store and backend health.
func Setup(mgr ctrl.Manager, o controller.Options, b *backendmonitor.Controller, h *healthcheck.Controller) error {
	// Add an 'internal' controller to the manager for the ProviderConfig.
	// This will be used to reconcile the backend store.
	if err := b.SetupWithManager(mgr); err != nil {
		return errors.Wrap(err, "failed to setup backendstore controller")
	}

	// Add an 'internal' controller to the manager for the ProviderConfig.
	// This will be used to reconcile the health of each backend.
	if err := h.SetupWithManager(mgr); err != nil {
		return errors.Wrap(err, "failed to setup health check controller")
	}

	name := providerconfig.ControllerName(apisv1alpha1.ProviderConfigGroupKind)

	of := resource.ProviderConfigKinds{
		Config:    apisv1alpha1.ProviderConfigGroupVersionKind,
		UsageList: apisv1alpha1.ProviderConfigUsageListGroupVersionKind,
	}

	r := providerconfig.NewReconciler(mgr, of,
		providerconfig.WithLogger(o.Logger.WithValues("providerconfig-reconciler", name)),
		providerconfig.WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorderFor(name))))

	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		WithOptions(o.ForControllerRuntime()).
		For(&apisv1alpha1.ProviderConfig{}).
		Watches(&apisv1alpha1.ProviderConfigUsage{}, &resource.EnqueueRequestForProviderConfig{}).
		Complete(ratelimiter.NewReconciler(name, r, o.GlobalRateLimiter))
}
