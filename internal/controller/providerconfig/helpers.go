package providerconfig

import (
	"context"
	"time"

	"github.com/crossplane/crossplane-runtime/pkg/resource"
	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	"github.com/pkg/errors"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// UpdateProviderConfigStatus updates the status of a ProviderConfig resource by applying a callback.
// The function uses an exponential backoff retry mechanism to handle potential conflicts during updates.
//
// The callback takes two ProviderConfig parameters. Before the callback is called, the first ProviderConfig
// parameter will become a DeepCopy of pc. The second will become the latest version of pc, as it is fetched
// from the Kube API. A callback function should aim to update the latest version of the pc (second parameter)
// with the changes which will be persisted in pc (and as a result, it's DeepCopy).
//
// Callback example 1, updating the latest version of pc with a field from your version of pc:
//
//	func(pcDeepCopy, pcLatest *apisv1alpha1.ProviderConfig) {
//	  pcLatest.Status.SomeOtherField = pcDeepCopy.Status.SomeOtherField
//	},
//
// Callback example 2, updating the latest version of pc with a string:
//
//	func(_, pcLatest *apisv1alpha1.ProviderConfig) {
//	  pcLatest.Status.SomeOtherField = "some-value"
//	},
//
// Example usage with above callback example 1:
//
//	err := UpdateProviderConfigStatus(ctx, pc, func(pcDeepCopy, pcLatest *apisv1alpha1.ProviderConfig) {
//	  pcLatest.Status.SomeOtherField = pcDeepCopy.Status.SomeOtherField
//	})
//
//	if err != nil {
//	  // Handle error
//	}
func UpdateProviderConfigStatus(ctx context.Context, kubeClient client.Client, pc *apisv1alpha1.ProviderConfig, callback func(*apisv1alpha1.ProviderConfig, *apisv1alpha1.ProviderConfig)) error {
	const (
		steps  = 4
		factor = 0.5
		jitter = 0.1
	)

	nn := types.NamespacedName{Name: pc.GetName(), Namespace: pc.Namespace}
	pcDeepCopy := pc.DeepCopy()

	err := retry.OnError(wait.Backoff{
		Steps:    steps,
		Duration: (time.Duration(pc.Spec.HealthCheckIntervalSeconds) * time.Second) - time.Second,
		Factor:   factor,
		Jitter:   jitter,
	}, resource.IsAPIError, func() error {
		if err := kubeClient.Get(ctx, nn, pc); err != nil {
			return err
		}

		callback(pcDeepCopy, pc)

		return kubeClient.Status().Update(ctx, pc)
	})

	if err != nil {
		if kerrors.IsNotFound(err) {
			return nil
		}

		return errors.Wrap(err, "Failed to update ProviderConfig status")
	}

	return nil
}
