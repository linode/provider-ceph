package bucket

const (
	errNotBucket                = "managed resource is not a Bucket custom resource"
	errTrackPCUsage             = "failed to track ProviderConfig usage"
	errCacheInit                = "failed to init Bucket cache"
	errGetPC                    = "failed to get ProviderConfig"
	errListPC                   = "failed to list ProviderConfigs"
	errUpdateBucketCR           = "failed to update Bucket CR"
	errGetCreds                 = "failed to get credentials"
	errBackendNotStored         = "s3 backend is not stored"
	errBackendInactive          = "s3 backend is inactive"
	errNoS3BackendsStored       = "no s3 backends stored"
	errNoS3BackendsRegistered   = "no s3 backends registered"
	errMissingS3Backend         = "missing s3 backends"
	errCodeBucketNotFound       = "NotFound"
	errFailedToCreateClient     = "failed to create s3 client"
	errBucketCreationInProgress = "bucket creation in progress"
	errObserveSubresource       = "failed to observe bucket subresource"
	errHandleSubresource        = "failed to handle bucket subresource"
	errObserveLifecycleConfig   = "failed to observe bucket lifecycle configuration"
	errHandleLifecycleConfig    = "failed to handle bucket lifecycle configuration"
	inUseFinalizer              = "bucket-in-use.provider-ceph.crossplane.io"
)
