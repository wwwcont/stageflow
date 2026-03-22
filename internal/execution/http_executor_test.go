package execution

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"stageflow/internal/domain"
)

func TestSafeHTTPExecutor_PolicyValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  HTTPExecutorConfig
		spec    domain.RequestSpec
		wantErr error
	}{
		{
			name:    "host not allowlisted",
			config:  HTTPExecutorConfig{AllowedHosts: []string{"svc.internal"}},
			spec:    domain.RequestSpec{Method: "GET", URLTemplate: "https://example.com"},
			wantErr: &PolicyViolationError{},
		},
		{
			name:    "ip host explicitly allowlisted passes policy",
			config:  HTTPExecutorConfig{AllowedHosts: []string{"127.0.0.1"}},
			spec:    domain.RequestSpec{Method: "GET", URLTemplate: "http://127.0.0.1"},
			wantErr: &domain.TransportError{},
		},
		{
			name:    "request body too large",
			config:  HTTPExecutorConfig{AllowedHosts: []string{"svc.internal"}, MaxRequestBodyBytes: 2},
			spec:    domain.RequestSpec{Method: "POST", URLTemplate: "https://svc.internal", BodyTemplate: "123"},
			wantErr: &PolicyViolationError{},
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
			switch tt.wantErr.(type) {
			case *PolicyViolationError:
				var policyErr *PolicyViolationError
				if !errors.As(err, &policyErr) {
					t.Fatalf("Execute() error = %v, want PolicyViolationError", err)
				}
			case *domain.TransportError:
				var transportErr *domain.TransportError
				if !errors.As(err, &transportErr) {
					t.Fatalf("Execute() error = %v, want TransportError", err)
				}
			default:
				t.Fatalf("unsupported wantErr type %T", tt.wantErr)
			}
		})
	}
}

func TestSafeHTTPExecutor_Execute_WithTestServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("env") != "staging" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("X-Request-ID", "req-123")
		w.Header().Set("Set-Cookie", "session=secret")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}

	executor, err := NewSafeHTTPExecutor(HTTPExecutorConfig{
		AllowedHosts:         []string{parsed.Hostname()},
		AllowIPHosts:         true,
		AllowLoopbackHosts:   true,
		MaxResponseBodyBytes: 1024,
	})
	if err != nil {
		t.Fatalf("NewSafeHTTPExecutor() error = %v", err)
	}

	result, err := executor.Execute(context.Background(), domain.RequestSpec{
		Method:      "POST",
		URLTemplate: server.URL + "/accounts",
		Query:       map[string]string{"env": "staging"},
		Headers: map[string]string{
			"Authorization": "Bearer secret",
			"X-API-Key":     "top-secret",
		},
		BodyTemplate: `{"name":"alice"}`,
		Timeout:      time.Second,
	}, HTTPExecutionPolicy{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Response.StatusCode != http.StatusCreated {
		t.Fatalf("StatusCode = %d", result.Response.StatusCode)
	}
	if string(result.Response.Body) != `{"ok":true}` {
		t.Fatalf("Body = %q", string(result.Response.Body))
	}
	if got := result.RequestSnapshot.Headers["Authorization"]; got != "[REDACTED]" {
		t.Fatalf("Authorization snapshot = %q", got)
	}
	if got := result.RequestSnapshot.Headers["X-API-Key"]; got != "[REDACTED]" {
		t.Fatalf("X-API-Key snapshot = %q", got)
	}
	if got := result.ResponseSnapshot.Headers["Set-Cookie"][0]; got != "[REDACTED]" {
		t.Fatalf("Set-Cookie snapshot = %q", got)
	}
}

func TestSafeHTTPExecutor_DoesNotFollowRedirects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/redirected", http.StatusFound)
	}))
	defer server.Close()

	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}

	executor, err := NewSafeHTTPExecutor(HTTPExecutorConfig{
		AllowedHosts:       []string{parsed.Hostname()},
		AllowIPHosts:       true,
		AllowLoopbackHosts: true,
	})
	if err != nil {
		t.Fatalf("NewSafeHTTPExecutor() error = %v", err)
	}

	result, err := executor.Execute(context.Background(), domain.RequestSpec{Method: "GET", URLTemplate: server.URL}, HTTPExecutionPolicy{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Response.StatusCode != http.StatusFound {
		t.Fatalf("StatusCode = %d, want %d", result.Response.StatusCode, http.StatusFound)
	}
}

func TestSafeHTTPExecutor_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}

	executor, err := NewSafeHTTPExecutor(HTTPExecutorConfig{
		AllowedHosts:       []string{parsed.Hostname()},
		AllowIPHosts:       true,
		AllowLoopbackHosts: true,
	})
	if err != nil {
		t.Fatalf("NewSafeHTTPExecutor() error = %v", err)
	}

	_, err = executor.Execute(context.Background(), domain.RequestSpec{Method: "GET", URLTemplate: server.URL, Timeout: 10 * time.Millisecond}, HTTPExecutionPolicy{})
	var timeoutErr *TimeoutError
	if err == nil || !errors.As(err, &timeoutErr) {
		t.Fatalf("Execute() error = %v, want TimeoutError", err)
	}
}

func TestSafeHTTPExecutor_Execute_AppliesPerRunPolicyOverrides(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.Header().Set("X-Echo", "secret-token")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"token":"secret-token"}`))
	}))
	defer server.Close()

	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}

	executor, err := NewSafeHTTPExecutor(HTTPExecutorConfig{
		AllowedHosts:         []string{"example.internal"},
		AllowIPHosts:         true,
		AllowLoopbackHosts:   true,
		MaxRequestBodyBytes:  1024,
		MaxResponseBodyBytes: 1024,
		DefaultTimeout:       time.Second,
	})
	if err != nil {
		t.Fatalf("NewSafeHTTPExecutor() error = %v", err)
	}

	_, err = executor.Execute(context.Background(), domain.RequestSpec{
		Method:       "POST",
		URLTemplate:  server.URL,
		BodyTemplate: "12345",
	}, HTTPExecutionPolicy{
		AllowedHosts:        []string{parsed.Hostname()},
		MaxRequestBodyBytes: 4,
		DefaultTimeout:      10 * time.Millisecond,
		SecretValues:        map[string]string{"token": "secret-token"},
	})
	var policyErr *PolicyViolationError
	if err == nil || !errors.As(err, &policyErr) {
		t.Fatalf("Execute() error = %v, want PolicyViolationError", err)
	}

	result, err := executor.Execute(context.Background(), domain.RequestSpec{
		Method:       "POST",
		URLTemplate:  server.URL,
		BodyTemplate: `{"token":"secret-token"}`,
	}, HTTPExecutionPolicy{
		AllowedHosts:        []string{parsed.Hostname()},
		MaxRequestBodyBytes: 128,
		DefaultTimeout:      250 * time.Millisecond,
		SecretValues:        map[string]string{"token": "secret-token"},
	})
	if err != nil {
		t.Fatalf("Execute() second call error = %v", err)
	}
	if got := result.RequestSnapshot.Body; got != `{"token":"[REDACTED]"}` {
		t.Fatalf("RequestSnapshot.Body = %q", got)
	}
	if got := result.ResponseSnapshot.Headers["X-Echo"][0]; got != "[REDACTED]" {
		t.Fatalf("ResponseSnapshot.Headers[X-Echo][0] = %q", got)
	}
	if got := result.ResponseSnapshot.Body; got != `{"ok":true,"token":"[REDACTED]"}` {
		t.Fatalf("ResponseSnapshot.Body = %q", got)
	}
}
