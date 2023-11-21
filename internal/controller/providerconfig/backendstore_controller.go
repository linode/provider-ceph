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

package providerconfig

import (
	"context"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane/crossplane-runtime/pkg/controller"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/providerconfig"

	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	s3internal "github.com/linode/provider-ceph/internal/s3"
)

const (
	errCreateS3Client  = "cannot create s3 client"
	errCreateSTSClient = "cannot create sts client"
	// #nosec
	errGetSecret        = "cannot get Secret"
	errBackendNotStored = "s3 backend is not stored"
)

func newBackendStoreReconciler(k client.Client, o controller.Options, s *backendstore.BackendStore) *BackendStoreReconciler {
	return &BackendStoreReconciler{
		kubeClient:   k,
		backendStore: s,
		log:          o.Logger.WithValues("backend-store-controller", providerconfig.ControllerName(apisv1alpha1.ProviderConfigGroupKind)),
	}
}

type BackendStoreReconciler struct {
	kubeClient   client.Client
	backendStore *backendstore.BackendStore
	log          logging.Logger
}

func (r *BackendStoreReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.log.Info("Reconciling backend store", "name", req.Name)
	providerConfig := &apisv1alpha1.ProviderConfig{}
	if err := r.kubeClient.Get(ctx, req.NamespacedName, providerConfig); err != nil {
		if kerrors.IsNotFound(err) {
			r.log.Info("Marking s3 backend as inactive on backend store", "name", req.Name)
			r.backendStore.ToggleBackendActiveStatus(req.Name, false)
			r.backendStore.SetBackendHealthStatus(req.Name, apisv1alpha1.HealthStatusUnknown)

			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, err
	}
	// ProviderConfig has been created or updated, add or
	// update its backend in the backend store.
	return ctrl.Result{}, r.addOrUpdateBackend(ctx, providerConfig)
}

func (r *BackendStoreReconciler) addOrUpdateBackend(ctx context.Context, pc *apisv1alpha1.ProviderConfig) error {
	secret, err := r.getProviderConfigSecret(ctx, pc.Spec.Credentials.SecretRef.Namespace, pc.Spec.Credentials.SecretRef.Name)
	if err != nil {
		return err
	}

	s3client, err := s3internal.NewS3Client(ctx, secret.Data, pc.Spec.HostBase, pc.Spec.UseHTTPS)
	if err != nil {
		return errors.Wrap(err, errCreateS3Client)
	}

	// If an STSAddress has not been set in the ProviderConfig Spec, use the HostBase.
	// The STSAddress is only necessary if we wish to contact an STS compliant authentication
	// service separate to the HostBase (i.e RGW address).
	stsAddress := pc.Spec.STSAddress
	if stsAddress == "" {
		stsAddress = pc.Spec.HostBase
	}
	stsclient, err := s3internal.NewSTSClient(ctx, secret.Data, stsAddress, pc.Spec.UseHTTPS)
	if err != nil {
		return errors.Wrap(err, errCreateSTSClient)
	}

	var health apisv1alpha1.HealthStatus
	health = apisv1alpha1.HealthStatusUnknown
	if pc.Status.Health != "" {
		health = pc.Status.Health
	}

	r.backendStore.AddOrUpdateBackend(pc.Name, s3client, stsclient, true, health)

	return nil
}

func (r *BackendStoreReconciler) getProviderConfigSecret(ctx context.Context, secretNamespace, secretName string) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	ns := types.NamespacedName{Namespace: secretNamespace, Name: secretName}
	if err := r.kubeClient.Get(ctx, ns, secret); err != nil {
		return nil, errors.Wrap(err, "cannot get provider secret")
	}

	return secret, nil
}

func (r *BackendStoreReconciler) setupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&apisv1alpha1.ProviderConfig{}).
		Complete(r)
}
