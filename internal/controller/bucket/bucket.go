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
	"context"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
	"gopkg.in/alecthomas/kingpin.v2"
	"k8s.io/apimachinery/pkg/types"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/pkg/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/connection"
	"github.com/crossplane/crossplane-runtime/pkg/controller"
	"github.com/crossplane/crossplane-runtime/pkg/event"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/ratelimiter"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/pkg/resource"

	"github.com/allegro/bigcache/v3"
	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	"github.com/linode/provider-ceph/internal/features"
	s3internal "github.com/linode/provider-ceph/internal/s3"
	"github.com/linode/provider-ceph/pkg/utils"
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

	inUseFinalizer = "bucket-in-use.provider-ceph.crossplane.io"
)

var bucketCache *bigcache.BigCache

func init() {
	var err error

	bucketCache, err = bigcache.New(context.Background(), bigcache.DefaultConfig(time.Hour))
	kingpin.FatalIfError(err, "Cannot init bucket cache")
}

// A NoOpService does nothing.
type NoOpService struct{}

var (
	newNoOpService = func(_ []byte) (interface{}, error) { return &NoOpService{}, nil }
)

// Setup adds a controller that reconciles Bucket managed resources.
func Setup(mgr ctrl.Manager, o controller.Options, s *backendstore.BackendStore) error {
	name := managed.ControllerName(v1alpha1.BucketGroupKind)

	cps := []managed.ConnectionPublisher{managed.NewAPISecretPublisher(mgr.GetClient(), mgr.GetScheme())}
	if o.Features.Enabled(features.EnableAlphaExternalSecretStores) {
		cps = append(cps, connection.NewDetailsManager(mgr.GetClient(), apisv1alpha1.StoreConfigGroupVersionKind))
	}

	opts := []managed.ReconcilerOption{
		managed.WithExternalConnecter(&connector{
			kube:         mgr.GetClient(),
			usage:        resource.NewProviderConfigUsageTracker(mgr.GetClient(), &apisv1alpha1.ProviderConfigUsage{}),
			newServiceFn: newNoOpService,
			backendStore: s,
			log:          o.Logger.WithValues("controller", name),
		}),
		managed.WithLogger(o.Logger.WithValues("controller", name)),
		managed.WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorderFor(name))),
		managed.WithConnectionPublishers(cps...),
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

// A connector is expected to produce an ExternalClient when its Connect method
// is called.
type connector struct {
	kube         client.Client
	usage        resource.Tracker
	newServiceFn func(creds []byte) (interface{}, error)
	backendStore *backendstore.BackendStore
	log          logging.Logger
}

// Connect typically produces an ExternalClient by:
// 1. Tracking that the managed resource is using a ProviderConfig.
// 2. Getting the managed resource's ProviderConfig.
// 3. Getting the credentials specified by the ProviderConfig.
// 4. Using the credentials to form a client.
func (c *connector) Connect(ctx context.Context, mg resource.Managed) (managed.ExternalClient, error) {
	if err := c.usage.Track(ctx, mg); err != nil {
		return nil, errors.Wrap(err, errTrackPCUsage)
	}

	return &external{
			kubeClient:   c.kube,
			backendStore: c.backendStore,
			log:          c.log},
		nil
}

// An ExternalClient observes, then either creates, updates, or deletes an
// external resource to ensure it reflects the managed resource's desired state.
type external struct {
	kubeClient   client.Client
	backendStore *backendstore.BackendStore
	log          logging.Logger
}

