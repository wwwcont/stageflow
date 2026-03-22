package execution

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.uber.org/zap"

	"stageflow/internal/domain"
	"stageflow/internal/observability"
)

const (
	defaultMaxRequestBodyBytes  int64 = 1 << 20
	defaultMaxResponseBodyBytes int64 = 2 << 20
	defaultDialTimeout                = 5 * time.Second
	defaultExecutorTimeout            = 10 * time.Second
)

type HTTPExecutorConfig struct {
	AllowedHosts         []string
	MaxRequestBodyBytes  int64
	MaxResponseBodyBytes int64
	DefaultTimeout       time.Duration
	DialTimeout          time.Duration
	SensitiveHeaders     []string
	AllowLoopbackHosts   bool
	AllowIPHosts         bool
}

type HTTPExecutionResult struct {
	Response         HTTPResponse
	Duration         time.Duration
	RequestSnapshot  HTTPRequestSnapshot
	ResponseSnapshot HTTPResponseSnapshot
}

type HTTPRequestSnapshot struct {
	Method  string
	URL     string
	Headers map[string]string
	Body    string
}

type HTTPResponseSnapshot struct {
	StatusCode int
	Headers    map[string][]string
	Body       string
}

type PolicyViolationError struct {
	Reason string
}

func (e *PolicyViolationError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("policy violation: %s", e.Reason)
}

type TimeoutError struct {
	Operation string
}

func (e *TimeoutError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("timeout: %s", e.Operation)
}

type SafeHTTPExecutor struct {
	config       HTTPExecutorConfig
	allowedHosts map[string]struct{}
	sensitive    map[string]struct{}
	logger       *zap.Logger
}

func NewSafeHTTPExecutor(cfg HTTPExecutorConfig) (*SafeHTTPExecutor, error) {
	if len(cfg.AllowedHosts) == 0 {
		return nil, fmt.Errorf("allowed hosts are required")
	}
	if cfg.MaxRequestBodyBytes <= 0 {
		cfg.MaxRequestBodyBytes = defaultMaxRequestBodyBytes
	}
	if cfg.MaxResponseBodyBytes <= 0 {
		cfg.MaxResponseBodyBytes = defaultMaxResponseBodyBytes
	}
	if cfg.DefaultTimeout <= 0 {
		cfg.DefaultTimeout = defaultExecutorTimeout
	}
	if cfg.DialTimeout <= 0 {
		cfg.DialTimeout = defaultDialTimeout
	}
	allowedHosts := make(map[string]struct{}, len(cfg.AllowedHosts)+2)
	for _, host := range cfg.AllowedHosts {
		normalized := strings.ToLower(strings.TrimSpace(host))
		if normalized == "" {
			return nil, fmt.Errorf("allowed hosts must not contain empty values")
		}
		allowedHosts[normalized] = struct{}{}
		if normalized == "localhost" || isLoopbackIP(normalized) {
			allowedHosts["localhost"] = struct{}{}
			allowedHosts["127.0.0.1"] = struct{}{}
			allowedHosts["::1"] = struct{}{}
		}
	}
	sensitive := defaultSensitiveHeaders()
	for _, header := range cfg.SensitiveHeaders {
		sensitive[strings.ToLower(strings.TrimSpace(header))] = struct{}{}
	}

	return &SafeHTTPExecutor{config: cfg, allowedHosts: allowedHosts, sensitive: sensitive, logger: zap.NewNop()}, nil
}

func (e *SafeHTTPExecutor) SetLogger(logger *zap.Logger) {
	if logger == nil {
		e.logger = zap.NewNop()
		return
	}
	e.logger = logger
}

