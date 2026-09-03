package bucket

import (
	"context"
	"testing"

	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	xpv1 "github.com/crossplane/crossplane-runtime/v2/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	"github.com/linode/provider-ceph/internal/backendstore/backendstorefakes"
	"github.com/linode/provider-ceph/internal/consts"
)

// newUpdateExternal registers the backend so updateOnBackend's health check can resolve it.
func newUpdateExternal(t *testing.T, recreateMissing bool, health apisv1alpha1.HealthStatus) *external {
	t.Helper()

	bs := backendstore.NewBackendStore()
	bs.AddOrUpdateBackend(consts.S3Backend1, &backendstorefakes.FakeS3Client{}, nil, health)

	return &external{
		recreateMissingBucket: recreateMissing,
		backendStore:          bs,
		log:                   logr.Discard(),
	}
}

// getBackends only returns backends registered during this reconcile, so each
// path must record a condition or the backend is dropped from the Status.
func TestUpdateOnBackendRecordsBackendOnEveryPath(t *testing.T) {
	t.Parallel()

	someErr := errors.New("backend refused the request")
	conflictErr := &s3types.BucketAlreadyExists{}

	testCases := map[string]struct {
		recreateMissing bool
		health          apisv1alpha1.HealthStatus
		headBucketErr   error
		createBucketErr error

		expectErr         bool
		expectedCondition xpv1.Condition
	}{
		"bucket present on healthy backend is Available": {
			health:            apisv1alpha1.HealthStatusHealthy,
			expectedCondition: xpv1.Available(),
		},
		// Unknown means health checking is disabled, so assume healthy.
		"bucket present on backend of unknown health is Available": {
			health:            apisv1alpha1.HealthStatusUnknown,
			expectedCondition: xpv1.Available(),
		},
		"bucket present on unhealthy backend is Unavailable": {
			health:            apisv1alpha1.HealthStatusUnhealthy,
			expectedCondition: xpv1.Unavailable().WithMessage("Backend is marked Unhealthy"),
		},
		"HeadBucket failure records Unavailable and surfaces the error": {
			health:            apisv1alpha1.HealthStatusHealthy,
			headBucketErr:     someErr,
			expectErr:         true,
			expectedCondition: xpv1.Unavailable().WithMessage(errors.Wrap(someErr, "failed to perform head bucket").Error()),
		},
		"missing bucket successfully recreated is Available": {
			recreateMissing:   true,
			health:            apisv1alpha1.HealthStatusHealthy,
			headBucketErr:     &s3types.NotFound{},
			expectedCondition: xpv1.Available(),
		},
		"failed recreate records Unavailable rather than dropping the backend": {
			recreateMissing:   true,
			health:            apisv1alpha1.HealthStatusHealthy,
			headBucketErr:     &s3types.NotFound{},
			createBucketErr:   someErr,
			expectErr:         true,
			expectedCondition: xpv1.Unavailable().WithMessage(errors.Wrap(someErr, "failed to create bucket").Error()),
		},
		// The name is held by another tenant, so the backend is not Available.
		"recreate of a bucket held by another tenant is Unavailable": {
			recreateMissing:   true,
			health:            apisv1alpha1.HealthStatusHealthy,
			headBucketErr:     &s3types.NotFound{},
			createBucketErr:   conflictErr,
			expectErr:         true,
			expectedCondition: xpv1.Unavailable().WithMessage(errors.Wrap(conflictErr, "failed to create bucket").Error()),
		},
	}

	for name, tt := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cl := &backendstorefakes.FakeS3Client{}
			cl.HeadBucketReturns(nil, tt.headBucketErr)
			cl.CreateBucketReturns(nil, tt.createBucketErr)

			e := newUpdateExternal(t, tt.recreateMissing, tt.health)
			bucket := &v1alpha1.Bucket{ObjectMeta: metav1.ObjectMeta{Name: consts.TestBucket}}
			bb := newBucketBackends()

			err := e.updateOnBackend(context.Background(), consts.S3Backend1, bucket, cl, bb)()

			if tt.expectErr {
				require.Error(t, err, "expected the failure to be surfaced to the caller")
			} else {
				require.NoError(t, err, "unexpected error")
			}

			got := bb.getBackends(consts.TestBucket, []string{consts.S3Backend1})

			require.Containsf(t, got, consts.S3Backend1,
				"backend must be recorded on every path; dropping it wipes status.atProvider.backends")

			// Condition.Equal ignores LastTransitionTime.
			actual := got[consts.S3Backend1].BucketCondition
			assert.Truef(t, tt.expectedCondition.Equal(actual),
				"unexpected bucket condition\nexpected: %+v\nactual:   %+v", tt.expectedCondition, actual)
		})
	}
}

func TestUpdateOnBackendDropsBackendWhenRecreateDisabled(t *testing.T) {
	t.Parallel()

	cl := &backendstorefakes.FakeS3Client{}
	cl.HeadBucketReturns(nil, &s3types.NotFound{})

	e := newUpdateExternal(t, false, apisv1alpha1.HealthStatusHealthy)
	bucket := &v1alpha1.Bucket{ObjectMeta: metav1.ObjectMeta{Name: consts.TestBucket}}
	bb := newBucketBackends()

	bb.setBucketCondition(consts.TestBucket, consts.S3Backend1, xpv1.Available())

	err := e.updateOnBackend(context.Background(), consts.S3Backend1, bucket, cl, bb)()
	require.NoError(t, err, "a missing bucket is not an error when recreate is disabled")

	assert.NotContains(t, bb.getBackends(consts.TestBucket, []string{consts.S3Backend1}), consts.S3Backend1,
		"with recreate disabled the backend is deliberately dropped, which removes it from the status")

	assert.Zero(t, cl.CreateBucketCallCount(), "recreate must not be attempted when disabled")
}