func (c *external) Observe(ctx context.Context, mg resource.Managed) (managed.ExternalObservation, error) {
	bucket, ok := mg.(*v1alpha1.Bucket)
	if !ok {
		return managed.ExternalObservation{}, errors.New(errNotBucket)
	}

	if !c.backendStore.BackendsAreStored() {
		return managed.ExternalObservation{}, errors.New(errNoS3BackendsStored)
	}

	type bucketExistsResult struct {
		bucketExists bool
		err          error
	}

	bucketExistsResults := make(chan bucketExistsResult)

	// Create a new context and cancel it when we have either found the bucket
	// somewhere or cannot find it anywhere.
	ctxC, cancel := context.WithCancel(ctx)
	defer cancel()

	if len(bucket.Spec.Providers) == 0 {
		bucket.Spec.Providers = c.backendStore.GetAllActiveBackendNames()
	}

	// Check for the bucket on each backend in a separate go routine
	allBackendClients := c.backendStore.GetBackendClients(bucket.Spec.Providers)
	for _, backendClient := range allBackendClients {
		go func(backendClient *s3.Client, bucketName string) {
			bucketExists, err := s3internal.BucketExists(ctxC, backendClient, bucketName)
			bucketExistsResults <- bucketExistsResult{bucketExists, err}
		}(backendClient, bucket.Name)
	}

	// Wait for any go routine to finish, if the bucket exists anywhere
	// return 'ResourceExists: true' as resulting calls to Create or Delete
	// are idempotent.
	for i := 0; i < len(allBackendClients); i++ {
		result := <-bucketExistsResults
		if result.err != nil {
			c.log.Info(errors.Wrap(result.err, errGetBucket).Error())

			continue
		}

		if result.bucketExists {
			return managed.ExternalObservation{
				// Return false when the external resource does not exist. This lets
				// the managed resource reconciler know that it needs to call Create to
				// (re)create the resource, or that it has successfully been deleted.
				ResourceExists: true,

				// Return false when the external resource exists, but it not up to date
				// with the desired managed resource state. This lets the managed
				// resource reconciler know that it needs to call Update.
				ResourceUpToDate: false,

				// Return any details that may be required to connect to the external
				// resource. These will be stored as the connection secret.
				ConnectionDetails: managed.ConnectionDetails{},
			}, nil
		}
	}

	// bucket not found anywhere.
	return managed.ExternalObservation{
		// Return false when the external resource does not exist. This lets
		// the managed resource reconciler know that it needs to call Create to
		// (re)create the resource, or that it has successfully been deleted.
		// If the bucket's Disabled flag has been set, no further action is needed.
		ResourceExists: bucket.Spec.Disabled,
	}, nil
}