func (e *SafeHTTPExecutor) Execute(ctx context.Context, spec domain.RequestSpec, policy HTTPExecutionPolicy) (HTTPExecutionResult, error) {
	requestURL, err := e.buildURL(spec)
	if err == nil {
		ctx, span := otel.Tracer("stageflow/http-executor").Start(ctx, "http.execution")
		if meta, ok := observability.RunContextFromContext(ctx); ok {
			span.SetAttributes(observability.RunContextAttributes(meta)...)
		}
		span.SetAttributes(
			attribute.String("http.method", spec.Method),
			attribute.String("url.scheme", requestURL.Scheme),
			attribute.String("server.address", requestURL.Hostname()),
			attribute.String("url.path", requestURL.Path),
		)
		defer span.End()
		result, execErr := e.execute(ctx, spec, requestURL, policy)
		if execErr != nil {
			span.RecordError(execErr)
			span.SetStatus(codes.Error, execErr.Error())
			return HTTPExecutionResult{}, execErr
		}
		span.SetAttributes(
			attribute.Int("http.status_code", result.Response.StatusCode),
			attribute.String("http.duration", result.Duration.String()),
		)
		span.SetStatus(codes.Ok, "")
		fields := []zap.Field{
			zap.String("method", spec.Method),
			zap.String("host", requestURL.Hostname()),
			zap.String("path", requestURL.Path),
			zap.Int("status_code", result.Response.StatusCode),
			zap.Duration("duration", result.Duration),
		}
		if meta, ok := observability.RunContextFromContext(ctx); ok {
			fields = append(fields, observability.RunContextLogFields(meta)...)
		}
		e.logger.Debug("http execution completed", fields...)
		return result, nil
	}
	return HTTPExecutionResult{}, err
}

func (e *SafeHTTPExecutor) execute(ctx context.Context, spec domain.RequestSpec, requestURL *url.URL, policy HTTPExecutionPolicy) (HTTPExecutionResult, error) {
	if err := spec.Validate(); err != nil {
		return HTTPExecutionResult{}, err
	}
	networkPolicy := e.resolveNetworkPolicy(policy)
	if err := e.validateURLPolicy(requestURL, networkPolicy); err != nil {
		return HTTPExecutionResult{}, err
	}
	maxRequestBodyBytes := e.config.MaxRequestBodyBytes
	if policy.MaxRequestBodyBytes > 0 {
		maxRequestBodyBytes = policy.MaxRequestBodyBytes
	}
	if int64(len(spec.BodyTemplate)) > maxRequestBodyBytes {
		return HTTPExecutionResult{}, &PolicyViolationError{Reason: "request body exceeds configured limit"}
	}

	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = e.config.DefaultTimeout
		if policy.DefaultTimeout > 0 {
			timeout = policy.DefaultTimeout
		}
	}
	timedCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(timedCtx, spec.Method, requestURL.String(), strings.NewReader(spec.BodyTemplate))
	if err != nil {
		return HTTPExecutionResult{}, &domain.TransportError{Operation: "build request", Cause: err}
	}
	for key, value := range spec.Headers {
		req.Header.Set(key, value)
	}
	requestSnapshot := HTTPRequestSnapshot{Method: req.Method, URL: requestURL.String(), Headers: redactStringHeaders(spec.Headers, e.sensitive), Body: spec.BodyTemplate}
	requestSnapshot = sanitizeSecretValue(requestSnapshot, policy.SecretValues).(HTTPRequestSnapshot)

	startedAt := time.Now()
	client := e.newClient(timeout, networkPolicy)
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || timedCtx.Err() == context.DeadlineExceeded {
			return HTTPExecutionResult{}, &TimeoutError{Operation: "http request"}
		}
		var policyErr *PolicyViolationError
		if errors.As(err, &policyErr) {
			return HTTPExecutionResult{}, policyErr
		}
		return HTTPExecutionResult{}, &domain.TransportError{Operation: "perform request", Cause: err}
	}
	defer resp.Body.Close()

	body, err := readLimitedBody(resp.Body, e.config.MaxResponseBodyBytes)
	if err != nil {
		var policyErr *PolicyViolationError
		if errors.As(err, &policyErr) {
			return HTTPExecutionResult{}, policyErr
		}
		return HTTPExecutionResult{}, &domain.TransportError{Operation: "read response body", Cause: err}
	}
	responseHeaders := cloneHeader(resp.Header)
	result := HTTPExecutionResult{
		Response:        HTTPResponse{StatusCode: resp.StatusCode, Headers: responseHeaders, Body: body},
		Duration:        time.Since(startedAt),
		RequestSnapshot: requestSnapshot,
		ResponseSnapshot: HTTPResponseSnapshot{
			StatusCode: resp.StatusCode,
			Headers:    redactHeaderValues(responseHeaders, e.sensitive),
			Body:       string(body),
		},
	}
	result.ResponseSnapshot = sanitizeSecretValue(result.ResponseSnapshot, policy.SecretValues).(HTTPResponseSnapshot)
	return result, nil
}

