package otel

import (
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

var provider trace.TracerProvider = trace.NewProvider(nil, false)
var propagator propagation.TextMapPropagator = propagation.TraceContext{}

func SetTracerProvider(p trace.TracerProvider) {
	if p != nil {
		provider = p
	}
}
func Tracer(name string, opts ...trace.TracerOption) trace.Tracer {
	return provider.Tracer(name, opts...)
}
func SetTextMapPropagator(p propagation.TextMapPropagator) {
	if p != nil {
		propagator = p
	}
}
func GetTextMapPropagator() propagation.TextMapPropagator { return propagator }
