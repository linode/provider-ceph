package v1alpha1

import (
	v1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ReasonHealthCheckDisabled v1.ConditionReason = "HealthCheckDisabled"
	ReasonHealthCheckSuccess  v1.ConditionReason = "HealthCheckSuccess"
	ReasonHealthCheckFail     v1.ConditionReason = "HealthCheckFail"
)

// HealthCheckDisabled returns a condition that indicates that the health
// of the resource is unknown because it is disabled.
func HealthCheckDisabled() v1.Condition {
	return v1.Condition{
		Type:               v1.TypeReady,
		Status:             corev1.ConditionUnknown,
		LastTransitionTime: metav1.Now(),
		Reason:             ReasonHealthCheckDisabled,
	}
}

// HealthCheckSuccess returns a condition that indicates that the resource
// is ready because the health check was successful.
func HealthCheckSuccess() v1.Condition {
	return v1.Condition{
		Type:               v1.TypeReady,
		Status:             corev1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             ReasonHealthCheckSuccess,
	}
}

// HealthCheckFail returns a condition that indicates that the resource
// is not ready because the health check was unsuccessful.
func HealthCheckFail() v1.Condition {
	return v1.Condition{
		Type:               v1.TypeReady,
		Status:             corev1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             ReasonHealthCheckFail,
	}
}