func (e *SafeHTTPExecutor) buildURL(spec domain.RequestSpec) (*url.URL, error) {
	parsed, err := url.Parse(spec.URLTemplate)
	if err != nil {
		return nil, &domain.TransportError{Operation: "parse request URL", Cause: err}
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, &PolicyViolationError{Reason: fmt.Sprintf("unsupported scheme %q", parsed.Scheme)}
	}
	if parsed.User != nil {
		return nil, &PolicyViolationError{Reason: "userinfo in URL is not allowed"}
	}
	if parsed.Fragment != "" {
		return nil, &PolicyViolationError{Reason: "URL fragments are not allowed"}
	}
	query := parsed.Query()
	for key, value := range spec.Query {
		query.Set(key, value)
	}
	parsed.RawQuery = query.Encode()
	return parsed, nil
}

type resolvedNetworkPolicy struct {
	allowedHosts  map[string]struct{}
	allowLoopback bool
	allowIPHosts  bool
}

func (e *SafeHTTPExecutor) resolveNetworkPolicy(policy HTTPExecutionPolicy) resolvedNetworkPolicy {
	allowedHosts := normalizedAllowedHosts(policy, e.allowedHosts)
	resolved := resolvedNetworkPolicy{
		allowedHosts:  allowedHosts,
		allowLoopback: e.config.AllowLoopbackHosts,
		allowIPHosts:  e.config.AllowIPHosts,
	}
	for host := range allowedHosts {
		if strings.EqualFold(host, "localhost") {
			resolved.allowLoopback = true
			continue
		}
		if ip := net.ParseIP(host); ip != nil {
			resolved.allowIPHosts = true
			if ip.IsLoopback() {
				resolved.allowLoopback = true
			}
		}
	}
	return resolved
}

func (e *SafeHTTPExecutor) validateURLPolicy(requestURL *url.URL, policy resolvedNetworkPolicy) error {
	host := strings.ToLower(requestURL.Hostname())
	if host == "" {
		return &PolicyViolationError{Reason: "request host is empty"}
	}
	if _, allowed := policy.allowedHosts[host]; !allowed {
		return &PolicyViolationError{Reason: fmt.Sprintf("host %q is not allowlisted", host)}
	}
	if ip := net.ParseIP(host); ip != nil {
		if !policy.allowIPHosts {
			return &PolicyViolationError{Reason: "IP hosts are not allowed"}
		}
		if ip.IsLoopback() && !policy.allowLoopback {
			return &PolicyViolationError{Reason: "loopback hosts are not allowed"}
		}
	}
	if strings.EqualFold(host, "localhost") && !policy.allowLoopback {
		return &PolicyViolationError{Reason: "localhost is not allowed"}
	}
	return nil
}

