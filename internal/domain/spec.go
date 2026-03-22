package domain

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type BackoffStrategy string

const (
	BackoffStrategyFixed       BackoffStrategy = "fixed"
	BackoffStrategyExponential BackoffStrategy = "exponential"
)

type ExtractionSource string

const (
	ExtractionSourceBody   ExtractionSource = "body"
	ExtractionSourceHeader ExtractionSource = "header"
	ExtractionSourceStatus ExtractionSource = "status"
)

type AssertionOperator string

const (
	AssertionOperatorEquals      AssertionOperator = "equals"
	AssertionOperatorNotEquals   AssertionOperator = "not_equals"
	AssertionOperatorContains    AssertionOperator = "contains"
	AssertionOperatorNotContains AssertionOperator = "not_contains"
	AssertionOperatorExists      AssertionOperator = "exists"
	AssertionOperatorRegex       AssertionOperator = "regex"
	AssertionOperatorIn          AssertionOperator = "in"
	AssertionOperatorNotEmpty    AssertionOperator = "not_empty"
)

type AssertionTarget string

const (
	AssertionTargetStatusCode AssertionTarget = "status_code"
	AssertionTargetBodyJSON   AssertionTarget = "body_json"
	AssertionTargetBodyText   AssertionTarget = "body_text"
	AssertionTargetHeader     AssertionTarget = "header"
)

type RetryPolicy struct {
	Enabled              bool            `json:"enabled"`
	MaxAttempts          int             `json:"max_attempts"`
	BackoffStrategy      BackoffStrategy `json:"backoff_strategy"`
	InitialInterval      time.Duration   `json:"initial_interval"`
	MaxInterval          time.Duration   `json:"max_interval"`
	RetryableStatusCodes []int           `json:"retryable_status_codes,omitempty"`
}

func (p RetryPolicy) Validate() error {
	if !p.Enabled {
		return nil
	}
	if p.MaxAttempts < 1 {
		return &ValidationError{Message: "retry policy max_attempts must be >= 1"}
	}
	switch p.BackoffStrategy {
	case BackoffStrategyFixed, BackoffStrategyExponential:
	default:
		return &ValidationError{Message: fmt.Sprintf("unsupported backoff strategy %q", p.BackoffStrategy)}
	}
	if p.InitialInterval < 0 || p.MaxInterval < 0 {
		return &ValidationError{Message: "retry intervals must be non-negative"}
	}
	if p.MaxInterval > 0 && p.InitialInterval > p.MaxInterval {
		return &ValidationError{Message: "retry initial interval must not exceed max interval"}
	}
	for _, code := range p.RetryableStatusCodes {
		if code < 100 || code > 599 {
			return &ValidationError{Message: fmt.Sprintf("retryable status code %d is out of range", code)}
		}
	}
	return nil
}

type RequestSpec struct {
	Method          string            `json:"method"`
	URLTemplate     string            `json:"url_template"`
	Headers         map[string]string `json:"headers,omitempty"`
	Query           map[string]string `json:"query,omitempty"`
	BodyTemplate    string            `json:"body_template,omitempty"`
	Timeout         time.Duration     `json:"timeout"`
	FollowRedirects bool              `json:"follow_redirects"`
	RetryPolicy     RetryPolicy       `json:"retry_policy"`
}

type RequestSpecOverride struct {
	Headers      map[string]string `json:"headers,omitempty"`
	Query        map[string]string `json:"query,omitempty"`
	BodyTemplate *string           `json:"body_template,omitempty"`
	Timeout      *time.Duration    `json:"timeout,omitempty"`
	RetryPolicy  *RetryPolicy      `json:"retry_policy,omitempty"`
}

func (s RequestSpec) Validate() error {
	if s.Method == "" {
		return &ValidationError{Message: "request method is required"}
	}
	if s.URLTemplate == "" {
		return &ValidationError{Message: "request url_template is required"}
	}
	if _, ok := validHTTPMethod(s.Method); !ok {
		return &ValidationError{Message: fmt.Sprintf("unsupported HTTP method %q", s.Method)}
	}
	if s.Timeout < 0 {
		return &ValidationError{Message: "request timeout must be non-negative"}
	}
	if err := s.RetryPolicy.Validate(); err != nil {
		return err
	}
	return nil
}

