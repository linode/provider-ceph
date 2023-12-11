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

package v1alpha1

import (
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
)

// A ProviderConfigSpec defines the desired state of a ProviderConfig.
type ProviderConfigSpec struct {
	// Credentials required to authenticate to this provider.
	Credentials ProviderCredentials `json:"credentials"`

	// HostBase url specified in s3cfg.
	HostBase string `json:"hostBase"`

	// STSAddress is a separate url for an optional external authenticator service.
	// This service should be able to handle the AssumeRole S3 API call.
	// If unset, STSAddress defaults to that of HostBase.
	// +optional
	STSAddress *string `json:"stsAddress,omitempty"`

	// HostBucket url specified in s3cfg.
	HostBucket string `json:"hostBucket,omitempty"`

	// UseHTTPS ceph cluster configuration.
	UseHTTPS bool `json:"useHttps,omitempty"`

	DisableHealthCheck bool `json:"disableHealthCheck,omitempty"`

	// +kubebuilder:validation:Minimum:=2
	// +kubebuilder:default:=30
	HealthCheckIntervalSeconds int32 `json:"healthCheckIntervalSeconds,omitempty"`
}

// ProviderCredentials required to authenticate.
type ProviderCredentials struct {
	xpv1.CommonCredentialSelectors `json:",inline"`
	// Source of the provider credentials.
	// +kubebuilder:validation:Enum=None;Secret;InjectedIdentity;Environment;Filesystem
	Source xpv1.CredentialsSource `json:"source"`
}

type HealthStatus string

const (
	HealthStatusHealthy   = "Healthy"
	HealthStatusUnhealthy = "Unhealthy"
	HealthStatusUnknown   = "Unknown"
)

// A ProviderConfigStatus reflects the observed state of a ProviderConfig.
type ProviderConfigStatus struct {

	// Health of the s3 backend represented by the ProviderConfig determined
	// by periodic health check.
	// +kubebuilder:validation:Enum=Healthy;Unhealthy;Unknown
	// Deprecated: Use ProviderConfogStatus.ConditionedStatus instead.
	// This field will be removed in a future release.
	Health HealthStatus `json:"health,omitempty"`
	// Deprecated: Use ProviderConfogStatus.ConditionedStatus instead.
	// This field will be removed in a future release.
	Reason                    string `json:"reason,omitempty"`
	xpv1.ProviderConfigStatus `json:",inline"`
}

// +kubebuilder:object:root=true

// A ProviderConfig configures a Ceph provider.
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="SECRET-NAME",type="string",JSONPath=".spec.credentials.secretRef.name",priority=1
// +kubebuilder:resource:scope=Cluster
type ProviderConfig struct {
	Status            ProviderConfigStatus `json:"status,omitempty"`
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec ProviderConfigSpec `json:"spec"`
}

// +kubebuilder:object:root=true

// ProviderConfigList contains a list of ProviderConfig.
type ProviderConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ProviderConfig `json:"items"`
}

// ProviderConfig type metadata.
var (
	ProviderConfigKind             = reflect.TypeOf(ProviderConfig{}).Name()
	ProviderConfigGroupKind        = schema.GroupKind{Group: Group, Kind: ProviderConfigKind}.String()
	ProviderConfigKindAPIVersion   = ProviderConfigKind + "." + SchemeGroupVersion.String()
	ProviderConfigGroupVersionKind = SchemeGroupVersion.WithKind(ProviderConfigKind)
)

func init() {
	SchemeBuilder.Register(&ProviderConfig{}, &ProviderConfigList{})
}
