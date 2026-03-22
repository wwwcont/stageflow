package semconv

import "go.opentelemetry.io/otel/attribute"

func ServiceName(v string) attribute.KeyValue { return attribute.String("service.name", v) }
func DeploymentEnvironment(v string) attribute.KeyValue {
	return attribute.String("deployment.environment", v)
}
func ServiceVersion(v string) attribute.KeyValue { return attribute.String("service.version", v) }
