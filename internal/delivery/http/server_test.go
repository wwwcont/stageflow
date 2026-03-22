package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"

	"stageflow/internal/config"
	"stageflow/internal/observability"
)

func TestServer_ExposesOperationalEndpoints(t *testing.T) {
	cfg := config.Config{
		ServiceName:   "stageflow",
		Environment:   "test",
		HTTP:          config.HTTPConfig{Address: ":0"},
		Observability: config.ObservabilityConfig{MetricsPath: "/metrics"},
		Metadata:      config.MetadataConfig{Version: "1.2.3"},
	}
	server := NewServer(cfg, zap.NewNop(), observability.NewMetrics(), nil, nil, nil, nil, nil, nil)

	tests := []struct {
		name       string
		path       string
		wantStatus int
		wantBody   map[string]string
	}{
		{name: "health", path: "/healthz", wantStatus: http.StatusOK, wantBody: map[string]string{"status": "ok"}},
		{name: "ready", path: "/readyz", wantStatus: http.StatusOK, wantBody: map[string]string{"status": "ready"}},
		{name: "version", path: "/version", wantStatus: http.StatusOK, wantBody: map[string]string{"service": "stageflow", "env": "test", "version": "1.2.3"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			resp := httptest.NewRecorder()
			server.server.Handler.ServeHTTP(resp, req)
			if resp.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", resp.Code, tt.wantStatus)
			}
			var body map[string]string
			if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}
			for key, wantValue := range tt.wantBody {
				if body[key] != wantValue {
					t.Fatalf("body[%q] = %q, want %q", key, body[key], wantValue)
				}
			}
		})
	}
}
