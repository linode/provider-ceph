package providerconfig

import (
	"github.com/crossplane/crossplane-runtime/pkg/controller"
	"github.com/crossplane/crossplane-runtime/pkg/event"
	"github.com/crossplane/crossplane-runtime/pkg/ratelimiter"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/providerconfig"
	"github.com/crossplane/crossplane-runtime/pkg/resource"
	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	ctrl "sigs.k8s.io/controller-runtime"
)

// Setup adds controllers to reconcile the backend store and backend health.
func Setup(mgr ctrl.Manager, o controller.Options, s *backendstore.BackendStore, a bool) error {
	name := providerconfig.ControllerName(apisv1alpha1.ProviderConfigGroupKind)

	of := resource.ProviderConfigKinds{
		Config:    apisv1alpha1.ProviderConfigGroupVersionKind,
		UsageList: apisv1alpha1.ProviderConfigUsageListGroupVersionKind,
	}

	// Add an 'internal' controller to the manager for the ProviderConfig.
	// This will be used to reconcile the backend store.
	if err := newBackendStoreReconciler(mgr.GetClient(), o, s).setupWithManager(mgr); err != nil {
		return err
	}

	// Add an 'internal' controller to the manager for the ProviderConfig.
	// This will be used to reconcile the health of each backend.
	if err := newHealthCheckReconciler(mgr.GetClient(), o, s, a).setupWithManager(mgr); err != nil {
		return err
	}

	r := providerconfig.NewReconciler(mgr, of,
		providerconfig.WithLogger(o.Logger.WithValues("controller", name)),
		providerconfig.WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorderFor(name))))

	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		WithOptions(o.ForControllerRuntime()).
		For(&apisv1alpha1.ProviderConfig{}).
		Watches(&apisv1alpha1.ProviderConfigUsage{}, &resource.EnqueueRequestForProviderConfig{}).
		Complete(ratelimiter.NewReconciler(name, r, o.GlobalRateLimiter))
}
