package execution

import (
	"context"
	"errors"
	"strings"
	"testing"

	"stageflow/internal/domain"
)

func TestSafeHTTPExecutor_AllowlistValidation(t *testing.T) {
	tests := []struct {
		name   string
		config HTTPExecutorConfig
		spec   domain.RequestSpec
		want   string
	}{
		{
			name:   "allows localhost when explicitly allowlisted",
			config: HTTPExecutorConfig{AllowedHosts: []string{"localhost"}},
			spec:   domain.RequestSpec{Method: "GET", URLTemplate: "http://localhost:8080/health"},
			want:   "perform request",
		},
		{
			name:   "allows 127.0.0.1 when localhost is allowlisted",
			config: HTTPExecutorConfig{AllowedHosts: []string{"localhost"}},
			spec:   domain.RequestSpec{Method: "GET", URLTemplate: "http://127.0.0.1:65530/health"},
			want:   "perform request",
		},
		{
			name:   "rejects userinfo in url",
			config: HTTPExecutorConfig{AllowedHosts: []string{"example.internal"}},
			spec:   domain.RequestSpec{Method: "GET", URLTemplate: "https://user:pass@example.internal/health"},
			want:   "userinfo",
		},
		{
			name:   "rejects fragments",
			config: HTTPExecutorConfig{AllowedHosts: []string{"example.internal"}},
			spec:   domain.RequestSpec{Method: "GET", URLTemplate: "https://example.internal/health#frag"},
			want:   "fragments",
		},
		{
			name:   "allows loopback when configured",
			config: HTTPExecutorConfig{AllowedHosts: []string{"127.0.0.1"}, AllowIPHosts: true, AllowLoopbackHosts: true},
			spec:   domain.RequestSpec{Method: "GET", URLTemplate: "http://127.0.0.1:65530/health"},
			want:   "perform request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor, err := NewSafeHTTPExecutor(tt.config)
			if err != nil {
				t.Fatalf("NewSafeHTTPExecutor() error = %v", err)
			}
			_, err = executor.Execute(context.Background(), tt.spec, HTTPExecutionPolicy{})
			if err == nil {
				t.Fatal("Execute() error = nil, want non-nil")
			}
			if tt.want == "perform request" {
				var transportErr *domain.TransportError
				if !errors.As(err, &transportErr) {
					t.Fatalf("Execute() error = %v, want transport error after passing allowlist", err)
				}
				return
			}
			if err.Error() == "" || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Execute() error = %v, want contains %q", err, tt.want)
			}
		})
	}
}
