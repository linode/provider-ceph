package healthcheck

import (
	"net/http"

	"github.com/crossplane/crossplane-runtime/pkg/controller"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/providerconfig"
	"github.com/go-logr/logr"
	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const controllerName = "health-check-controller"

type Controller struct {
	kubeClientUncached client.Client
	kubeClientCached   client.Client
	backendStore       *backendstore.BackendStore
	httpClient         *http.Client
	log                logr.Logger
	autoPauseBucket    bool
}

func NewController(options ...func(*Controller)) *Controller {
	r := &Controller{}
	for _, o := range options {
		o(r)
	}

	return r
}

func WithKubeClientUncached(k client.Client) func(*Controller) {
	return func(r *Controller) {
		r.kubeClientUncached = k
	}
}

func WithKubeClientCached(k client.Client) func(*Controller) {
	return func(r *Controller) {
		r.kubeClientCached = k
	}
}

func WithLogger(l logr.Logger) func(*Controller) {
	return func(r *Controller) {
		r.log = l.WithValues(apisv1alpha1.ProviderConfigGroupKind, providerconfig.ControllerName(controllerName))
	}
}

func WithBackendStore(b *backendstore.BackendStore) func(*Controller) {
	return func(r *Controller) {
		r.backendStore = b
	}
}

func WithAutoPause(autoPause *bool) func(*Controller) {
	return func(r *Controller) {
		r.autoPauseBucket = *autoPause
	}
}

func WithHttpClient(httpClient *http.Client) func(*Controller) {
	return func(r *Controller) {
		r.httpClient = httpClient
	}
}

func (r *Controller) SetupWithManager(mgr ctrl.Manager) error {
	const maxReconciles = 5

	return ctrl.NewControllerManagedBy(mgr).
		For(&apisv1alpha1.ProviderConfig{}).
		Named(controllerName).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: maxReconciles,
		}.ForControllerRuntime()).
		Complete(r)
}
