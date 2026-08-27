package bucket

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	xpv1 "github.com/crossplane/crossplane-runtime/v2/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"

	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	"github.com/linode/provider-ceph/internal/consts"
	"github.com/linode/provider-ceph/internal/controller/s3clienthandler"
	"github.com/linode/provider-ceph/internal/otel/traces"
	"github.com/linode/provider-ceph/internal/rgw"

	"go.opentelemetry.io/otel"
)

// CORSConfigurationClient is the client for API methods and reconciling the CORSConfiguration.
type CORSConfigurationClient struct {
	backendStore    *backendstore.BackendStore
	s3ClientHandler *s3clienthandler.Handler
	log             logr.Logger
}

func NewCORSConfigurationClient(b *backendstore.BackendStore, h *s3clienthandler.Handler, l logr.Logger) *CORSConfigurationClient {
	return &CORSConfigurationClient{backendStore: b, s3ClientHandler: h, log: l}
}

//nolint:dupl // CORSConfiguration is similar to other subresource clients.
func (l *CORSConfigurationClient) Observe(ctx context.Context, bucket *v1alpha1.Bucket, backendNames []string) (ResourceStatus, error) {
	ctx, span := otel.Tracer("").Start(ctx, "bucket.CORSConfigurationClient.Observe")
	defer span.End()
	ctx, log := traces.InjectTraceAndLogger(ctx, l.log)

	observationChan := make(chan ResourceStatus, len(backendNames))
	errChan := make(chan error, len(backendNames))

	for _, backendName := range backendNames {
		beName := backendName
		go func() {
			if l.backendStore.GetBackendHealthStatus(beName) == apisv1alpha1.HealthStatusUnhealthy {
				observationChan <- NoAction

				return
			}

			observation, err := l.observeBackend(ctx, bucket, beName)
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
			log.Info("Context timeout during bucket CORS configuration observation", consts.KeyBucketName, bucket.Name)
			err := errors.Wrap(ctx.Err(), errObserveCORSConfig)
			traces.SetAndRecordError(span, err)

			return NeedsUpdate, err
		case observation := <-observationChan:
			if observation == NeedsUpdate || observation == NeedsDeletion {
				return observation, nil
			}
		case err := <-errChan:
			err = errors.Wrap(err, errObserveCORSConfig)
			traces.SetAndRecordError(span, err)

			return NeedsUpdate, err
		}
	}

	return Updated, nil
}

func (l *CORSConfigurationClient) observeBackend(ctx context.Context, bucket *v1alpha1.Bucket, backendName string) (ResourceStatus, error) {
	_, log := traces.InjectTraceAndLogger(ctx, l.log)

	log.V(1).Info("Observing subresource CORS configuration on backend", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, backendName)

	s3Client, err := l.s3ClientHandler.GetS3Client(ctx, bucket, backendName)
	if err != nil {
		return NeedsUpdate, err
	}
	response, err := rgw.GetBucketCors(ctx, s3Client, aws.String(bucket.Name))
	if err != nil {
		return NeedsUpdate, err
	}

	if bucket.Spec.ForProvider.CORSConfiguration == nil || bucket.Spec.CORSConfigurationDisabled {
		if response == nil || len(response.CORSRules) == 0 {
			log.V(1).Info("No CORS configuration found on backend - no action required", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, backendName)

			return NoAction, nil
		}

		log.V(1).Info("CORS configuration found on backend - requires deletion", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, backendName)

		return NeedsDeletion, nil
	}

	var corsRulesInCR []v1alpha1.CORSRule
	if bucket.Spec.ForProvider.CORSConfiguration != nil {
		corsRulesInCR = bucket.Spec.ForProvider.CORSConfiguration.Rules
	}

	var corsRulesOnBackend []s3types.CORSRule
	if response != nil {
		corsRulesOnBackend = response.CORSRules
	}

	if len(corsRulesOnBackend) != 0 && len(corsRulesInCR) == 0 {
		return NeedsDeletion, nil
	}

	if !cmp.Equal(corsRulesOnBackend, rgw.GenerateCORSRules(corsRulesInCR), cmpopts.EquateEmpty(), cmpopts.IgnoreUnexported(s3types.CORSRule{})) {
		log.V(1).Info("CORS configuration requires update on backend", consts.KeyBucketName, bucket.Name, consts.KeyBackendName, backendName)

		return NeedsUpdate, nil
	}

	return Updated, nil
}

//nolint:dupl // CORSConfiguration and other configurations have similar Handle logic.
func (l *CORSConfigurationClient) Handle(ctx context.Context, b *v1alpha1.Bucket, backendName string, bb *bucketBackends) error {
	ctx, span := otel.Tracer("").Start(ctx, "bucket.CORSConfigurationClient.Handle")
	defer span.End()

	if l.backendStore.GetBackendHealthStatus(backendName) == apisv1alpha1.HealthStatusUnhealthy {
		traces.SetAndRecordError(span, errUnhealthyBackend)

		return errUnhealthyBackend
	}

	observation, err := l.observeBackend(ctx, b, backendName)
	if err != nil {
		err = errors.Wrap(err, errHandleCORSConfig)
		traces.SetAndRecordError(span, err)

		return err
	}

	switch observation {
	case NoAction:
		return nil
	case Updated:
		available := xpv1.Available()
		bb.setCORSConfigCondition(b.Name, backendName, &available)

	case NeedsDeletion:
		if err := l.delete(ctx, b, backendName); err != nil {
			err = errors.Wrap(err, errHandleCORSConfig)
			deleting := xpv1.Deleting().WithMessage(err.Error())
			bb.setCORSConfigCondition(b.Name, backendName, &deleting)

			traces.SetAndRecordError(span, err)

			return err
		}
		bb.setCORSConfigCondition(b.Name, backendName, nil)

	case NeedsUpdate:
		if err := l.createOrUpdate(ctx, b, backendName); err != nil {
			err = errors.Wrap(err, errHandleCORSConfig)
			unavailable := xpv1.Unavailable().WithMessage(err.Error())
			bb.setCORSConfigCondition(b.Name, backendName, &unavailable)

			traces.SetAndRecordError(span, err)

			return err
		}
		available := xpv1.Available()
		bb.setCORSConfigCondition(b.Name, backendName, &available)
	}

	return nil
}

func (l *CORSConfigurationClient) createOrUpdate(ctx context.Context, b *v1alpha1.Bucket, backendName string) error {
	s3Client, err := l.s3ClientHandler.GetS3Client(ctx, b, backendName)
	if err != nil {
		return err
	}

	_, err = rgw.PutBucketCors(ctx, s3Client, b)

	return err
}

func (l *CORSConfigurationClient) delete(ctx context.Context, b *v1alpha1.Bucket, backendName string) error {
	s3Client, err := l.s3ClientHandler.GetS3Client(ctx, b, backendName)
	if err != nil {
		return err
	}

	return rgw.DeleteBucketCors(ctx, s3Client, aws.String(b.Name))
}
