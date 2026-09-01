package bucket

import (
	"context"
	"testing"

	xpv1 "github.com/crossplane/crossplane-runtime/v2/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/v2/pkg/meta"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	"github.com/linode/provider-ceph/internal/consts"
)

// newBucketFakeClient returns a fake client that serves as both the kubeClient and
// the kubeReader. In production kubeReader is the manager's uncached API reader, so
// it reads the same store the writes go to. Two fake clients would have two
// independent stores and every optimistic lock would fail.
func newBucketFakeClient(t *testing.T, objs ...client.Object) client.WithWatch {
	t.Helper()

	s := runtime.NewScheme()
	require.NoError(t, scheme.AddToScheme(s), "registering built-in types")
	s.AddKnownTypes(v1alpha1.SchemeGroupVersion, &v1alpha1.Bucket{}, &v1alpha1.BucketList{})
	metav1.AddToGroupVersion(s, v1alpha1.SchemeGroupVersion)

	return fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(objs...).
		WithStatusSubresource(objs...).
		Build()
}

// TestUpdateBucketCRObjectPatchDoesNotClobberConcurrentWrite covers the lost update
// that wedges a Bucket CR in the delete flow. An external client sets
// spec.disabled=true while provider-ceph sits between the Get and the Patch of its
// own auto-pause write. The object patch carries an optimistic lock, so the stale
// write is rejected with a conflict and the callback runs again against the current
// object. The two branches of the callback write different label values, so the
// surviving value says which read the winning patch was built from.
func TestUpdateBucketCRObjectPatchDoesNotClobberConcurrentWrite(t *testing.T) {
	t.Parallel()

	const sawDisabled = "saw-disabled"

	cl := newBucketFakeClient(t, &v1alpha1.Bucket{
		ObjectMeta: metav1.ObjectMeta{
			Name:   consts.TestBucket,
			Labels: map[string]string{meta.AnnotationKeyReconciliationPaused: ""},
		},
	})

	e := external{kubeClient: cl, kubeReader: cl, log: logr.Discard()}

	ctx := context.Background()
	calls := 0

	err := e.updateBucketCR(ctx, &v1alpha1.Bucket{ObjectMeta: metav1.ObjectMeta{Name: consts.TestBucket}},
		func(bucketLatest *v1alpha1.Bucket) UpdateRequired {
			calls++

			// Land the concurrent write after this callback's Get and before its
			// Patch, on the first attempt only.
			if calls == 1 {
				concurrent := &v1alpha1.Bucket{}
				require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: consts.TestBucket}, concurrent))
				concurrent.Spec.Disabled = true
				require.NoError(t, cl.Update(ctx, concurrent))
			}

			// Stands in for isPauseRequired, whose decision depends on the state the
			// callback read.
			if bucketLatest.Spec.Disabled {
				bucketLatest.Labels[meta.AnnotationKeyReconciliationPaused] = sawDisabled
			} else {
				bucketLatest.Labels[meta.AnnotationKeyReconciliationPaused] = consts.TrueStr
			}

			return NeedsObjectUpdate
		})
	require.NoError(t, err, "unexpected err")

	assert.Equal(t, 2, calls, "callback should be retried after the conflict")

	got := &v1alpha1.Bucket{}
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: consts.TestBucket}, got))
	assert.True(t, got.Spec.Disabled, "concurrent write to spec.disabled should survive")
	assert.Equal(t, sawDisabled, got.Labels[meta.AnnotationKeyReconciliationPaused],
		"the surviving patch should be the one built from the re-read object")
}