func normalizedAllowedHosts(policy HTTPExecutionPolicy, defaults map[string]struct{}) map[string]struct{} {
	if len(policy.AllowedHosts) == 0 {
		return defaults
	}
	allowedHosts := make(map[string]struct{}, len(policy.AllowedHosts)+2)
	for _, host := range policy.AllowedHosts {
		normalized := strings.ToLower(strings.TrimSpace(host))
		if normalized == "" {
			continue
		}
		allowedHosts[normalized] = struct{}{}
		if normalized == "localhost" || isLoopbackIP(normalized) {
			allowedHosts["localhost"] = struct{}{}
			allowedHosts["127.0.0.1"] = struct{}{}
			allowedHosts["::1"] = struct{}{}
		}
	}
	if len(allowedHosts) == 0 {
		return defaults
	}
	return allowedHosts
}

func isLoopbackIP(host string) bool {
	ip := net.ParseIP(strings.Trim(strings.ToLower(strings.TrimSpace(host)), "[]"))
	return ip != nil && ip.IsLoopback()
}

func (e *SafeHTTPExecutor) newClient(timeout time.Duration, policy resolvedNetworkPolicy) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy: nil,
			DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				return e.dialContext(ctx, network, address, policy)
			},
			ForceAttemptHTTP2:     false,
			ResponseHeaderTimeout: timeout,
			DisableCompression:    false,
		},
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func (e *SafeHTTPExecutor) dialContext(ctx context.Context, network, address string, policy resolvedNetworkPolicy) (net.Conn, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	if ip := net.ParseIP(host); ip != nil {
		if !policy.allowIPHosts {
			return nil, &PolicyViolationError{Reason: "IP hosts are not allowed"}
		}
		if ip.IsLoopback() && !policy.allowLoopback {
			return nil, &PolicyViolationError{Reason: "loopback hosts are not allowed"}
		}
	}
	if strings.EqualFold(host, "localhost") && !policy.allowLoopback {
		return nil, &PolicyViolationError{Reason: "localhost is not allowed"}
	}
	dialer := &net.Dialer{Timeout: e.config.DialTimeout}
	return dialer.DialContext(ctx, network, address)
}

func readLimitedBody(body io.Reader, limit int64) ([]byte, error) {
	limited := io.LimitReader(body, limit+1)
	payload, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(payload)) > limit {
		return nil, &PolicyViolationError{Reason: "response body exceeds configured limit"}
	}
	return payload, nil
}

func defaultSensitiveHeaders() map[string]struct{} {
	return map[string]struct{}{
		"authorization":       {},
		"cookie":              {},
		"set-cookie":          {},
		"proxy-authorization": {},
		"x-api-key":           {},
	}
}

func redactStringHeaders(headers map[string]string, sensitive map[string]struct{}) map[string]string {
	result := make(map[string]string, len(headers))
	for key, value := range headers {
		if _, ok := sensitive[strings.ToLower(key)]; ok {
			result[key] = "[REDACTED]"
			continue
		}
		result[key] = value
	}
	return result
}

func redactHeaders(headers http.Header, sensitive map[string]struct{}) map[string]string {
	result := make(map[string]string, len(headers))
	for key, values := range headers {
		if _, ok := sensitive[strings.ToLower(key)]; ok {
			result[key] = "[REDACTED]"
			continue
		}
		if len(values) == 0 {
			result[key] = ""
			continue
		}
		result[key] = values[0]
	}
	return result
}

func redactHeaderValues(headers map[string][]string, sensitive map[string]struct{}) map[string][]string {
	result := make(map[string][]string, len(headers))
	for key, values := range headers {
		if _, ok := sensitive[strings.ToLower(key)]; ok {
			result[key] = []string{"[REDACTED]"}
			continue
		}
		cloned := make([]string, len(values))
		copy(cloned, values)
		result[key] = cloned
	}
	return result
}

func cloneHeader(headers http.Header) map[string][]string {
	cloned := make(map[string][]string, len(headers))
	for key, values := range headers {
		copyValues := make([]string, len(values))
		copy(copyValues, values)
		cloned[key] = copyValues
	}
	return cloned
}
