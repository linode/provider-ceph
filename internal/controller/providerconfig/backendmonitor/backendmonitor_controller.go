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

	"github.com/aws/aws-sdk-go-v2/aws"
	v1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"go.opentelemetry.io/otel"
	corev1 "k8s.io/api/core/v1"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	"github.com/linode/provider-ceph/internal/consts"
	"github.com/linode/provider-ceph/internal/otel/traces"
	"github.com/linode/provider-ceph/internal/rgw"
	"github.com/linode/provider-ceph/internal/utils"
)

const (
	errCreateS3Client           = "failed create s3 client"
	errCreateSTSClient          = "failed create sts client"
	errGetProviderConfig        = "failed to get ProviderConfig"
	errGetSecret                = "failed to get Secret"
	errCleanup                  = "failed to perform cleanup"
	errDeleteLCValidationBucket = "failed to delete lifecycle configuration validation bucket"
)

func (c *Controller) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx, span := otel.Tracer("").Start(ctx, "backendmonitor.Controller.Reconcile")
	defer span.End()
	ctx, log := traces.InjectTraceAndLogger(ctx, c.log)

	log.V(1).Info("Reconciling backend store", "name", req.Name)
	providerConfig := &apisv1alpha1.ProviderConfig{}
	if err := c.kubeClient.Get(ctx, req.NamespacedName, providerConfig); err != nil {
		if kerrors.IsNotFound(err) {
			// ProviderConfig has been deleted, perform cleanup.
			if err := c.cleanup(ctx, req); err != nil {
				err = errors.Wrap(err, errCleanup)
				traces.SetAndRecordError(span, err)

				return ctrl.Result{}, err
			}

			log.Info("Removing s3 backend as from backend store", "name", req.Name)
			c.backendStore.DeleteBackend(req.Name)

			// The ProviderConfig no longer exists so there is no need to requeue the reconcile key.
			return ctrl.Result{}, nil
		}
		err = errors.Wrap(err, errGetProviderConfig)
		traces.SetAndRecordError(span, err)

		return ctrl.Result{}, err
	}

	if err := c.addOrUpdateBackend(ctx, providerConfig); err != nil {
		traces.SetAndRecordError(span, err)

		return ctrl.Result{}, err
	}

	// Requeue the reconcile key after the interval. We do this because we need to
	// ensure that if a ProviderConfig's referenced Secret is updated, we also update
	// the client in the backend store with the new credentials.
	return ctrl.Result{RequeueAfter: c.requeueInterval}, nil
}

func (c *Controller) addOrUpdateBackend(ctx context.Context, pc *apisv1alpha1.ProviderConfig) error {
	secret, err := c.getProviderConfigSecret(ctx, pc.Spec.Credentials.SecretRef.Namespace, pc.Spec.Credentials.SecretRef.Name)
	if err != nil {
		return err
	}

	s3Client, err := rgw.NewS3Client(ctx, secret.Data, &pc.Spec, c.s3Timeout, nil)
	if err != nil {
		return errors.Wrap(err, errCreateS3Client)
	}

	stsClient, err := rgw.NewSTSClient(ctx, secret.Data, &pc.Spec, c.s3Timeout)
	if err != nil {
		return errors.Wrap(err, errCreateSTSClient)
	}

	readyCondition := pc.Status.GetCondition(v1.TypeReady)
	c.backendStore.AddOrUpdateBackend(pc.Name, s3Client, stsClient, utils.MapConditionToHealthStatus(readyCondition))

	return nil
}

func (c *Controller) getProviderConfigSecret(ctx context.Context, secretNamespace, secretName string) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	ns := types.NamespacedName{Namespace: secretNamespace, Name: secretName}
	if err := c.kubeClient.Get(ctx, ns, secret); err != nil {
		return nil, errors.Wrap(err, errGetSecret)
	}

	return secret, nil
}

// cleanup deletes the lifecycle configuration validation bucket from the backend.
// This function is only called when a ProviderConfig has been deleted.
func (c *Controller) cleanup(ctx context.Context, req ctrl.Request) error {
	ctx, log := traces.InjectTraceAndLogger(ctx, c.log)

	backendClient := c.backendStore.GetBackendS3Client(req.Name)
	if backendClient == nil {
		log.Info("Backend client not found during validation bucket cleanup - aborting cleanup", consts.KeyBackendName, req.Name)

		return nil
	}

	log.Info("Deleting lifecycle configuration validation bucket", consts.KeyBucketName, v1alpha1.LifecycleConfigValidationBucketName, consts.KeyBackendName, req.Name)
	if err := rgw.DeleteBucket(ctx, backendClient, aws.String(v1alpha1.LifecycleConfigValidationBucketName), true); err != nil {
		return errors.Wrap(err, errDeleteLCValidationBucket)
	}

	return nil
}