// TestUpdateBucketCRStatusPatchDoesNotClobberConcurrentWrite covers the same lost
// update on the status subresource. status.atProvider.backends is only written here,
// and the obj-endpoint delete handshake blocks until it empties, so a patch built
// from a stale read either strands an entry or erases one another writer just added.
func TestUpdateBucketCRStatusPatchDoesNotClobberConcurrentWrite(t *testing.T) {
	t.Parallel()

	cl := newBucketFakeClient(t, &v1alpha1.Bucket{
		ObjectMeta: metav1.ObjectMeta{Name: consts.TestBucket},
		Status: v1alpha1.BucketStatus{
			AtProvider: v1alpha1.BucketObservation{
				Backends: v1alpha1.Backends{
					consts.S3Backend1: &v1alpha1.BackendInfo{BucketCondition: xpv1.Available()},
				},
			},
		},
	})

	e := external{kubeClient: cl, kubeReader: cl, log: logr.Discard()}

	ctx := context.Background()
	calls := 0

	err := e.updateBucketCR(ctx, &v1alpha1.Bucket{ObjectMeta: metav1.ObjectMeta{Name: consts.TestBucket}},
		func(bucketLatest *v1alpha1.Bucket) UpdateRequired {
			calls++

			// Land the concurrent write after this callback's Get and before its
			// Patch, on the first attempt only.
			if calls == 1 {
				concurrent := &v1alpha1.Bucket{}
				require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: consts.TestBucket}, concurrent))
				concurrent.Status.AtProvider.Backends[consts.S3Backend2] = &v1alpha1.BackendInfo{
					BucketCondition: xpv1.Available(),
				}
				require.NoError(t, cl.Status().Update(ctx, concurrent))
			}

			// Stands in for setBucketStatus, which rebuilds Backends from the object
			// it was given. The bucket is gone from s3-backend-1 only.
			delete(bucketLatest.Status.AtProvider.Backends, consts.S3Backend1)

			return NeedsStatusUpdate
		})
	require.NoError(t, err, "unexpected err")

	assert.Equal(t, 2, calls, "callback should be retried after the conflict")

	got := &v1alpha1.Bucket{}
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: consts.TestBucket}, got))
	assert.NotContains(t, got.Status.AtProvider.Backends, consts.S3Backend1,
		"s3-backend-1 should be removed from status")
	assert.Contains(t, got.Status.AtProvider.Backends, consts.S3Backend2,
		"the concurrent write of s3-backend-2 should survive")
}

// TestUpdateBucketCRRejectsEmptyResourceVersion covers a reader that returns an
// object with no resourceVersion. MergeFromWithOptimisticLock would turn that into
// an opaque error that is neither a conflict nor a NotFound, so it would be neither
// retried nor recognised by the caller.
func TestUpdateBucketCRRejectsEmptyResourceVersion(t *testing.T) {
	t.Parallel()

	cl := newBucketFakeClient(t, &v1alpha1.Bucket{
		ObjectMeta: metav1.ObjectMeta{Name: consts.TestBucket},
	})

	e := external{
		kubeClient: cl,
		kubeReader: noResourceVersionReader{Reader: cl},
		log:        logr.Discard(),
	}

	err := e.updateBucketCR(context.Background(),
		&v1alpha1.Bucket{ObjectMeta: metav1.ObjectMeta{Name: consts.TestBucket}},
		func(bucketLatest *v1alpha1.Bucket) UpdateRequired {
			return NeedsObjectUpdate
		})

	require.ErrorContains(t, err, errNoResourceVersion, "unexpected err")
}

// noResourceVersionReader strips the resourceVersion from every object it reads.
type noResourceVersionReader struct {
	client.Reader
}

func (r noResourceVersionReader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if err := r.Reader.Get(ctx, key, obj, opts...); err != nil {
		return err
	}
	obj.SetResourceVersion("")

	return nil
}

func TestPauseAllowed(t *testing.T) {
	t.Parallel()

	deleting := metav1.Now()

	cases := map[string]struct {
		bucket *v1alpha1.Bucket
		want   bool
	}{
		"Active Bucket CR - pause allowed": {
			bucket: &v1alpha1.Bucket{},
			want:   true,
		},
		"Disabled Bucket CR - pause not allowed": {
			bucket: &v1alpha1.Bucket{Spec: v1alpha1.BucketSpec{Disabled: true}},
			want:   false,
		},
		"Deleting Bucket CR - pause not allowed": {
			bucket: &v1alpha1.Bucket{
				ObjectMeta: metav1.ObjectMeta{DeletionTimestamp: &deleting},
			},
			want: false,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, pauseAllowed(tc.bucket), "unexpected pauseAllowed result")
		})
	}
}
