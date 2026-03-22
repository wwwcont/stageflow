package execution

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"stageflow/internal/domain"
)

type AssertionErrors struct {
	Failures []domain.AssertionFailureError
}

func (e *AssertionErrors) Error() string {
	if e == nil || len(e.Failures) == 0 {
		return ""
	}
	parts := make([]string, 0, len(e.Failures))
	for _, failure := range e.Failures {
		parts = append(parts, failure.Error())
	}
	return strings.Join(parts, "; ")
}

func (e *AssertionErrors) Unwrap() error {
	return domain.ErrAssertionFailure
}

type DefaultAsserter struct{}

func NewDefaultAsserter() *DefaultAsserter {
	return &DefaultAsserter{}
}

func (a *DefaultAsserter) Assert(_ context.Context, spec domain.AssertionSpec, response HTTPResponse) error {
	var failures []domain.AssertionFailureError
	var bodyPayload any
	var bodyParsed bool
	for _, rule := range spec.Rules {
		if err := rule.Validate(); err != nil {
			return err
		}
		actual, exists, err := func() (any, bool, error) {
			switch rule.Target {
			case domain.AssertionTargetStatusCode:
				return response.StatusCode, true, nil
			case domain.AssertionTargetHeader:
				return extractFromHeaders(rule.Selector, response.Headers)
			case domain.AssertionTargetBodyText:
				return string(response.Body), len(response.Body) > 0, nil
			case domain.AssertionTargetBodyJSON:
				if !bodyParsed {
					if len(response.Body) == 0 {
						bodyPayload = nil
					} else if err := json.Unmarshal(response.Body, &bodyPayload); err != nil {
						return nil, false, fmt.Errorf("decode JSON body: %w", err)
					}
					bodyParsed = true
				}
				return resolveJSONPath(bodyPayload, rule.Selector)
			default:
				return nil, false, fmt.Errorf("unsupported assertion target %q", rule.Target)
			}
		}()
		if err != nil {
			return err
		}
		if failure := evaluateAssertionRule(rule, actual, exists); failure != nil {
			failures = append(failures, *failure)
		}
	}
	if len(failures) > 0 {
		return &AssertionErrors{Failures: failures}
	}
	return nil
}

func evaluateAssertionRule(rule domain.AssertionRule, actual any, exists bool) *domain.AssertionFailureError {
	fail := func(message string) *domain.AssertionFailureError {
		return &domain.AssertionFailureError{RuleName: rule.Name, Message: message}
	}

	switch rule.Operator {
	case domain.AssertionOperatorExists:
		if !exists {
			return fail("expected value to exist")
		}
	case domain.AssertionOperatorEquals:
		if !exists || fmt.Sprint(actual) != rule.ExpectedValue {
			return fail(fmt.Sprintf("expected %q, got %q", rule.ExpectedValue, fmt.Sprint(actual)))
		}
	case domain.AssertionOperatorContains:
		if !exists || !strings.Contains(fmt.Sprint(actual), rule.ExpectedValue) {
			return fail(fmt.Sprintf("expected %q to contain %q", fmt.Sprint(actual), rule.ExpectedValue))
		}
	case domain.AssertionOperatorNotEmpty:
		if !exists || isEmptyValue(actual) {
			return fail("expected value to be not empty")
		}
	default:
		return fail(fmt.Sprintf("operator %q is not implemented in assertions engine", rule.Operator))
	}
	return nil
}

func isEmptyValue(value any) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(typed) == ""
	case []any:
		return len(typed) == 0
	case map[string]any:
		return len(typed) == 0
	default:
		return fmt.Sprint(typed) == ""
	}
}
