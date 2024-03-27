// Package otel provides tools to interact with the opentelemetry golang sdk
package otel

import (
	"context"
	"time"

	otelresource "go.opentelemetry.io/otel/sdk/resource"

	// We have to set this to match the version used in ~/go/pkg/mod/go.opentelemetry.io/otel/sdk
	// See https://github.com/open-telemetry/opentelemetry-go/issues/2341
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

const (
	ServiceName                = "provider-ceph"
	TimeoutGatherHostResources = time.Millisecond * 500
)

// RuntimeResources creates an otel sdk resource struct describing the service
// and runtime (host, process, runtime, etc). When used together with a
// TracerProvider this data will be included in all traces created from it.
func RuntimeResources() (*otelresource.Resource, error) {
	ctx, cancel := context.WithTimeout(context.Background(), TimeoutGatherHostResources)
	defer cancel()
	runtimeResources, err := otelresource.New(
		ctx,
		otelresource.WithOSDescription(), // I.E. "Ubuntu 20.04.6 LTS (Focal Fossa) (Linux bos-lhvxje 5.15.0-60-generic #66~20.04.1-Ubuntu SMP Wed Jan 25 09:41:30 UTC 2023 x86_64)"
		otelresource.WithProcessRuntimeDescription(), // I.E. "go version go1.20.3 linux/amd64"
		otelresource.WithHost(),                      // I.E. In k8s this is the pod name
		otelresource.WithSchemaURL(semconv.SchemaURL),
		otelresource.WithAttributes(
			semconv.ServiceName(ServiceName),
		),
	)
	if err != nil {
		return nil, err
	}

	return runtimeResources, nil
}
