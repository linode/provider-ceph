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

package bucket

import (
	"context"

	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	"github.com/go-logr/logr"
	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	"github.com/linode/provider-ceph/internal/controller/s3clienthandler"
)

// SubresourceClient is the interface all Bucket sub-resources must conform to.
type SubresourceClient interface {
	Observe(ctx context.Context, bucket *v1alpha1.Bucket, backendNames []string) (ResourceStatus, error)
	Handle(ctx context.Context, bucket *v1alpha1.Bucket, backendName string, bb *bucketBackends) error
}

// NewSubresourceClients creates the array of all sub resource clients.
func NewSubresourceClients(b *backendstore.BackendStore, h *s3clienthandler.Handler, config SubresourceClientConfig, l logr.Logger) []SubresourceClient {
	subresourceClients := make([]SubresourceClient, 0)

	if !config.LifecycleConfigurationClientDisabled {
		subresourceClients = append(subresourceClients, NewLifecycleConfigurationClient(b, h, l.WithValues("lifecycle-configuration-client", managed.ControllerName(v1alpha1.BucketGroupKind))))
	}
	if !config.ACLClientDisabled {
		subresourceClients = append(subresourceClients, NewACLClient(b, h, l.WithValues("acl-client", managed.ControllerName(v1alpha1.BucketGroupKind))))
	}
	if !config.PolicyClientDisabled {
		subresourceClients = append(subresourceClients, NewPolicyClient(b, h, l.WithValues("policy-client", managed.ControllerName(v1alpha1.BucketGroupKind))))
	}
	if !config.VersioningConfigurationClientDisabled {
		subresourceClients = append(subresourceClients, NewVersioningConfigurationClient(b, h, l.WithValues("versioning-configuration-client", managed.ControllerName(v1alpha1.BucketGroupKind))))
	}
	if !config.ObjectLockConfigurationClientDisabled {
		subresourceClients = append(subresourceClients, NewObjectLockConfigurationClient(b, h, l.WithValues("object-lock-configuration-client", managed.ControllerName(v1alpha1.BucketGroupKind))))
	}

	return subresourceClients
}

// ResourceStatus represents the current status of the resource.
type ResourceStatus int

const (
	// NoAction is returned if the resource requires no action.
	NoAction ResourceStatus = iota
	// Updated is returned if the resource is updated.
	Updated
	// NeedsUpdate is returned if the resource required updating.
	NeedsUpdate
	// NeedsDeletion is returned if the resource needs to be deleted.
	NeedsDeletion
)

type SubresourceClientConfig struct {
	LifecycleConfigurationClientDisabled  bool
	ACLClientDisabled                     bool
	PolicyClientDisabled                  bool
	VersioningConfigurationClientDisabled bool
	ObjectLockConfigurationClientDisabled bool
}
