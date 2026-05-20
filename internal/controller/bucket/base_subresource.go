package bucket

import (
	"context"

	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	"github.com/go-logr/logr"

	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	"github.com/linode/provider-ceph/internal/consts"
	"github.com/linode/provider-ceph/internal/controller/s3clienthandler"
	"github.com/linode/provider-ceph/internal/otel/traces"

	"go.opentelemetry.io/otel"
)

// Subresource provides the plugin interface for observing and handling backend state.
type Subresource interface {
	// ObserveBackend observes the resource on a specific backend.
	// Should return NoAction/Updated/NeedsUpdate/NeedsDeletion and any error.
	ObserveBackend(ctx context.Context, bucket *v1alpha1.Bucket, backendName string) (ResourceStatus, error)

	// GetObserveErrorMsg returns the error message for observe failures.
	GetObserveErrorMsg() string

	// SkipObservation returns true if observation should be skipped for this subresource.
	// By default (false), all subresources are observed. Override to implement conditional observation.
	SkipObservation(bucket *v1alpha1.Bucket) bool

	// HandleObservation handles the resource on a specific backend based on the observation result.
	HandleObservation(ctx context.Context, observation ResourceStatus, bucket *v1alpha1.Bucket, backendName string, bb *bucketBackends) error

	// GetHandleErrorMsg returns the error message for handle failures.
	GetHandleErrorMsg() string

	// GetLogger returns the logger for this subresource.
	GetLogger() logr.Logger

	// GetBackendStore returns the backend store.
	GetBackendStore() *backendstore.BackendStore

	// GetS3ClientHandler returns the s3 client handler.
	GetS3ClientHandler() *s3clienthandler.Handler

	// GetSubresourceName returns the name of the subresource for tracing.
	GetSubresourceName() string
}

// BaseSubresourceClient provides common logic for all subresource clients.
type BaseSubresourceClient struct {
	backendStore    *backendstore.BackendStore
	s3ClientHandler *s3clienthandler.Handler
	log             logr.Logger
}

// NewBaseSubresourceClient creates a new base subresource client.
func NewBaseSubresourceClient(b *backendstore.BackendStore, h *s3clienthandler.Handler, l logr.Logger) BaseSubresourceClient {
	return BaseSubresourceClient{
		backendStore:    b,
		s3ClientHandler: h,
		log:             l,
	}
}

// SkipObservation provides a default implementation that does not skip observations.
// Subresources can override this to implement conditional observation logic.
func (b *BaseSubresourceClient) SkipObservation(bucket *v1alpha1.Bucket) bool {
	return false // By default, observe all subresources
}

// Observe implements the common observe pattern for all subresources.
// It handles concurrent observation of all backends and consolidates results.
func (b *BaseSubresourceClient) Observe(ctx context.Context, bucket *v1alpha1.Bucket, backendNames []string, subresource Subresource) (ResourceStatus, error) {
	ctx, span := otel.Tracer("").Start(ctx, "bucket."+subresource.GetSubresourceName()+".Observe")
	defer span.End()
	ctx, log := traces.InjectTraceAndLogger(ctx, subresource.GetLogger())

	// Check if this subresource should be skipped
	if subresource.SkipObservation(bucket) {
		log.V(1).Info(subresource.GetSubresourceName() + " observation skipped")

		return Updated, nil
	}

	observationChan := make(chan ResourceStatus, len(backendNames))
	errChan := make(chan error, len(backendNames))

	for _, backendName := range backendNames {
		beName := backendName
		go func() {
			if subresource.GetBackendStore().GetBackendHealthStatus(backendName) == apisv1alpha1.HealthStatusUnhealthy {
				// If a backend is marked as unhealthy, we can ignore it for now by returning NoAction.
				// The backend may be down for some time and we do not want to block Create/Update/Delete
				// calls on other backends. By returning NoAction here, we would never pass the Observe
				// phase until the backend becomes Healthy or Disabled.
				observationChan <- NoAction

				return
			}

			observation, err := subresource.ObserveBackend(ctx, bucket, beName)
			if err != nil {
				errChan <- err

				return
			}
			observationChan <- observation
		}()
	}

	for i := 0; i < len(backendNames); i++ {
		select {
		case <-ctx.Done():
			log.Info("Context timeout during bucket "+subresource.GetSubresourceName()+" observation", consts.KeyBucketName, bucket.Name)
			err := errors.Wrap(ctx.Err(), subresource.GetObserveErrorMsg())
			traces.SetAndRecordError(span, err)

			return NeedsUpdate, err
		case observation := <-observationChan:
			if observation == NeedsUpdate || observation == NeedsDeletion {
				return observation, nil
			}
		case err := <-errChan:
			err = errors.Wrap(err, subresource.GetObserveErrorMsg())
			traces.SetAndRecordError(span, err)

			return NeedsUpdate, err
		}
	}

	return Updated, nil
}

// Handle implements the common handle pattern for all subresources.
// It performs health checks and delegates to the handler for observation-specific logic.
func (b *BaseSubresourceClient) Handle(ctx context.Context, bucket *v1alpha1.Bucket, backendName string, bb *bucketBackends, subresource Subresource) error {
	ctx, span := otel.Tracer("").Start(ctx, "bucket."+subresource.GetSubresourceName()+".Handle")
	defer span.End()

	// Check if this subresource should be skipped
	if subresource.SkipObservation(bucket) {
		return nil
	}

	if subresource.GetBackendStore().GetBackendHealthStatus(backendName) == apisv1alpha1.HealthStatusUnhealthy {
		traces.SetAndRecordError(span, errUnhealthyBackend)

		return errUnhealthyBackend
	}

	observation, err := subresource.ObserveBackend(ctx, bucket, backendName)
	if err != nil {
		err = errors.Wrap(err, subresource.GetHandleErrorMsg())
		traces.SetAndRecordError(span, err)

		return err
	}

	err = subresource.HandleObservation(ctx, observation, bucket, backendName, bb)
	if err != nil {
		traces.SetAndRecordError(span, err)
	}

	return err
}
