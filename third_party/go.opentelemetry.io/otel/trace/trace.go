package trace

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

type SpanKind int

const (
	SpanKindInternal SpanKind = iota
	SpanKindServer
)

type TracerOption interface{}
type StartConfig struct {
	SpanKind   SpanKind
	Attributes []attribute.KeyValue
}
type StartOption interface{ apply(*StartConfig) }
type startOptionFunc func(*StartConfig)

func (f startOptionFunc) apply(c *StartConfig) { f(c) }
func WithSpanKind(kind SpanKind) StartOption {
	return startOptionFunc(func(c *StartConfig) { c.SpanKind = kind })
}
func WithAttributes(attrs ...attribute.KeyValue) StartOption {
	return startOptionFunc(func(c *StartConfig) { c.Attributes = append(c.Attributes, attrs...) })
}

type Span interface {
	End()
	SetAttributes(...attribute.KeyValue)
	RecordError(error)
	SetStatus(codes.Code, string)
}
type Tracer interface {
	Start(context.Context, string, ...StartOption) (context.Context, Span)
}
type TracerProvider interface {
	Tracer(string, ...TracerOption) Tracer
}
type Exporter interface{ ExportSpan(SpanData) error }
type SpanData struct {
	Name              string         `json:"name"`
	Start             time.Time      `json:"start"`
	EndTime           time.Time      `json:"end"`
	Attributes        map[string]any `json:"attributes,omitempty"`
	Status            string         `json:"status,omitempty"`
	StatusDescription string         `json:"status_description,omitempty"`
	Errors            []string       `json:"errors,omitempty"`
}

type Provider struct {
	Exporter Exporter
	Sample   bool
}

func NewProvider(exporter Exporter, sample bool) *Provider {
	return &Provider{Exporter: exporter, Sample: sample}
}
func (p *Provider) Tracer(name string, _ ...TracerOption) Tracer {
	return tracer{name: name, provider: p}
}
func (p *Provider) Shutdown(context.Context) error { return nil }

type tracer struct {
	name     string
	provider *Provider
}

func (t tracer) Start(ctx context.Context, name string, opts ...StartOption) (context.Context, Span) {
	cfg := StartConfig{}
	for _, opt := range opts {
		opt.apply(&cfg)
	}
	return ctx, &span{name: name, startedAt: time.Now().UTC(), provider: t.provider, attrs: cfg.Attributes}
}

type span struct {
	mu         sync.Mutex
	name       string
	startedAt  time.Time
	provider   *Provider
	attrs      []attribute.KeyValue
	status     codes.Code
	statusDesc string
	errors     []string
	ended      bool
}

func (s *span) End() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended {
		return
	}
	s.ended = true
	if s.provider == nil || s.provider.Exporter == nil || !s.provider.Sample {
		return
	}
	attrs := map[string]any{}
	for _, kv := range s.attrs {
		attrs[kv.Key] = kv.Value
	}
	_ = s.provider.Exporter.ExportSpan(SpanData{Name: s.name, Start: s.startedAt, EndTime: time.Now().UTC(), Attributes: attrs, Status: string(s.status), StatusDescription: s.statusDesc, Errors: s.errors})
}
func (s *span) SetAttributes(attrs ...attribute.KeyValue) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attrs = append(s.attrs, attrs...)
}
func (s *span) RecordError(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.errors = append(s.errors, err.Error())
}
func (s *span) SetStatus(code codes.Code, desc string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = code
	s.statusDesc = desc
}

type StdoutExporter struct{}

func (StdoutExporter) ExportSpan(data SpanData) error {
	b, _ := json.Marshal(data)
	_, err := fmt.Fprintln(os.Stdout, string(b))
	return err
}
