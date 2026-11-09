package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// An unset endpoint is a deployment that wants no collector, and it must leave
// logging exactly as it found it — the local stack and every suite run that way.
func TestNoEndpointLeavesTheStdoutHandlerAlone(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	if Enabled() {
		t.Error("Enabled() with no endpoint")
	}

	tel, err := Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	stdout := slog.NewJSONHandler(&bytes.Buffer{}, nil)
	if got := tel.Handler(stdout); got != slog.Handler(stdout) {
		t.Error("the handler was wrapped although nothing is exported")
	}
	if err := tel.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown with nothing started: %v", err)
	}
}

// Whitespace is not an endpoint. A variable left as a blank string in a manifest
// is the same statement as one nobody wrote.
func TestBlankEndpointIsNotAnEndpoint(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "   ")
	if Enabled() {
		t.Error("Enabled() for a whitespace endpoint")
	}
}

func TestAnEndpointTurnsExportOn(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://collector.invalid:4317")
	if !Enabled() {
		t.Fatal("Enabled() with an endpoint set")
	}

	// The exporters connect lazily, so this does not need a collector to answer.
	tel, err := Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	stdout := slog.NewJSONHandler(&bytes.Buffer{}, nil)
	if got := tel.Handler(stdout); got == slog.Handler(stdout) {
		t.Error("the handler was not wrapped although a collector is configured")
	}

	// Shutting down against a collector that does not exist is allowed to fail;
	// what must not happen is that it blocks past its context.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- tel.Shutdown(ctx) }()
	select {
	case err := <-done:
		// Whatever it reports, it reported: an unreachable collector is a
		// failure to flush, not a reason for this to hang.
		t.Logf("Shutdown against an absent collector: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("Shutdown did not return")
	}
}

// fanOut is what puts one record in two places. Every method has to carry both,
// because a handler that quietly dropped one destination would look like
// working logging right up until somebody went looking in the other.
func TestFanOutWritesToEveryHandler(t *testing.T) {
	var first, second bytes.Buffer
	handler := fanOut{
		slog.NewJSONHandler(&first, nil),
		slog.NewJSONHandler(&second, nil),
	}

	slog.New(handler).Info("directory bind accepted", "Duration", 12.5)

	for name, written := range map[string]*bytes.Buffer{"first": &first, "second": &second} {
		var record map[string]any
		if err := json.Unmarshal(written.Bytes(), &record); err != nil {
			t.Fatalf("%s handler wrote %q: %v", name, written.String(), err)
		}
		if record["msg"] != "directory bind accepted" {
			t.Errorf("%s handler msg = %v", name, record["msg"])
		}
		if record["Duration"] != 12.5 {
			t.Errorf("%s handler Duration = %v", name, record["Duration"])
		}
	}
}

func TestFanOutCarriesAttrsAndGroupsToEveryHandler(t *testing.T) {
	var first, second bytes.Buffer
	handler := fanOut{
		slog.NewJSONHandler(&first, nil),
		slog.NewJSONHandler(&second, nil),
	}.WithAttrs([]slog.Attr{slog.String("RequestId", "abc")}).WithGroup("Request")

	slog.New(handler).Info("http response", "StatusCode", 200)

	for name, written := range map[string]*bytes.Buffer{"first": &first, "second": &second} {
		if !strings.Contains(written.String(), `"RequestId":"abc"`) {
			t.Errorf("%s handler lost the attribute: %s", name, written.String())
		}
		if !strings.Contains(written.String(), `"Request":{"StatusCode":200}`) {
			t.Errorf("%s handler lost the group: %s", name, written.String())
		}
	}
}

// One destination refusing a record must not hide it from the other, and the
// refusal must still be reported.
func TestFanOutReportsAFailureAndStillWritesTheRest(t *testing.T) {
	var written bytes.Buffer
	failure := errors.New("collector refused it")
	handler := fanOut{&failing{err: failure}, slog.NewJSONHandler(&written, nil)}

	err := handler.Handle(context.Background(), slog.Record{Level: slog.LevelInfo, Message: "kept"})
	if !errors.Is(err, failure) {
		t.Errorf("Handle error = %v, want the handler's", err)
	}
	if !strings.Contains(written.String(), "kept") {
		t.Errorf("the working handler was skipped: %q", written.String())
	}
}

// Enabled is an OR: a record any destination wants is a record that is built.
func TestFanOutIsEnabledWhenAnyHandlerIs(t *testing.T) {
	quiet := slog.NewJSONHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelError})
	loud := slog.NewJSONHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelDebug})

	if (fanOut{quiet}).Enabled(context.Background(), slog.LevelInfo) {
		t.Error("enabled for a level no handler wants")
	}
	if !(fanOut{quiet, loud}).Enabled(context.Background(), slog.LevelInfo) {
		t.Error("not enabled although one handler wants the level")
	}
}

// A handler that is not enabled for a level is not asked to handle it.
func TestFanOutSkipsAHandlerThatDoesNotWantTheLevel(t *testing.T) {
	refuser := &failing{err: errors.New("should not be called"), level: slog.LevelError}
	handler := fanOut{refuser}

	if err := handler.Handle(
		context.Background(), slog.Record{Level: slog.LevelInfo, Message: "ignored"},
	); err != nil {
		t.Errorf("Handle = %v, want nil", err)
	}
	if refuser.called {
		t.Error("a handler was asked to handle a level it refused")
	}
}

// failing stands in for a destination that is not working.
type failing struct {
	err    error
	level  slog.Level
	called bool
}

func (f *failing) Enabled(_ context.Context, level slog.Level) bool { return level >= f.level }

func (f *failing) Handle(context.Context, slog.Record) error {
	f.called = true
	return f.err
}

func (f *failing) WithAttrs([]slog.Attr) slog.Handler { return f }
func (f *failing) WithGroup(string) slog.Handler      { return f }
