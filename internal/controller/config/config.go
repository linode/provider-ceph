/*
Copyright 2020 The Crossplane Authors.

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

package config

import (
	"context"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/crossplane/crossplane-runtime/pkg/controller"
	"github.com/crossplane/crossplane-runtime/pkg/event"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/ratelimiter"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/providerconfig"
	"github.com/crossplane/crossplane-runtime/pkg/resource"

	apisv1alpha1 "github.com/crossplane/provider-ceph/apis/v1alpha1"
	"github.com/crossplane/provider-ceph/internal/backendstore"
	s3internal "github.com/crossplane/provider-ceph/internal/s3"
)

const (
	errCreateClient = "cannot create s3 client"
	errGetSecret    = "cannot get Secret"
)

// Setup adds a controller that reconciles ProviderConfigs by accounting for
// their current usage.
func Setup(mgr ctrl.Manager, o controller.Options, s *backendstore.BackendStore) error {
	name := providerconfig.ControllerName(apisv1alpha1.ProviderConfigGroupKind)

	of := resource.ProviderConfigKinds{
		Config:    apisv1alpha1.ProviderConfigGroupVersionKind,
		UsageList: apisv1alpha1.ProviderConfigUsageListGroupVersionKind,
	}

	// Add an 'internal' controller to the manager for the ProviderConfig.
	// This will be used, initially, to manage the backendstore of s3 clients.
	if err := newReconciler(mgr.GetClient(), o, s).setupWithManager(mgr); err != nil {
		return err
	}

	r := providerconfig.NewReconciler(mgr, of,
		providerconfig.WithLogger(o.Logger.WithValues("controller", name)),
		providerconfig.WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorderFor(name))))

	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		WithOptions(o.ForControllerRuntime()).
		For(&apisv1alpha1.ProviderConfig{}).
		Watches(&source.Kind{Type: &apisv1alpha1.ProviderConfigUsage{}}, &resource.EnqueueRequestForProviderConfig{}).
		Complete(ratelimiter.NewReconciler(name, r, o.GlobalRateLimiter))
}

func newReconciler(k client.Client, o controller.Options, s *backendstore.BackendStore) *Reconciler {
	return &Reconciler{
		kube:         k,
		backendStore: s,
		log:          o.Logger.WithValues("internal-controller", providerconfig.ControllerName(apisv1alpha1.ProviderConfigGroupKind)),
	}
}

type Reconciler struct {
	kube         client.Client
	backendStore *backendstore.BackendStore
	log          logging.Logger
}

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.log.Info("Reconciling object", "name", req.Name)
	providerConfig := &apisv1alpha1.ProviderConfig{}
	if err := r.kube.Get(ctx, req.NamespacedName, providerConfig); err != nil {
		if kerrors.IsNotFound(err) {
			r.log.Info("Deleting s3 backend from backend store", "name", req.Name)
			r.backendStore.DeleteBackend(req.Name)

			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, err
	}
	// ProviderConfig has been created or updated, add or
	// update its backend in the backend store.
	r.log.Info("Adding s3 backend to backend store", "name", req.Name)

	return ctrl.Result{}, r.addOrUpdateBackend(ctx, providerConfig)
}

func (r *Reconciler) addOrUpdateBackend(ctx context.Context, pc *apisv1alpha1.ProviderConfig) error {
	secret, err := r.getProviderConfigSecret(ctx, pc.Spec.Credentials.SecretRef.Namespace, pc.Spec.Credentials.SecretRef.Name)
	if err != nil {
		return err
	}

	s3client, err := s3internal.NewClient(ctx, secret.Data, &pc.Spec)
	if err != nil {
		return errors.Wrap(err, errCreateClient)
	}

	r.backendStore.AddOrUpdateBackend(pc.Name, s3client)

	return nil
}

func (r *Reconciler) getProviderConfigSecret(ctx context.Context, secretNamespace, secretName string) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	ns := types.NamespacedName{Namespace: secretNamespace, Name: secretName}
	if err := r.kube.Get(ctx, ns, secret); err != nil {
		return nil, errors.Wrap(err, "cannot get provider secret")
	}

	return secret, nil
}

func (r *Reconciler) setupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&apisv1alpha1.ProviderConfig{}).
		Complete(r)
}