//nolint:maintidx,gocognit,gocyclo,cyclop,nolintlint // Function requires numerous checks.
func (c *external) Create(ctx context.Context, mg resource.Managed) (managed.ExternalCreation, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()

	bucket, ok := mg.(*v1alpha1.Bucket)
	if !ok {
		return managed.ExternalCreation{}, errors.New(errNotBucket)
	}

	if bucket.Spec.Disabled {
		c.log.Info("Bucket is disabled - no buckets to be created on backends", "bucket name", bucket.Name)

		return managed.ExternalCreation{}, nil
	}

	if !c.backendStore.BackendsAreStored() {
		return managed.ExternalCreation{}, errors.New(errNoS3BackendsStored)
	}

	// This solution expects we have one leader of the controllers.
	if err := bucketCache.Set(string(bucket.UID), []byte(bucket.ObjectMeta.ResourceVersion)); err != nil {
		return managed.ExternalCreation{}, err
	}

	if len(bucket.Spec.Providers) == 0 {
		bucket.Spec.Providers = c.backendStore.GetAllActiveBackendNames()
	}

	// Create the bucket on each backend in a separate go routine
	activeBackends := c.backendStore.GetActiveBackends(bucket.Spec.Providers)
	if len(activeBackends) == 0 {
		return managed.ExternalCreation{}, errors.New(errNoS3BackendsRegistered)
	} else if len(activeBackends) != len(bucket.Spec.Providers) {
		return managed.ExternalCreation{}, errors.New(errMissingS3Backend)
	}

	bucket.Status.SetConditions(xpv1.Creating())

	wg := sync.WaitGroup{}
	lock := sync.Mutex{}
	errorsLeft := 0
	errChan := make(chan error, len(activeBackends))

	for beName := range activeBackends {
		originalBucket := bucket.DeepCopy()

		cl := c.backendStore.GetBackendClient(beName)
		if cl == nil {
			c.log.Info("Backend client not found for backend - bucket cannot be created on backend", "bucket name", originalBucket.Name, "backend name", beName)

			continue
		}

		c.log.Info("Creating bucket", "bucket name", originalBucket.Name, "backend name", beName)

		pc := &apisv1alpha1.ProviderConfig{}
		if err := c.kubeClient.Get(ctx, types.NamespacedName{Name: beName}, pc); err != nil {
			return managed.ExternalCreation{}, errors.Wrap(err, errGetPC)
		}

		if utils.IsHealthCheckBucket(bucket) && pc.Spec.DisableHealthCheck {
			c.log.Info("Health check is disabled on backend - health-check-bucket will not be created", "backend name", beName)

			continue
		}

		wg.Add(1)
		errorsLeft++

		beName := beName
		go func() {
			defer wg.Done()

			if status, ok := originalBucket.Status.AtProvider.BackendStatuses[beName]; ok && status == v1alpha1.BackendReadyStatus {
				c.log.Info("Bucket already exists on backend", "bucket name", originalBucket.Name, "backend name", beName)

				errChan <- nil

				return
			}

			var err error

			for i := 0; i < s3internal.RequestRetries; i++ {
				_, err = cl.CreateBucket(ctx, s3internal.BucketToCreateBucketInput(originalBucket))
				if resource.Ignore(isAlreadyExists, err) == nil {
					break
				}
			}

			if err != nil {
				c.log.Info("Failed to create bucket on backend", "backend name", beName, "bucket_name", originalBucket.Name)

				errChan <- err

				return
			}

			lock.Lock()
			defer lock.Unlock()

			latestVersion, err := bucketCache.Get(string(originalBucket.UID))
			if err != nil && !errors.Is(err, bigcache.ErrEntryNotFound) {
				c.log.Info("Failed to get bucket from cache", "backend name", beName, "bucket_name", originalBucket.Name)

				errChan <- err

				return
			}

			bucketToUpdate := originalBucket
			if latestVersion == nil || bucketToUpdate.ObjectMeta.ResourceVersion != string(latestVersion) {
				c.log.Info("Bucket version is obsolete", "bucket_name", originalBucket.Name)

				bucketToUpdate = &v1alpha1.Bucket{}

				if err := c.kubeClient.Get(ctx, types.NamespacedName{Name: bucketToUpdate.Name}, bucketToUpdate); err != nil {
					c.log.Info("Failed to fetch latest bucket", "backend name", beName, "bucket_name", bucketToUpdate.Name)

					errChan <- err

					return
				}
			}

			bucketToUpdate.Status.SetConditions(xpv1.Available())

			if bucketToUpdate.Status.AtProvider.BackendStatuses == nil {
				bucketToUpdate.Status.AtProvider.BackendStatuses = v1alpha1.BackendStatuses{}
			}
			bucketToUpdate.Status.AtProvider.BackendStatuses[beName] = v1alpha1.BackendReadyStatus

			if err := c.kubeClient.Update(ctx, bucketToUpdate); err != nil {
				c.log.Info("Failed to update bucket", "backend name", beName, "bucket_name", bucketToUpdate.Name)

				errChan <- err

				return
			}

			if err := bucketCache.Set(string(originalBucket.UID), []byte(bucketToUpdate.ObjectMeta.ResourceVersion)); err != nil {
				c.log.Info("Failed to set bucket in cache", "backend name", beName, "bucket_name", originalBucket.Name)

				errChan <- err

				return
			}

			errChan <- nil
		}()
	}

	if errorsLeft == 0 {
		c.log.Info("Failed to find any backend for bucket", "bucket_name", bucket.Name)

		if err := bucketCache.Delete(string(bucket.UID)); err != nil && !errors.Is(err, bigcache.ErrEntryNotFound) {
			c.log.Info("Failed to delete bucket from cache", "bucket_name", bucket.Name)

			return managed.ExternalCreation{}, err
		}

		return managed.ExternalCreation{}, nil
	}

	return c.waitForCreation(ctx, bucket, errChan, errorsLeft, &wg)
}

func (c *external) waitForCreation(ctx context.Context, bucket *v1alpha1.Bucket, errChan chan error, errorsLeft int, wg *sync.WaitGroup) (managed.ExternalCreation, error) {
	var err error

WAIT:
	for {
		select {
		case <-ctx.Done():
			c.log.Info("Context timeout", "bucket_name", bucket.Name)

			return managed.ExternalCreation{}, ctx.Err()
		case err = <-errChan:
			errorsLeft--

			if err != nil {
				c.log.Info("Failed to create on backend", "bucket_name", bucket.Name)

				if errorsLeft > 0 {
					continue
				}

				break WAIT
			}

			go func() {
				wg.Wait()

				if err := bucketCache.Delete(string(bucket.UID)); err != nil && !errors.Is(err, bigcache.ErrEntryNotFound) {
					c.log.Info("Failed to delete bucket from cache", "bucket_name", bucket.Name)
				}
			}()

			return managed.ExternalCreation{}, nil
		}
	}

	return managed.ExternalCreation{}, err
}

