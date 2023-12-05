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

package backendmonitor

import (
	"context"

	"github.com/crossplane/crossplane-runtime/pkg/errors"
	corev1 "k8s.io/api/core/v1"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	s3internal "github.com/linode/provider-ceph/internal/s3"
)

const (
	errCreateClient = "cannot create s3 client"
)

func (c *Controller) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	c.log.Info("Reconciling backend store", "name", req.Name)
	providerConfig := &apisv1alpha1.ProviderConfig{}
	if err := c.kubeClient.Get(ctx, req.NamespacedName, providerConfig); err != nil {
		if kerrors.IsNotFound(err) {
			c.log.Info("Marking s3 backend as inactive on backend store", "name", req.Name)
			c.backendStore.ToggleBackendActiveStatus(req.Name, false)
			c.backendStore.SetBackendHealthStatus(req.Name, apisv1alpha1.HealthStatusUnknown)

			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, err
	}
	// ProviderConfig has been created or updated, add or
	// update its backend in the backend store.
	return ctrl.Result{}, c.addOrUpdateBackend(ctx, providerConfig)
}

func (c *Controller) addOrUpdateBackend(ctx context.Context, pc *apisv1alpha1.ProviderConfig) error {
	secret, err := c.getProviderConfigSecret(ctx, pc.Spec.Credentials.SecretRef.Namespace, pc.Spec.Credentials.SecretRef.Name)
	if err != nil {
		return err
	}

	s3client, err := s3internal.NewClient(ctx, secret.Data, &pc.Spec)
	if err != nil {
		return errors.Wrap(err, errCreateClient)
	}

	var health apisv1alpha1.HealthStatus
	health = apisv1alpha1.HealthStatusUnknown
	if pc.Status.Health != "" {
		health = pc.Status.Health
	}

	c.backendStore.AddOrUpdateBackend(pc.Name, s3client, true, health)

	return nil
}

func (c *Controller) getProviderConfigSecret(ctx context.Context, secretNamespace, secretName string) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	ns := types.NamespacedName{Namespace: secretNamespace, Name: secretName}
	if err := c.kubeClient.Get(ctx, ns, secret); err != nil {
		return nil, errors.Wrap(err, "cannot get provider secret")
	}

	return secret, nil
}
