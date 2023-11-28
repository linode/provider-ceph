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
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane/crossplane-runtime/pkg/connection"
	"github.com/crossplane/crossplane-runtime/pkg/controller"
	"github.com/crossplane/crossplane-runtime/pkg/event"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/ratelimiter"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/pkg/resource"

	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	"github.com/linode/provider-ceph/internal/features"
)

const (
	errNotBucket                = "managed resource is not a Bucket custom resource"
	errTrackPCUsage             = "cannot track ProviderConfig usage"
	errCacheInit                = "cannot init Bucket cache"
	errGetPC                    = "cannot get ProviderConfig"
	errListPC                   = "cannot list ProviderConfigs"
	errGetBucket                = "cannot get Bucket"
	errListBuckets              = "cannot list Buckets"
	errCreateBucket             = "cannot create Bucket"
	errDeleteBucket             = "cannot delete Bucket"
	errUpdateBucket             = "cannot update Bucket"
	errListObjects              = "cannot list objects"
	errDeleteObject             = "cannot delete object"
	errGetCreds                 = "cannot get credentials"
	errBackendNotStored         = "s3 backend is not stored"
	errBackendInactive          = "s3 backend is inactive"
	errNoS3BackendsStored       = "no s3 backends stored"
	errNoS3BackendsRegistered   = "no s3 backends registered"
	errMissingS3Backend         = "missing s3 backends"
	errCodeBucketNotFound       = "NotFound"
	errFailedToCreateClient     = "failed to create s3 client"
	errBucketCreationInProgress = "bucket creation in progress"
	errPutLifecycleConfig       = "cannot put Bucket lifecycle configuration"
	errDeleteLifecycle          = "cannot delete Bucket lifecycle"
	errGetLifecycleConfig       = "cannot get Bucket lifecycle configuration"

	inUseFinalizer = "bucket-in-use.provider-ceph.crossplane.io"
)

// A NoOpService does nothing.
type NoOpService struct{}

var (
	NewNoOpService = func(_ []byte) (interface{}, error) { return &NoOpService{}, nil }
)

// Setup adds a controller that reconciles Bucket managed resources.
func Setup(mgr ctrl.Manager, o controller.Options, c *Connector) error {
	name := managed.ControllerName(v1alpha1.BucketGroupKind)

	cps := []managed.ConnectionPublisher{managed.NewAPISecretPublisher(mgr.GetClient(), mgr.GetScheme())}
	if o.Features.Enabled(features.EnableAlphaExternalSecretStores) {
		cps = append(cps, connection.NewDetailsManager(mgr.GetClient(), apisv1alpha1.StoreConfigGroupVersionKind))
	}

	opts := []managed.ReconcilerOption{
		managed.WithCriticalAnnotationUpdater(managed.NewRetryingCriticalAnnotationUpdater(mgr.GetClient())),
		managed.WithTimeout(c.operationTimeout + time.Second),
		managed.WithPollInterval(c.pollInterval),
		managed.WithExternalConnecter(c),
		managed.WithLogger(o.Logger.WithValues("bucket reconciler", name)),
		managed.WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorderFor(name))),
		managed.WithConnectionPublishers(cps...),
		managed.WithCreationGracePeriod(c.creationGracePeriod),
	}

	if o.Features.Enabled(features.EnableAlphaManagementPolicies) {
		opts = append(opts, managed.WithManagementPolicies())
	}

	r := managed.NewReconciler(mgr, resource.ManagedKind(v1alpha1.BucketGroupVersionKind), opts...)

	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		WithOptions(o.ForControllerRuntime()).
		For(&v1alpha1.Bucket{}).
		Complete(ratelimiter.NewReconciler(name, r, o.GlobalRateLimiter))
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
