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
	kubeClient      client.Client
	cachedReader    client.Reader
	backendStore    *backendstore.BackendStore
	httpClient      *http.Client
	log             logr.Logger
	autoPauseBucket bool
}

func NewController(options ...func(*Controller)) *Controller {
	r := &Controller{}
	for _, o := range options {
		o(r)
	}

	return r
}

func WithCachedReader(r client.Reader) func(*Controller) {
	return func(c *Controller) {
		c.cachedReader = r
	}
}

func WithKubeClient(k client.Client) func(*Controller) {
	return func(c *Controller) {
		c.kubeClient = k
	}
}

func WithLogger(l logr.Logger) func(*Controller) {
	return func(c *Controller) {
		c.log = l.WithValues(apisv1alpha1.ProviderConfigGroupKind, providerconfig.ControllerName(controllerName))
	}
}

func WithBackendStore(b *backendstore.BackendStore) func(*Controller) {
	return func(c *Controller) {
		c.backendStore = b
	}
}

func WithAutoPause(autoPause *bool) func(*Controller) {
	return func(c *Controller) {
		c.autoPauseBucket = *autoPause
	}
}

func WithHttpClient(httpClient *http.Client) func(*Controller) {
	return func(c *Controller) {
		c.httpClient = httpClient
	}
}

func (c *Controller) SetupWithManager(mgr ctrl.Manager) error {
	const maxReconciles = 5

	return ctrl.NewControllerManagedBy(mgr).
		For(&apisv1alpha1.ProviderConfig{}).
		Named(controllerName).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: maxReconciles,
		}.ForControllerRuntime()).
		Complete(c)
}
