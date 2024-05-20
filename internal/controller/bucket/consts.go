package bucket

const (
	// k8s error messages.
	errNotBucket      = "managed resource is not a Bucket custom resource"
	errTrackPCUsage   = "failed to track ProviderConfig usage"
	errGetPC          = "failed to get ProviderConfig"
	errUpdateBucketCR = "failed to update Bucket CR"

	// Backend store error messages.
	errNoS3BackendsStored = "no s3 backends stored in backendstore"
	errNoActiveS3Backends = "no active s3 backends in backendstore"
	errMissingS3Backend   = "one or more desired providers are inactive or unhealthy"

	// Subresource error messages.
	errObserveSubresource = "failed to observe bucket subresource"
	errHandleSubresource  = "failed to handle bucket subresource"

	// Lifecycle configuration error messages.
	errObserveLifecycleConfig = "failed to observe bucket lifecycle configuration"
	errHandleLifecycleConfig  = "failed to handle bucket lifecycle configuration"

	// ACL error messages.
	errObserveAcl = "failed to observe bucket acl"
	errHandleAcl  = "failed to handle bucket acl"

	// BucketPolicy error messages.
	errObservePolicy = "failed to observe bucket policy"
	errHandlePolicy  = "failed to handle bucket policy"

	True = "true"
)
