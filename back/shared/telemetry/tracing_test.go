package telemetry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestHTTPTracePropagationWithoutSecrets(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter), sdktrace.WithSampler(sdktrace.AlwaysSample()))
	previous, propagator := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		otel.SetTracerProvider(previous)
		otel.SetTextMapPropagator(propagator)
	})
	router := chi.NewRouter()
	router.Use(Middleware)
	router.Get("/users/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !trace.SpanContextFromContext(r.Context()).IsValid() {
			t.Error("server did not receive trace context")
		}
		w.WriteHeader(204)
	})
	server := httptest.NewServer(router)
	defer server.Close()
	ctx, root := provider.Tracer("test").Start(context.Background(), "root")
	req, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/users/123?token=private-secret", nil)
	client := &http.Client{Transport: ClientTransport{Base: http.DefaultTransport}}
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	root.End()
	spans := exporter.GetSpans()
	if len(spans) != 3 {
		t.Fatalf("spans=%d", len(spans))
	}
	for _, span := range spans {
		if span.SpanContext.TraceID() != root.SpanContext().TraceID() {
			t.Fatal("trace split between services")
		}
		for _, attribute := range span.Attributes {
			if strings.Contains(attribute.Value.AsString(), "private-secret") || strings.Contains(attribute.Value.AsString(), "/users/123") {
				t.Fatal("private URL recorded in trace")
			}
		}
	}
	if spans[0].Name != "GET /users/{id}" {
		t.Fatalf("route not normalized: %s", spans[0].Name)
	}
}
