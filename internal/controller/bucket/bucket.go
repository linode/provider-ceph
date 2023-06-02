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

	"golang.org/x/sync/errgroup"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/pkg/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
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
	s3internal "github.com/linode/provider-ceph/internal/s3"
)

const (
	errNotBucket            = "managed resource is not a Bucket custom resource"
	errTrackPCUsage         = "cannot track ProviderConfig usage"
	errGetPC                = "cannot get ProviderConfig"
	errListPC               = "cannot list ProviderConfigs"
	errGetBucket            = "cannot get Bucket"
	errListBuckets          = "cannot list Buckets"
	errCreateBucket         = "cannot create Bucket"
	errDeleteBucket         = "cannot delete Bucket"
	errUpdateBucket         = "cannot update Bucket"
	errGetCreds             = "cannot get credentials"
	errBackendNotStored     = "s3 backend is not stored"
	errNoS3BackendsStored   = "no s3 backends stored"
	errCodeBucketNotFound   = "NotFound"
	errFailedToCreateClient = "failed to create s3 client"

	defaultPC = "default"

	requestRetries = 5
)

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

	return &external{backendStore: c.backendStore.GetBackendStore(), log: c.log}, nil
}

// An ExternalClient observes, then either creates, updates, or deletes an
// external resource to ensure it reflects the managed resource's desired state.
type external struct {
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

	// Check for the bucket on each backend in a separate go routine
	allBackends := c.backendStore.GetAllBackends()
	for _, s3Backend := range allBackends {
		go func(backend *s3.Client, bucketName string) {
			bucketExists, err := c.bucketExists(ctxC, backend, bucketName)
			bucketExistsResults <- bucketExistsResult{bucketExists, err}
		}(s3Backend, bucket.Name)
	}

	// Wait for any go routine to finish, if the bucket exists anywhere
	// return 'ResourceExists: true' as resulting calls to Create or Delete
	// are idempotent.
	for i := 0; i < len(allBackends); i++ {
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
		ResourceExists: false,
	}, nil
}

func (c *external) Create(ctx context.Context, mg resource.Managed) (managed.ExternalCreation, error) {
	bucket, ok := mg.(*v1alpha1.Bucket)
	if !ok {
		return managed.ExternalCreation{}, errors.New(errNotBucket)
	}

	backends := newBackendStatuses()

	bucket.Status.SetConditions(xpv1.Creating())
	defer setBucketStatus(bucket, backends.getBackendStatuses())

	// Where a bucket has a ProviderConfigReference Name, we can infer that this bucket is to be
	// created only on this S3 Backend. An empty config reference name will be automatically set
	// to "default".
	if bucket.GetProviderConfigReference() != nil && bucket.GetProviderConfigReference().Name != defaultPC {
		return c.create(ctx, bucket, backends)
	}

	// No ProviderConfigReference Name specified for bucket, we can infer that this bucket is to
	// be created on all S3 Backends.
	return c.createAll(ctx, bucket, backends)
}

func (c *external) create(ctx context.Context, bucket *v1alpha1.Bucket, backends *backendStatuses) (managed.ExternalCreation, error) {
	s3Backend, err := c.getStoredBackend(bucket.GetProviderConfigReference().Name)
	if err != nil {
		return managed.ExternalCreation{}, err
	}

	backends.setBackendStatus(bucket.GetProviderConfigReference().Name, v1alpha1.BackendNotReadyStatus)

	c.log.Info("Creating bucket on single s3 backend", "bucket name", bucket.Name, "backend name", bucket.GetProviderConfigReference().Name)
	_, err = s3Backend.CreateBucket(ctx, s3internal.BucketToCreateBucketInput(bucket))
	if resource.Ignore(isAlreadyExists, err) != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, errCreateBucket)
	}

	backends.setBackendStatus(bucket.GetProviderConfigReference().Name, v1alpha1.BackendReadyStatus)

	return managed.ExternalCreation{}, nil
}

func (c *external) createAll(ctx context.Context, bucket *v1alpha1.Bucket, backends *backendStatuses) (managed.ExternalCreation, error) {
	if !c.backendStore.BackendsAreStored() {
		return managed.ExternalCreation{}, errors.New(errNoS3BackendsStored)
	}

	c.log.Info("Creating bucket on all available s3 backends", "bucket name", bucket.Name)

	g := new(errgroup.Group)

	// Create the bucket on each backend in a separate go routine
	allBackends := c.backendStore.GetAllBackends()
	for beName, client := range allBackends {
		bn := beName
		cl := client
		bucket := bucket

		g.Go(func() (err error) {
			backends.setBackendStatus(bn, v1alpha1.BackendNotReadyStatus)
			for i := 0; i < requestRetries; i++ {
				_, err = cl.CreateBucket(ctx, s3internal.BucketToCreateBucketInput(bucket))
				if resource.Ignore(isAlreadyExists, err) == nil {
					backends.setBackendStatus(bn, v1alpha1.BackendReadyStatus)

					break
				}
			}

			return err
		})
	}

	err := g.Wait()
	if err != nil {
		// Bucket could not be created on any backend. Return the error
		// so that the operation can be retried.
		return managed.ExternalCreation{}, errors.Wrap(err, errCreateBucket)
	}

	return managed.ExternalCreation{}, nil
}