func (c *external) Update(ctx context.Context, mg resource.Managed) (managed.ExternalUpdate, error) {
	bucket, ok := mg.(*v1alpha1.Bucket)
	if !ok {
		return managed.ExternalUpdate{}, errors.New(errNotBucket)
	}

	latestVersion, err := bucketCache.Get(string(bucket.UID))
	if latestVersion != nil || !errors.Is(err, bigcache.ErrEntryNotFound) {
		c.log.Info("Bucket creation in progress", "bucket_name", bucket.Name, "error", err)

		return managed.ExternalUpdate{}, errors.New(errBucketCreationInProgress)
	}

	if utils.IsHealthCheckBucket(bucket) {
		c.log.Info("Update is NOOP for health check bucket - updates performed by heath-check-controller", "bucket", bucket.Name)

		return managed.ExternalUpdate{}, nil
	}

	if bucket.Spec.Disabled {
		c.log.Info("Bucket is disabled - remove any existing buckets from backends", "bucket name", bucket.Name)

		return managed.ExternalUpdate{}, c.Delete(ctx, mg)
	}

	if err := c.updateAll(ctx, bucket); err != nil {
		return managed.ExternalUpdate{}, err
	}

	bucket.Status.SetConditions(xpv1.Available())

	return managed.ExternalUpdate{
		// Optionally return any details that may be required to connect to the
		// external resource. These will be stored as the connection secret.
		ConnectionDetails: managed.ConnectionDetails{},
	}, nil
}

