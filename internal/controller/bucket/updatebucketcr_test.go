package bucket

import (
	"context"
	"testing"

	"github.com/crossplane/crossplane-runtime/v2/pkg/meta"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	"github.com/linode/provider-ceph/internal/consts"
)

// TestUpdateBucketCRObjectPatchDoesNotClobberConcurrentWrite covers the lost update
// that wedges a Bucket CR in the delete flow. An external client sets
// spec.disabled=true and the paused label to "false" in a single write, while
// provider-ceph sits between the Get and the Patch of its own auto-pause write.
// The object patch carries an optimistic lock, so the stale write is rejected
// with a conflict and retried against the current object.
func TestUpdateBucketCRObjectPatchDoesNotClobberConcurrentWrite(t *testing.T) {
	t.Parallel()

	s := runtime.NewScheme()
	require.NoError(t, scheme.AddToScheme(s), "registering built-in types")
	s.AddKnownTypes(apisv1alpha1.SchemeGroupVersion, &v1alpha1.Bucket{})

	bucket := &v1alpha1.Bucket{
		ObjectMeta: metav1.ObjectMeta{
			Name:   consts.TestBucket,
			Labels: map[string]string{meta.AnnotationKeyReconciliationPaused: ""},
		},
	}

	cl := fake.NewClientBuilder().
		WithObjects(bucket).
		WithStatusSubresource(bucket).
		WithScheme(s).Build()

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
				concurrent.Labels[meta.AnnotationKeyReconciliationPaused] = consts.FalseStr
				require.NoError(t, cl.Update(ctx, concurrent))
			}

			// Stands in for isPauseRequired, whose decision depends on the state
			// the callback read.
			if bucketLatest.Spec.Disabled {
				bucketLatest.Labels[meta.AnnotationKeyReconciliationPaused] = consts.FalseStr
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
	assert.Equal(t, consts.FalseStr, got.Labels[meta.AnnotationKeyReconciliationPaused],
		"concurrent write to the paused label should not be clobbered")
}