func (c *external) Update(ctx context.Context, mg resource.Managed) (managed.ExternalUpdate, error) {
	bucket, ok := mg.(*v1alpha1.Bucket)
	if !ok {
		return managed.ExternalUpdate{}, errors.New(errNotBucket)
	}

	c.log.Info("Updating bucket on all available s3 backends", "bucket name", bucket.Name)

	g := new(errgroup.Group)

	backends := newBackendStatuses()
	if bucket.Status.AtProvider.BackendStatuses != nil {
		backends = newBackendStatusesWithExisting(bucket.Status.AtProvider.BackendStatuses)
	}
	defer setBucketStatus(bucket, backends.getBackendStatuses())

	for backendName, s3Backend := range c.backendStore.GetAllBackends() {
		backend := s3Backend
		beName := backendName
		g.Go(func() error {
			backends.setBackendStatus(beName, v1alpha1.BackendNotReadyStatus)
			for i := 0; i < requestRetries; i++ {
				bucketExists, err := c.bucketExists(ctx, backend, bucket.Name)
				if err != nil {
					return err
				}
				if !bucketExists {
					backends.deleteBackendFromStatuses(beName)

					return nil
				}

				backends.setBackendStatus(beName, v1alpha1.BackendNotReadyStatus)

				err = c.update(ctx, bucket, backend)
				if err == nil {
					backends.setBackendStatus(beName, v1alpha1.BackendReadyStatus)

					break
				}
			}

			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return managed.ExternalUpdate{}, errors.Wrap(err, errUpdateBucket)
	}

	bucket.Status.SetConditions(xpv1.Available())

	return managed.ExternalUpdate{
		// Optionally return any details that may be required to connect to the
		// external resource. These will be stored as the connection secret.
		ConnectionDetails: managed.ConnectionDetails{},
	}, nil
}

func (c *external) update(ctx context.Context, bucket *v1alpha1.Bucket, s3Backend *s3.Client) error {
	if s3types.ObjectOwnership(aws.ToString(bucket.Spec.ForProvider.ObjectOwnership)) == s3types.ObjectOwnershipBucketOwnerEnforced {
		_, err := s3Backend.PutBucketAcl(ctx, s3internal.BucketToPutBucketACLInput(bucket))
		if err != nil {
			return err
		}
	}

	if bucket.Spec.ForProvider.ObjectOwnership == nil {
		_, err := s3Backend.DeleteBucketOwnershipControls(ctx, &s3.DeleteBucketOwnershipControlsInput{
			Bucket: aws.String(bucket.Name),
		})
		if err != nil {
			return err
		}

		return nil
	}

	_, err := s3Backend.PutBucketOwnershipControls(ctx, s3internal.BucketToPutBucketOwnershipControlsInput(bucket))

	return err
}

func (c *external) Delete(ctx context.Context, mg resource.Managed) error {
	bucket, ok := mg.(*v1alpha1.Bucket)
	if !ok {
		return errors.New(errNotBucket)
	}

	if !c.backendStore.BackendsAreStored() {
		return errors.New(errNoS3BackendsStored)
	}

	bucket.Status.SetConditions(xpv1.Deleting())

	c.log.Info("Deleting bucket on all available s3 backends", "bucket name", bucket.Name)

	g := new(errgroup.Group)
	for _, client := range c.backendStore.GetAllBackends() {
		cl := client
		g.Go(func() error {
			var err error
			for i := 0; i < requestRetries; i++ {
				_, err := cl.DeleteBucket(ctx, &s3.DeleteBucketInput{Bucket: aws.String(bucket.Name)})
				if resource.Ignore(isNotFound, err) == nil {
					break
				}
			}

			return err
		})
	}

	if err := g.Wait(); err != nil {
		return errors.Wrap(err, errDeleteBucket)
	}

	return nil
}

func (c *external) bucketExists(ctx context.Context, s3Backend *s3.Client, bucketName string) (bool, error) {
	_, err := s3Backend.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(bucketName)})
	if err != nil {
		if isNotFound(err) {
			return false, nil
		}
		// Some other error occurred, return false with error
		// as we cannot verify the bucket exists.
		return false, err
	}
	// Bucket exists, return true with no error.
	return true, nil
}

func (c *external) getStoredBackend(s3BackendName string) (*s3.Client, error) {
	s3Backend := c.backendStore.GetBackend(s3BackendName)
	if s3Backend != nil {
		return s3Backend, nil
	}

	return nil, errors.New(errBackendNotStored)
}

// isNotFound helper function to test for NotFound error
func isNotFound(err error) bool {
	var notFoundError *s3types.NotFound

	return errors.As(err, &notFoundError)
}

// isAlreadyExists helper function to test for ErrCodeBucketAlreadyOwnedByYou error
func isAlreadyExists(err error) bool {
	var alreadyOwnedByYou *s3types.BucketAlreadyOwnedByYou

	return errors.As(err, &alreadyOwnedByYou)
}

func setBucketStatus(bucket *v1alpha1.Bucket, statuses v1alpha1.BackendStatuses) {
	bucket.Status.SetConditions(xpv1.Unavailable())
	bucket.Status.AtProvider.BackendStatuses = statuses

	for _, status := range bucket.Status.AtProvider.BackendStatuses {
		if status == v1alpha1.BackendReadyStatus {
			bucket.Status.SetConditions(xpv1.Available())

			break
		}
	}
}
