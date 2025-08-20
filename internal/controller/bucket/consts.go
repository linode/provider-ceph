package bucket

import "github.com/crossplane/crossplane-runtime/v2/pkg/errors"

var errUnhealthyBackend = errors.New("backend marked as unhealthy in backendstore")

const (
	// k8s error messages.
	errNotBucket      = "managed resource is not a Bucket custom resource"
	errTrackPCUsage   = "failed to track ProviderConfig usage"
	errGetPC          = "failed to get ProviderConfig"
	errUpdateBucketCR = "failed to update Bucket CR"

	// Backend store error messages.
	errNoS3BackendsStored    = "no s3 backends stored in backendstore"
	errAllS3BackendsDisabled = "all s3 backends have been disabled for this bucket - please check cr labels"

	// Subresource error messages.
	errObserveSubresource = "failed to observe bucket subresource"
	errHandleSubresource  = "failed to handle bucket subresource"

	// Lifecycle configuration error messages.
	errObserveLifecycleConfig = "failed to observe bucket lifecycle configuration"
	errHandleLifecycleConfig  = "failed to handle bucket lifecycle configuration"

	// Versioning configuration error messages.
	errObserveVersioningConfig = "failed to observe bucket versioning configuration"
	errHandleVersioningConfig  = "failed to handle bucket versioning configuration"

	// Object lock configuration error messages.
	errObserveObjectLockConfig = "failed to observe object lock configuration"
	errHandleObjectLockConfig  = "failed to handle object lock configuration"

	// ACL error messages.
	errObserveAcl = "failed to observe bucket acl"
	errHandleAcl  = "failed to handle bucket acl"

	// Policy error messages.
	errObservePolicy = "failed to observe bucket policy"
	errHandlePolicy  = "failed to handle bucket policy"

	True = "true"
)
