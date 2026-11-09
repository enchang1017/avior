// Package telemetry exports traces and logs over OTLP, the way the rest of this
// estate does.
//
// Nothing here is configured by an AVIOR_ variable. The OpenTelemetry SDK reads
// OTEL_EXPORTER_OTLP_ENDPOINT, OTEL_SERVICE_NAME and OTEL_RESOURCE_ATTRIBUTES
// itself, and those are the names every other service in this namespace is
// already deployed with — a second spelling for the same setting would make
// this repository the one place the estate's telemetry is configured
// differently, which is the failure the shared names exist to prevent.
//
// Unset OTEL_EXPORTER_OTLP_ENDPOINT switches the whole thing off and leaves
// stdout logging alone, which is what the local stack and the test suites want.
package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Name is the instrumentation scope every record this process emits carries.
// It becomes scope_name in Loki.
const Name = "avior"

// Telemetry is what was started, and how to stop it.
type Telemetry struct {
	logs   *sdklog.LoggerProvider
	traces *sdktrace.TracerProvider
}

// Enabled reports whether an OTLP collector was named.
func Enabled() bool {
	return strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")) != ""
}

// Start builds the providers and registers them globally.
//
// Registering globally is what lets the instrumentation libraries — otelhttp
// around the server, and the slog bridge below — find them without being handed
// anything. It returns a zero Telemetry when no endpoint is configured, so the
// caller does not have to ask twice.
func Start(ctx context.Context) (*Telemetry, error) {
	if !Enabled() {
		return &Telemetry{}, nil
	}

	// WithFromEnv reads OTEL_RESOURCE_ATTRIBUTES and OTEL_SERVICE_NAME, which is
	// where deployment.environment and service.namespace come from. They are not
	// set here: a manifest says which environment it is, and a binary that
	// decided for itself would be wrong in whichever one it was copied to.
	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithProcessRuntimeDescription(),
	)
	// A resource carrying attributes this SDK version does not know is a
	// warning, not a failure, and comes back beside a usable resource.
	if err != nil && !errors.Is(err, resource.ErrPartialResource) &&
		!errors.Is(err, resource.ErrSchemaURLConflict) {
		return nil, fmt.Errorf("describe this process: %w", err)
	}

	traceExporter, err := otlptracegrpc.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("open the trace exporter: %w", err)
	}
	traces := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
	)

	logExporter, err := otlploggrpc.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("open the log exporter: %w", err)
	}
	logs := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExporter)),
		sdklog.WithResource(res),
	)

	otel.SetTracerProvider(traces)
	// The logger provider is handed to the bridge in Handler below rather than
	// registered globally: this process has exactly one place that logs, and a
	// global would let anything else start logging without being wired to.
	//
	// W3C tracecontext, so a trace started by whoever called us continues here
	// rather than beginning again — which is how a request crossing two
	// services ends up as two unrelated traces.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))

	return &Telemetry{logs: logs, traces: traces}, nil
}

// Shutdown flushes what is still batched.
//
// Called on the way out, where the alternative is losing the records of the
// shutdown itself — which are the ones somebody reads after an unexplained
// restart.
func (t *Telemetry) Shutdown(ctx context.Context) error {
	var errs []error
	if t.traces != nil {
		if err := t.traces.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("stop the trace exporter: %w", err))
		}
	}
	if t.logs != nil {
		if err := t.logs.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("stop the log exporter: %w", err))
		}
	}
	return errors.Join(errs...)
}

// Handler returns the slog handler this process should log through.
//
// Two destinations, and both are wanted. Stdout is what `kubectl logs` shows
// and is the only thing available when the collector is the thing that is
// broken; OTLP is what reaches Loki carrying trace_id and span_id, which is
// what makes a log line and the span that produced it findable from each other.
//
// The bridge takes the record whole: the message stays the message and every
// attribute stays an attribute, which is the shape the estate's other services
// emit and the shape its queries are written against.
func (t *Telemetry) Handler(stdout slog.Handler) slog.Handler {
	if t.logs == nil {
		return stdout
	}
	return fanOut{stdout, otelslog.NewHandler(Name, otelslog.WithLoggerProvider(t.logs))}
}

// fanOut writes every record to each handler.
type fanOut []slog.Handler

func (f fanOut) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range f {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (f fanOut) Handle(ctx context.Context, record slog.Record) error {
	var errs []error
	for _, h := range f {
		if !h.Enabled(ctx, record.Level) {
			continue
		}
		// Cloned, because a handler may retain what it is given and the two
		// would otherwise share one record's attribute storage.
		if err := h.Handle(ctx, record.Clone()); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (f fanOut) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := make(fanOut, len(f))
	for i, h := range f {
		next[i] = h.WithAttrs(attrs)
	}
	return next
}

func (f fanOut) WithGroup(name string) slog.Handler {
	next := make(fanOut, len(f))
	for i, h := range f {
		next[i] = h.WithGroup(name)
	}
	return next
}
