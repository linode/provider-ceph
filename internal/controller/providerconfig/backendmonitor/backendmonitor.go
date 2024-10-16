package backendmonitor

import (
	"time"

	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/providerconfig"
	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Controller struct {
	kubeClient      client.Client
	backendStore    *backendstore.BackendStore
	log             logging.Logger
	s3Timeout       time.Duration
	requeueInterval time.Duration
}

func NewController(options ...func(*Controller)) *Controller {
	r := &Controller{}
	for _, o := range options {
		o(r)
	}

	return r
}

func WithKubeClient(k client.Client) func(*Controller) {
	return func(r *Controller) {
		r.kubeClient = k
	}
}

func WithLogger(l logging.Logger) func(*Controller) {
	return func(r *Controller) {
		r.log = l.WithValues("backend-store-controller", providerconfig.ControllerName(apisv1alpha1.ProviderConfigGroupKind))
	}
}

func WithBackendStore(b *backendstore.BackendStore) func(*Controller) {
	return func(r *Controller) {
		r.backendStore = b
	}
}

func WithS3Timeout(t time.Duration) func(*Controller) {
	return func(r *Controller) {
		r.s3Timeout = t
	}
}

func WithRequeueInterval(t time.Duration) func(*Controller) {
	return func(r *Controller) {
		r.requeueInterval = t
	}
}

func (c *Controller) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&apisv1alpha1.ProviderConfig{}).
		Complete(c)
}
