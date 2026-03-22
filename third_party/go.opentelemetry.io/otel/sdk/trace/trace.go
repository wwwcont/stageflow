package trace

import (
	"context"

	oteltrace "go.opentelemetry.io/otel/trace"
)

type Sampler struct{ Sample bool }

func NeverSample() Sampler                { return Sampler{Sample: false} }
func TraceIDRatioBased(r float64) Sampler { return Sampler{Sample: r > 0} }

type TracerProviderOption interface{ apply(*providerConfig) }
type providerConfig struct {
	exporter oteltrace.Exporter
	sample   bool
}
type optionFunc func(*providerConfig)

func (f optionFunc) apply(c *providerConfig) { f(c) }
func WithBatcher(exporter oteltrace.Exporter) TracerProviderOption {
	return optionFunc(func(c *providerConfig) { c.exporter = exporter })
}
func WithResource(any) TracerProviderOption { return optionFunc(func(*providerConfig) {}) }
func WithSampler(s Sampler) TracerProviderOption {
	return optionFunc(func(c *providerConfig) { c.sample = s.Sample })
}

type TracerProvider struct{ inner *oteltrace.Provider }

func NewTracerProvider(opts ...TracerProviderOption) *TracerProvider {
	cfg := providerConfig{sample: true}
	for _, opt := range opts {
		opt.apply(&cfg)
	}
	return &TracerProvider{inner: oteltrace.NewProvider(cfg.exporter, cfg.sample)}
}
func (p *TracerProvider) Tracer(name string, opts ...oteltrace.TracerOption) oteltrace.Tracer {
	return p.inner.Tracer(name, opts...)
}
func (p *TracerProvider) Shutdown(ctx context.Context) error { return p.inner.Shutdown(ctx) }
