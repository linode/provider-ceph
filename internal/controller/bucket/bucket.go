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

	corev1 "k8s.io/api/core/v1"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane/crossplane-runtime/pkg/connection"
	"github.com/crossplane/crossplane-runtime/pkg/controller"
	"github.com/crossplane/crossplane-runtime/pkg/event"
	"github.com/crossplane/crossplane-runtime/pkg/ratelimiter"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/pkg/resource"

	"github.com/crossplane/provider-ceph/apis/provider-ceph/v1alpha1"
	apisv1alpha1 "github.com/crossplane/provider-ceph/apis/v1alpha1"
	"github.com/crossplane/provider-ceph/internal/controller/features"
	s3client "github.com/crossplane/provider-ceph/internal/s3"
)

const (
	errNotBucket    = "managed resource is not a Bucket custom resource"
	errTrackPCUsage = "cannot track ProviderConfig usage"
	errGetPC        = "cannot get ProviderConfig"
	errGetCreds     = "cannot get credentials"
)

// A NoOpService does nothing.
type NoOpService struct{}

var (
	newNoOpService = func(_ []byte) (interface{}, error) { return &NoOpService{}, nil }
)

// Setup adds a controller that reconciles Bucket managed resources.
func Setup(mgr ctrl.Manager, o controller.Options) error {
	name := managed.ControllerName(v1alpha1.BucketGroupKind)

	cps := []managed.ConnectionPublisher{managed.NewAPISecretPublisher(mgr.GetClient(), mgr.GetScheme())}
	if o.Features.Enabled(features.EnableAlphaExternalSecretStores) {
		cps = append(cps, connection.NewDetailsManager(mgr.GetClient(), apisv1alpha1.StoreConfigGroupVersionKind))
	}

	r := managed.NewReconciler(mgr,
		resource.ManagedKind(v1alpha1.BucketGroupVersionKind),
		managed.WithExternalConnecter(&connector{
			kube:         mgr.GetClient(),
			usage:        resource.NewProviderConfigUsageTracker(mgr.GetClient(), &apisv1alpha1.ProviderConfigUsage{}),
			newServiceFn: newNoOpService}),
		managed.WithLogger(o.Logger.WithValues("controller", name)),
		managed.WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorderFor(name))),
		managed.WithConnectionPublishers(cps...))

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
}

// Connect typically produces an ExternalClient by:
// 1. Tracking that the managed resource is using a ProviderConfig.
// 2. Getting the managed resource's ProviderConfig.
// 3. Getting the credentials specified by the ProviderConfig.
// 4. Using the credentials to form a client.
func (c *connector) Connect(ctx context.Context, mg resource.Managed) (managed.ExternalClient, error) {
	cr, ok := mg.(*v1alpha1.Bucket)
	if !ok {
		return nil, errors.New(errNotBucket)
	}

	if err := c.usage.Track(ctx, mg); err != nil {
		return nil, errors.Wrap(err, errTrackPCUsage)
	}

	pc := &apisv1alpha1.ProviderConfig{}
	if err := c.kube.Get(ctx, types.NamespacedName{Name: cr.GetProviderConfigReference().Name}, pc); err != nil {
		return nil, errors.Wrap(err, errGetPC)
	}

	// Get the Secret referenced by the Provider.
	secret := &corev1.Secret{}
	ns := types.NamespacedName{Namespace: pc.Spec.Credentials.SecretRef.Namespace, Name: pc.Spec.Credentials.SecretRef.Name}
	if err := c.kube.Get(ctx, ns, secret); err != nil {
		return nil, errors.Wrap(err, "cannot get Provider secret")
	}

	return &external{s3Client: s3client.NewClient(secret.Data, &pc.Spec)}, nil
}

// An ExternalClient observes, then either creates, updates, or deletes an
// external resource to ensure it reflects the managed resource's desired state.
type external struct {
	// A 'client' used to connect to the external resource API. In practice this
	// would be something like an AWS SDK client.
	s3Client *s3.S3
}

func (c *external) Observe(ctx context.Context, mg resource.Managed) (managed.ExternalObservation, error) {
	cr, ok := mg.(*v1alpha1.Bucket)
	if !ok {
		return managed.ExternalObservation{}, errors.New(errNotBucket)
	}

	resp, err := c.s3Client.ListBucketsWithContext(ctx, nil)
	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, "unable to list buckets using s3 client")
	}

	for _, bucket := range resp.Buckets {
		if cr.Name == *bucket.Name {
			return managed.ExternalObservation{
				// Return false when the external resource does not exist. This lets
				// the managed resource reconciler know that it needs to call Create to
				// (re)create the resource, or that it has successfully been deleted.
				ResourceExists: true,

				// Return false when the external resource exists, but it not up to date
				// with the desired managed resource state. This lets the managed
				// resource reconciler know that it needs to call Update.
				ResourceUpToDate: true,

				// Return any details that may be required to connect to the external
				// resource. These will be stored as the connection secret.
				ConnectionDetails: managed.ConnectionDetails{},
			}, nil

		}
	}

	return managed.ExternalObservation{
		// Return false when the external resource does not exist. This lets
		// the managed resource reconciler know that it needs to call Create to
		// (re)create the resource, or that it has successfully been deleted.
		ResourceExists: false,
	}, nil
}

func (c *external) Create(ctx context.Context, mg resource.Managed) (managed.ExternalCreation, error) {
	cr, ok := mg.(*v1alpha1.Bucket)
	if !ok {
		return managed.ExternalCreation{}, errors.New(errNotBucket)
	}

	_, err := c.s3Client.CreateBucketWithContext(ctx, &s3.CreateBucketInput{Bucket: aws.String(cr.Name)})
	if err != nil {
		return managed.ExternalCreation{}, err
	}

	return managed.ExternalCreation{
		// Optionally return any details that may be required to connect to the
		// external resource. These will be stored as the connection secret.
		ConnectionDetails: managed.ConnectionDetails{},
	}, nil
}

func (c *external) Update(ctx context.Context, mg resource.Managed) (managed.ExternalUpdate, error) {
	_, ok := mg.(*v1alpha1.Bucket)
	if !ok {
		return managed.ExternalUpdate{}, errors.New(errNotBucket)
	}

	return managed.ExternalUpdate{
		// Optionally return any details that may be required to connect to the
		// external resource. These will be stored as the connection secret.
		ConnectionDetails: managed.ConnectionDetails{},
	}, nil
}

func (c *external) Delete(ctx context.Context, mg resource.Managed) error {
	cr, ok := mg.(*v1alpha1.Bucket)
	if !ok {
		return errors.New(errNotBucket)
	}

	_, err := c.s3Client.DeleteBucketWithContext(ctx, &s3.DeleteBucketInput{Bucket: aws.String(cr.Name)})
	if err != nil {
		return err
	}

	return nil
}
