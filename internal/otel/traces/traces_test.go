package traces

import (
	"context"
	"testing"

	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestInjectTraceAndLogger_NoTrace(t *testing.T) {
	t.Parallel()

	baseLogger := testr.New(t)
	ctx := context.Background()

	_, logger := InjectTraceAndLogger(ctx, baseLogger)
	assert.NotNil(t, logger)

	// Should still be the same base logger since no trace
	assert.Equal(t, baseLogger, logger)
}

func TestInjectTraceAndLogger_WithValidTrace(t *testing.T) {
	t.Parallel()

	baseLogger := testr.New(t)

	// Setup a test tracer provider
	sr := tracetest.NewSpanRecorder()
	tp := trace.NewTracerProvider()
	tp.RegisterSpanProcessor(sr)
	tracer := tp.Tracer("test")

	// Create a span
	ctx, span := tracer.Start(context.Background(), "test-span")
	defer span.End()

	_, logger := InjectTraceAndLogger(ctx, baseLogger)
	assert.NotNil(t, logger)
	assert.NotEqual(t, baseLogger, logger)

	// Span context should be valid
	sc := span.SpanContext()
	assert.True(t, sc.IsValid())
}

func TestInjectTraceAndLogger_ReusesLoggerFromContext(t *testing.T) {
	t.Parallel()

	baseLogger := testr.New(t)
	ctx := context.WithValue(context.Background(), ctxKeyLogger{}, baseLogger)

	// Inject the logger, should reuse the existing logger from context
	_, logger := InjectTraceAndLogger(ctx, testr.New(t))

	// Should return the original logger from the context
	assert.Equal(t, baseLogger, logger)
}
