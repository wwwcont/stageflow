package resource

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
)

type Resource struct{ Attributes []attribute.KeyValue }
type Option interface{ apply(*Resource) }
type optionFunc func(*Resource)

func (f optionFunc) apply(r *Resource) { f(r) }
func WithAttributes(attrs ...attribute.KeyValue) Option {
	return optionFunc(func(r *Resource) { r.Attributes = append(r.Attributes, attrs...) })
}
func New(_ context.Context, opts ...Option) (*Resource, error) {
	r := &Resource{}
	for _, opt := range opts {
		opt.apply(r)
	}
	return r, nil
}
