package bucket

import (
	"context"
	"time"

	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"github.com/go-logr/logr"
	"github.com/linode/provider-ceph/internal/backendstore"
	"github.com/linode/provider-ceph/internal/controller/s3clienthandler"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// A Connector is expected to produce an ExternalClient when its Connect method
// is called.
type Connector struct {
	kube                  client.Client
	autoPauseBucket       bool
	minReplicas           uint
	recreateMissingBucket bool
	backendStore          *backendstore.BackendStore
	subresourceClients    []SubresourceClient
	s3ClientHandler       *s3clienthandler.Handler
	log                   logr.Logger
	operationTimeout      time.Duration
	creationGracePeriod   time.Duration
	pollInterval          time.Duration
	usage                 resource.Tracker
	newServiceFn          func(creds []byte) (interface{}, error)
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

func WithMinimumReplicas(m *uint) func(*Connector) {
	return func(c *Connector) {
		c.minReplicas = *m
	}
}

func WithRecreateMissingBucket(a *bool) func(*Connector) {
	return func(c *Connector) {
		c.recreateMissingBucket = *a
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

func WithS3ClientHandler(h *s3clienthandler.Handler) func(*Connector) {
	return func(c *Connector) {
		c.s3ClientHandler = h
	}
}

func WithLog(l logr.Logger) func(*Connector) {
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
			kubeClient:            c.kube,
			autoPauseBucket:       c.autoPauseBucket,
			minReplicas:           c.minReplicas,
			recreateMissingBucket: c.recreateMissingBucket,
			operationTimeout:      c.operationTimeout,
			backendStore:          c.backendStore,
			subresourceClients:    c.subresourceClients,
			s3ClientHandler:       c.s3ClientHandler,
			log:                   c.log,
		},
		nil
}

// external observes, then either creates, updates, or deletes an external
// resource to ensure it reflects the managed resource's desired state.
type external struct {
	kubeClient            client.Client
	autoPauseBucket       bool
	minReplicas           uint
	recreateMissingBucket bool
	operationTimeout      time.Duration
	backendStore          *backendstore.BackendStore
	subresourceClients    []SubresourceClient
	s3ClientHandler       *s3clienthandler.Handler
	log                   logr.Logger
}
