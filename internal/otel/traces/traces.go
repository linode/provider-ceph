// Package traces provides helper functions for tracing with OpenTelemetry SDK.
package traces

import (
	"context"
	"fmt"
	"time"

	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/linode/provider-ceph/internal/otel"

	otelsdk "go.opentelemetry.io/otel"
	otelcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	otelsdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// InitTracerProvider configures a global tracer provider and dials to the OTEL Collector.
// Failing in doing so returns an error since service actively export their traces and
// require the Collector to be up.
// Returns a shutdown function that should be called at the end of the program to flush
// all in-momory traces.
func InitTracerProvider(log logging.Logger, otelCollectorAddress string, dialTimeout, exportInterval time.Duration) (func(context.Context), error) {
	runtimeResources, err := otel.RuntimeResources()
	if err != nil {
		return nil, fmt.Errorf("failed to gather runtime resources for traces provider: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	defer cancel()

	conn, err := grpc.NewClient(otelCollectorAddress,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC connection to otel collector: %w", err)
	}

	// Set up a tracer exporter
	traceExporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithGRPCConn(conn))
	if err != nil {
		return nil, fmt.Errorf("failed to create traces exporter: %w", err)
	}

	tp := otelsdktrace.NewTracerProvider(
		otelsdktrace.WithBatcher(traceExporter, otelsdktrace.WithBatchTimeout(exportInterval)),
		otelsdktrace.WithResource(runtimeResources),
	)
	otelsdk.SetTracerProvider(tp)
	otelsdk.SetTextMapPropagator(propagation.TraceContext{})

	flushFunction := func(ctx context.Context) {
		if err := tp.Shutdown(ctx); err != nil {
			log.Debug("failed to shutdown tracer provider and flush in-memory records", "error", err.Error())
		}
	}

	return flushFunction, nil
}

func SetAndRecordError(span trace.Span, err error) {
	span.SetStatus(otelcodes.Error, err.Error())
	span.RecordError(err)
}
