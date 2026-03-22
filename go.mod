module stageflow

go 1.23.0

replace go.uber.org/zap => ./third_party/go.uber.org/zap

replace github.com/prometheus/client_golang => ./third_party/github.com/prometheus/client_golang

replace go.opentelemetry.io/otel => ./third_party/go.opentelemetry.io/otel

require (
	github.com/prometheus/client_golang v0.0.0-00010101000000-000000000000
	go.opentelemetry.io/otel v0.0.0-00010101000000-000000000000
	go.uber.org/zap v0.0.0-00010101000000-000000000000
)
