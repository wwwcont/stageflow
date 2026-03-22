package propagation

import (
	"context"
	"net/http"
)

type TextMapCarrier interface {
	Get(string) string
	Set(string, string)
	Keys() []string
}
type HeaderCarrier http.Header

func (c HeaderCarrier) Get(key string) string { return http.Header(c).Get(key) }
func (c HeaderCarrier) Set(key, value string) { http.Header(c).Set(key, value) }
func (c HeaderCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}

type TextMapPropagator interface {
	Extract(context.Context, TextMapCarrier) context.Context
}
type TraceContext struct{}

func (TraceContext) Extract(ctx context.Context, _ TextMapCarrier) context.Context { return ctx }