func (c *external) updateAll(ctx context.Context, bucket *v1alpha1.Bucket) error {
	bucketBackends := newBucketBackends()
	defer c.setBucketStatus(bucket, bucketBackends)

	g := new(errgroup.Group)

	activeBackends := c.backendStore.GetActiveBackends(bucket.Spec.Providers)
	if len(activeBackends) == 0 {
		return errors.New(errNoS3BackendsRegistered)
	} else if len(activeBackends) != len(bucket.Spec.Providers) {
		return errors.New(errMissingS3Backend)
	}

	for backendName := range activeBackends {
		if !c.backendStore.IsBackendActive(backendName) {
			c.log.Info("Backend is marked inactive - bucket will not be updated on backend", "bucket name", bucket.Name, "backend name", backendName)

			continue
		}

		cl := c.backendStore.GetBackendClient(backendName)
		if cl == nil {
			c.log.Info("Backend client not found for backend - bucket cannot be updated on backend", "bucket name", bucket.Name, "backend name", backendName)

			continue
		}

		c.log.Info("Updating bucket", "bucket name", bucket.Name, "backend name", backendName)

		beName := backendName
		g.Go(func() error {
			bucketBackends.setBucketBackendStatus(bucket.Name, beName, v1alpha1.BackendNotReadyStatus)

			for i := 0; i < s3internal.RequestRetries; i++ {
				bucketExists, err := s3internal.BucketExists(ctx, cl, bucket.Name)
				if err != nil {
					return err
				}
				if !bucketExists {
					bucketBackends.deleteBucketBackend(bucket.Name, beName)

					return nil
				}

				bucketBackends.setBucketBackendStatus(bucket.Name, beName, v1alpha1.BackendNotReadyStatus)

				err = c.update(ctx, bucket, cl)
				if err == nil {
					// Check to see if this backend has been marked as 'Unhealthy'. It may be 'Unknown' due to
					// the healthcheck being disabled. In which case we can only assume the backend is healthy
					// and mark the bucket as 'Ready' for this backend.
					if c.backendStore.GetBackendHealthStatus(beName) == apisv1alpha1.HealthStatusUnhealthy {
						break
					}

					bucketBackends.setBucketBackendStatus(bucket.Name, beName, v1alpha1.BackendReadyStatus)
				}
			}

			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return errors.Wrap(err, errUpdateBucket)
	}

	return nil
}

func (c *external) update(ctx context.Context, bucket *v1alpha1.Bucket, s3Backend *s3.Client) error {
	if s3types.ObjectOwnership(aws.ToString(bucket.Spec.ForProvider.ObjectOwnership)) == s3types.ObjectOwnershipBucketOwnerEnforced {
		_, err := s3Backend.PutBucketAcl(ctx, s3internal.BucketToPutBucketACLInput(bucket))
		if err != nil {
			return err
		}
	}

	//TODO: Add functionality for bucket ownership controls, using s3 apis:
	// - DeleteBucketOwnershipControls
	// - PutBucketOwnershipControls
	if controllerutil.AddFinalizer(bucket, inUseFinalizer) {
		// we need to update the object to add the finalizer otherwise it is only added
		// to the object's managed fields and does not block deletion.
		return c.kubeClient.Update(ctx, bucket)
	}

	return nil
}

func (c *external) Delete(ctx context.Context, mg resource.Managed) error {
	bucket, ok := mg.(*v1alpha1.Bucket)
	if !ok {
		return errors.New(errNotBucket)
	}

	if utils.IsHealthCheckBucket(bucket) {
		c.log.Info("Delete is NOOP for health check bucket as it is owned by, and garbage collected on deletion of its related providerconfig", "bucket", bucket.Name)

		return nil
	}

	// There are two scenarios where the bucket status needs to be updated during a
	// Delete invocation:
	// 1. The caller attempts to delete the CR and an error occurs during the call to
	// the bucket's backends. In this case the bucket may be successfully deleted
	// from some backends, but not from others. As such, we must update the bucket CR
	// status accordingly as Delete has ultimately failed and the 'in-use' finalizer
	// will not be removed.
	// 2. The caller attempts to delete the bucket from it's backends without deleting
	// the bucket CR. This is done by setting the Disabled flag on the bucket
	// CR spec. If the deletion is successful or unsuccessful, the bucket CR status must be
	// updated.
	bucketBackends := newBucketBackends()
	defer c.setBucketStatus(bucket, bucketBackends)

	if !c.backendStore.BackendsAreStored() {
		return errors.New(errNoS3BackendsStored)
	}

	bucket.Status.SetConditions(xpv1.Deleting())

	g := new(errgroup.Group)

	activeBackends := bucket.Spec.Providers
	if len(activeBackends) == 0 {
		activeBackends = c.backendStore.GetAllActiveBackendNames()
	}

	for _, backendName := range activeBackends {
		bucketBackends.setBucketBackendStatus(bucket.Name, backendName, v1alpha1.BackendDeletingStatus)

		c.log.Info("Deleting bucket", "bucket name", bucket.Name, "backend name", backendName)
		cl := c.backendStore.GetBackendClient(backendName)
		beName := backendName
		g.Go(func() error {
			var err error
			for i := 0; i < s3internal.RequestRetries; i++ {
				if err = s3internal.DeleteBucket(ctx, cl, aws.String(bucket.Name)); err != nil {
					break
				}
				bucketBackends.deleteBucketBackend(bucket.Name, beName)
			}

			return err
		})
	}

	if err := g.Wait(); err != nil {
		return errors.Wrap(err, errDeleteBucket)
	}

	// update object to remove in-use finalizer and allow deletion
	if controllerutil.RemoveFinalizer(bucket, inUseFinalizer) {
		// we need to update the object to add the finalizer otherwise it is only added
		// to the object's managed fields and does not block deletion.
		return c.kubeClient.Update(ctx, bucket)
	}

	return nil
}

// isAlreadyExists helper function to test for ErrCodeBucketAlreadyOwnedByYou error
func isAlreadyExists(err error) bool {
	var alreadyOwnedByYou *s3types.BucketAlreadyOwnedByYou

	return errors.As(err, &alreadyOwnedByYou)
}

func (c *external) setBucketStatus(bucket *v1alpha1.Bucket, bucketBackends *bucketBackends) {
	bucket.Status.SetConditions(xpv1.Unavailable())
	bucketBackendStatuses := bucketBackends.getBucketBackendStatuses(bucket.Name, bucket.Spec.Providers)
	bucket.Status.AtProvider.BackendStatuses = bucketBackendStatuses
	for _, backendStatus := range bucketBackendStatuses {
		if backendStatus == v1alpha1.BackendReadyStatus {
			bucket.Status.SetConditions(xpv1.Available())

			break
		}
	}
}
