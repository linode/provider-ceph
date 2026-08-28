package otel

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
)

// The sdk resource detectors carry the semconv version the sdk was built against,
// so an import here from a different version fails the merge. Tracing is off by
// default, which leaves this to fail at startup rather than in a build.
func TestRuntimeResources(t *testing.T) {
	t.Parallel()

	resources, err := RuntimeResources()
	require.NoError(t, err, "schema URL must match the one the sdk resources use")
	assert.Equal(t, semconv.SchemaURL, resources.SchemaURL())
}
