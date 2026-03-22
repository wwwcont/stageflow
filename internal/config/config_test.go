package config

import (
	"context"
	"strings"
	"testing"
	"time"
)

type mapEnv map[string]string

func (m mapEnv) LookupEnv(key string) (string, bool) {
	value, ok := m[key]
	return value, ok
}

func TestLoad_NormalizesAllowedHostsAndStrings(t *testing.T) {
	cfg, err := Load(context.Background(), []string{"-allowed-host", " API.INTERNAL "}, mapEnv{
		"STAGEFLOW_SERVICE_NAME":     " stageflow-api ",
		"STAGEFLOW_ALLOWED_HOSTS":    " Example.Internal, api.internal ,EXAMPLE.INTERNAL ",
		"STAGEFLOW_LOG_LEVEL":        " DEBUG ",
		"STAGEFLOW_METRICS_PATH":     " /metrics ",
		"STAGEFLOW_TRACING_EXPORTER": " STDOUT ",
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.ServiceName != "stageflow-api" {
		t.Fatalf("ServiceName = %q, want %q", cfg.ServiceName, "stageflow-api")
	}
	if cfg.Log.Level != "debug" {
		t.Fatalf("Log.Level = %q, want %q", cfg.Log.Level, "debug")
	}
	if got, want := cfg.Runtime.AllowedHosts, []string{"example.internal", "api.internal"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("AllowedHosts = %#v, want %#v", got, want)
	}
	if cfg.Observability.Tracing.Exporter != "stdout" {
		t.Fatalf("Tracing.Exporter = %q, want %q", cfg.Observability.Tracing.Exporter, "stdout")
	}
}

func TestLoad_ReturnsInvalidEnvironmentParsingErrors(t *testing.T) {
	_, err := Load(context.Background(), nil, mapEnv{
		"STAGEFLOW_HTTP_READ_TIMEOUT":    "not-a-duration",
		"STAGEFLOW_REDIS_DB":             "abc",
		"STAGEFLOW_TRACING_SAMPLE_RATIO": "oops",
		"STAGEFLOW_TRACING_ENABLED":      "sometimes",
	})
	if err == nil {
		t.Fatal("Load() error = nil, want parsing error")
	}

	for _, expected := range []string{
		"STAGEFLOW_HTTP_READ_TIMEOUT must be a valid duration",
		"STAGEFLOW_REDIS_DB must be a valid integer",
		"STAGEFLOW_TRACING_SAMPLE_RATIO must be a valid float",
		"STAGEFLOW_TRACING_ENABLED must be a valid boolean",
	} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("Load() error = %q, want substring %q", err.Error(), expected)
		}
	}
}

func TestLoad_RejectsEmptyAllowedHostsAfterNormalization(t *testing.T) {
	_, err := Load(context.Background(), nil, mapEnv{
		"STAGEFLOW_ALLOWED_HOSTS": " , , ",
	})
	if err == nil {
		t.Fatal("Load() error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "at least one allowed outbound HTTP host is required") {
		t.Fatalf("Load() error = %q", err.Error())
	}
}

func TestLoad_DefaultAllowedHostsIncludeLocalSandbox(t *testing.T) {
	cfg, err := Load(context.Background(), nil, mapEnv{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	for _, want := range []string{"example.internal", "host.docker.internal", "localhost"} {
		var found bool
		for _, host := range cfg.Runtime.AllowedHosts {
			if host == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("AllowedHosts = %#v, missing %q", cfg.Runtime.AllowedHosts, want)
		}
	}
}

func TestLoad_AppliesDurationFlagOverride(t *testing.T) {
	cfg, err := Load(context.Background(), []string{"-http-read-timeout", "7s"}, mapEnv{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HTTP.ReadTimeout != 7*time.Second {
		t.Fatalf("ReadTimeout = %s, want %s", cfg.HTTP.ReadTimeout, 7*time.Second)
	}
}
