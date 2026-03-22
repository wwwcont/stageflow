package stdouttrace

import "go.opentelemetry.io/otel/trace"

type Option interface{}

func WithPrettyPrint() Option               { return struct{}{} }
func New(...Option) (trace.Exporter, error) { return trace.StdoutExporter{}, nil }
