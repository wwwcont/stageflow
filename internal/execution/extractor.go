package execution

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"stageflow/internal/domain"
)

type ExtractionError struct {
	RuleName string
	Message  string
}

func (e *ExtractionError) Error() string {
	if e == nil {
		return ""
	}
	if e.RuleName == "" {
		return fmt.Sprintf("extraction error: %s", e.Message)
	}
	return fmt.Sprintf("extraction error: %s: %s", e.RuleName, e.Message)
}

type DefaultExtractor struct{}

func NewDefaultExtractor() *DefaultExtractor {
	return &DefaultExtractor{}
}

func (e *DefaultExtractor) Extract(_ context.Context, spec domain.ExtractionSpec, response HTTPResponse) (map[string]any, error) {
	result := make(map[string]any, len(spec.Rules))
	var bodyPayload any
	var bodyParsed bool
	for _, rule := range spec.Rules {
		if err := rule.Validate(); err != nil {
			return nil, err
		}
		value, found, err := func() (any, bool, error) {
			switch rule.Source {
			case domain.ExtractionSourceBody:
				if !bodyParsed {
					if len(response.Body) == 0 {
						bodyPayload = nil
					} else if err := json.Unmarshal(response.Body, &bodyPayload); err != nil {
						return nil, false, &ExtractionError{RuleName: rule.Name, Message: fmt.Sprintf("decode JSON body: %v", err)}
					}
					bodyParsed = true
				}
				return extractFromBody(rule.Selector, bodyPayload)
			case domain.ExtractionSourceHeader:
				return extractFromHeaders(rule.Selector, response.Headers)
			case domain.ExtractionSourceStatus:
				return extractFromStatus(rule.Selector, response.StatusCode)
			default:
				return nil, false, &ExtractionError{RuleName: rule.Name, Message: fmt.Sprintf("unsupported source %q", rule.Source)}
			}
		}()
		if err != nil {
			return nil, err
		}
		if !found {
			if rule.Required {
				return nil, &ExtractionError{RuleName: rule.Name, Message: "required value not found"}
			}
			continue
		}
		if rule.JSONEncode {
			encoded, err := json.Marshal(value)
			if err != nil {
				return nil, &ExtractionError{RuleName: rule.Name, Message: fmt.Sprintf("encode JSON value: %v", err)}
			}
			result[rule.Name] = string(encoded)
			continue
		}
		result[rule.Name] = value
	}
	return result, nil
}

func extractFromBody(selector string, payload any) (any, bool, error) {
	return resolveJSONPath(payload, selector)
}

func extractFromHeaders(selector string, headers map[string][]string) (any, bool, error) {
	selector = strings.TrimPrefix(strings.TrimSpace(selector), "header.")
	if selector == "" {
		return nil, false, fmt.Errorf("header selector is empty")
	}
	for key, values := range headers {
		if strings.EqualFold(key, selector) {
			if len(values) == 0 {
				return "", true, nil
			}
			return values[0], true, nil
		}
	}
	return nil, false, nil
}

func extractFromStatus(selector string, statusCode int) (any, bool, error) {
	selector = strings.TrimSpace(selector)
	if selector != "" && selector != "status_code" {
		return nil, false, fmt.Errorf("unsupported status selector %q", selector)
	}
	return statusCode, true, nil
}