func (o RequestSpecOverride) Validate() error {
	if o.Timeout != nil && *o.Timeout < 0 {
		return &ValidationError{Message: "request override timeout must be non-negative"}
	}
	if o.RetryPolicy != nil {
		if err := o.RetryPolicy.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (o RequestSpecOverride) IsZero() bool {
	return len(o.Headers) == 0 &&
		len(o.Query) == 0 &&
		o.BodyTemplate == nil &&
		o.Timeout == nil &&
		o.RetryPolicy == nil
}

func (o RequestSpecOverride) Apply(base RequestSpec) (RequestSpec, error) {
	if err := o.Validate(); err != nil {
		return RequestSpec{}, err
	}
	result := base
	if len(o.Headers) > 0 {
		if result.Headers == nil {
			result.Headers = make(map[string]string, len(o.Headers))
		} else {
			cloned := make(map[string]string, len(result.Headers)+len(o.Headers))
			for k, v := range result.Headers {
				cloned[k] = v
			}
			result.Headers = cloned
		}
		for key, value := range o.Headers {
			result.Headers[key] = value
		}
	}
	if len(o.Query) > 0 {
		if result.Query == nil {
			result.Query = make(map[string]string, len(o.Query))
		} else {
			cloned := make(map[string]string, len(result.Query)+len(o.Query))
			for k, v := range result.Query {
				cloned[k] = v
			}
			result.Query = cloned
		}
		for key, value := range o.Query {
			result.Query[key] = value
		}
	}
	if o.BodyTemplate != nil {
		result.BodyTemplate = *o.BodyTemplate
	}
	if o.Timeout != nil {
		result.Timeout = *o.Timeout
	}
	if o.RetryPolicy != nil {
		result.RetryPolicy = *o.RetryPolicy
	}
	if err := result.Validate(); err != nil {
		return RequestSpec{}, err
	}
	return result, nil
}

type ExtractionRule struct {
	Name       string           `json:"name"`
	Source     ExtractionSource `json:"source"`
	Selector   string           `json:"selector"`
	Required   bool             `json:"required"`
	JSONEncode bool             `json:"json_encode"`
}

func (r ExtractionRule) Validate() error {
	if r.Name == "" {
		return &ValidationError{Message: "extraction rule name is required"}
	}
	switch r.Source {
	case ExtractionSourceBody, ExtractionSourceHeader, ExtractionSourceStatus:
	default:
		return &ValidationError{Message: fmt.Sprintf("unsupported extraction source %q", r.Source)}
	}
	if r.Source != ExtractionSourceStatus && strings.TrimSpace(r.Selector) == "" {
		return &ValidationError{Message: fmt.Sprintf("extraction rule %q requires selector", r.Name)}
	}
	return nil
}

type ExtractionSpec struct {
	Rules []ExtractionRule `json:"rules,omitempty"`
}

func (s ExtractionSpec) Validate() error {
	seen := make(map[string]struct{}, len(s.Rules))
	for _, rule := range s.Rules {
		if err := rule.Validate(); err != nil {
			return err
		}
		if _, ok := seen[rule.Name]; ok {
			return &ValidationError{Message: fmt.Sprintf("duplicate extraction rule %q", rule.Name)}
		}
		seen[rule.Name] = struct{}{}
	}
	return nil
}

type AssertionRule struct {
	Name           string            `json:"name"`
	Target         AssertionTarget   `json:"target"`
	Operator       AssertionOperator `json:"operator"`
	Selector       string            `json:"selector,omitempty"`
	ExpectedValue  string            `json:"expected_value,omitempty"`
	ExpectedValues []string          `json:"expected_values,omitempty"`
}

func (r AssertionRule) Validate() error {
	if r.Name == "" {
		return &ValidationError{Message: "assertion rule name is required"}
	}
	switch r.Target {
	case AssertionTargetStatusCode, AssertionTargetBodyJSON, AssertionTargetBodyText, AssertionTargetHeader:
	default:
		return &ValidationError{Message: fmt.Sprintf("unsupported assertion target %q", r.Target)}
	}
	switch r.Operator {
	case AssertionOperatorEquals, AssertionOperatorNotEquals, AssertionOperatorContains, AssertionOperatorNotContains, AssertionOperatorExists, AssertionOperatorRegex, AssertionOperatorIn, AssertionOperatorNotEmpty:
	default:
		return &ValidationError{Message: fmt.Sprintf("unsupported assertion operator %q", r.Operator)}
	}
	if (r.Target == AssertionTargetBodyJSON || r.Target == AssertionTargetHeader) && strings.TrimSpace(r.Selector) == "" {
		return &ValidationError{Message: fmt.Sprintf("assertion rule %q requires selector", r.Name)}
	}
	if r.Operator == AssertionOperatorIn && len(r.ExpectedValues) == 0 {
		return &ValidationError{Message: fmt.Sprintf("assertion rule %q requires expected_values", r.Name)}
	}
	if r.Operator != AssertionOperatorExists && r.Operator != AssertionOperatorIn && r.Operator != AssertionOperatorNotEmpty && strings.TrimSpace(r.ExpectedValue) == "" {
		return &ValidationError{Message: fmt.Sprintf("assertion rule %q requires expected_value", r.Name)}
	}
	return nil
}

type AssertionSpec struct {
	Rules []AssertionRule `json:"rules,omitempty"`
}

func (s AssertionSpec) Validate() error {
	seen := make(map[string]struct{}, len(s.Rules))
	for _, rule := range s.Rules {
		if err := rule.Validate(); err != nil {
			return err
		}
		if _, ok := seen[rule.Name]; ok {
			return &ValidationError{Message: fmt.Sprintf("duplicate assertion rule %q", rule.Name)}
		}
		seen[rule.Name] = struct{}{}
	}
	return nil
}

func ValidateJSONPayload(payload json.RawMessage, field string) error {
	if len(payload) == 0 {
		return nil
	}
	if !json.Valid(payload) {
		return &ValidationError{Message: fmt.Sprintf("%s must contain valid JSON", field)}
	}
	return nil
}

func validHTTPMethod(method string) (string, bool) {
	normalized := strings.ToUpper(strings.TrimSpace(method))
	switch normalized {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions:
		return normalized, true
	default:
		return normalized, false
	}
}
