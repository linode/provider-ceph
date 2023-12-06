package bucket

import (
	"context"
	"time"

	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/linode/provider-ceph/internal/backendstore"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// A Connector is expected to produce an ExternalClient when its Connect method
// is called.
type Connector struct {
	kube                client.Client
	autoPauseBucket     bool
	backendStore        *backendstore.BackendStore
	subresourceClients  []SubresourceClient
	log                 logging.Logger
	operationTimeout    time.Duration
	creationGracePeriod time.Duration
	pollInterval        time.Duration
	usage               resource.Tracker
	newServiceFn        func(creds []byte) (interface{}, error)
}

func NewConnector(options ...func(*Connector)) *Connector {
	c := &Connector{}
	for _, o := range options {
		o(c)
	}

	return c
}

func WithKubeClient(k client.Client) func(*Connector) {
	return func(c *Connector) {
		c.kube = k
	}
}

func WithAutoPause(a *bool) func(*Connector) {
	return func(c *Connector) {
		c.autoPauseBucket = *a
	}
}

func WithOperationTimeout(t time.Duration) func(*Connector) {
	return func(c *Connector) {
		c.operationTimeout = t
	}
}

func WithCreationGracePeriod(t time.Duration) func(*Connector) {
	return func(c *Connector) {
		c.creationGracePeriod = t
	}
}

func WithPollInterval(t time.Duration) func(*Connector) {
	return func(c *Connector) {
		c.pollInterval = t
	}
}

func WithUsage(u resource.Tracker) func(*Connector) {
	return func(c *Connector) {
		c.usage = u
	}
}

func WithBackendStore(s *backendstore.BackendStore) func(*Connector) {
	return func(c *Connector) {
		c.backendStore = s
	}
}

func WithSubresourceClients(s []SubresourceClient) func(*Connector) {
	return func(c *Connector) {
		c.subresourceClients = s
	}
}

func WithLog(l logging.Logger) func(*Connector) {
	return func(c *Connector) {
		c.log = l
	}
}

func WithNewServiceFn(s func(creds []byte) (interface{}, error)) func(*Connector) {
	return func(c *Connector) {
		c.newServiceFn = s
	}
}

func (c *Connector) Connect(ctx context.Context, mg resource.Managed) (managed.ExternalClient, error) {
	if err := c.usage.Track(ctx, mg); err != nil {
		return nil, errors.Wrap(err, errTrackPCUsage)
	}

	return &external{
			kubeClient:         c.kube,
			autoPauseBucket:    c.autoPauseBucket,
			operationTimeout:   c.operationTimeout,
			backendStore:       c.backendStore,
			subresourceClients: c.subresourceClients,
			log:                c.log},
		nil
}

// external observes, then either creates, updates, or deletes an external
// resource to ensure it reflects the managed resource's desired state.
type external struct {
	kubeClient         client.Client
	autoPauseBucket    bool
	operationTimeout   time.Duration
	backendStore       *backendstore.BackendStore
	subresourceClients []SubresourceClient
	log                logging.Logger
}
