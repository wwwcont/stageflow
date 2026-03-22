package execution

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"stageflow/internal/domain"
)

type RenderData struct {
	Run       RunRenderData
	Workspace WorkspaceRenderData
	Secrets   map[string]string
	Vars      map[string]any
	Steps     map[string]map[string]any
}

type RunRenderData struct {
	ID    string
	Input map[string]any
}

type WorkspaceRenderData struct {
	Vars map[string]string
}

type TemplateError struct {
	Template  string
	Reference string
	Message   string
}

func (e *TemplateError) Error() string {
	if e == nil {
		return ""
	}
	if e.Reference == "" {
		return fmt.Sprintf("template error: %s", e.Message)
	}
	return fmt.Sprintf("template error: %s (%s)", e.Message, e.Reference)
}

var templateExpression = regexp.MustCompile(`\{\{\s*([^{}]+?)\s*\}\}`)

type DefaultTemplateRenderer struct {
	now  func() time.Time
	uuid func() (string, error)
}

func NewDefaultTemplateRenderer() *DefaultTemplateRenderer {
	return &DefaultTemplateRenderer{now: func() time.Time { return time.Now().UTC() }, uuid: newUUID}
}

func NewRenderData(run domain.FlowRun, workspace domain.Workspace, runtimeVars map[string]any, stepValues map[string]map[string]any) (RenderData, error) {
	var input map[string]any
	if len(run.InputJSON) > 0 {
		if err := json.Unmarshal(run.InputJSON, &input); err != nil {
			return RenderData{}, fmt.Errorf("decode run input: %w", err)
		}
	}
	if input == nil {
		input = map[string]any{}
	}
	if stepValues == nil {
		stepValues = map[string]map[string]any{}
	}
	if runtimeVars == nil {
		runtimeVars = map[string]any{}
	}
	return RenderData{
		Run:       RunRenderData{ID: string(run.ID), Input: input},
		Workspace: WorkspaceRenderData{Vars: workspace.VariableMap()},
		Secrets:   workspace.SecretMap(),
		Vars:      runtimeVars,
		Steps:     stepValues,
	}, nil
}

func (r *DefaultTemplateRenderer) Resolve(_ context.Context, spec domain.RequestSpec, data RenderData) (ResolvedRequest, error) {
	method := spec.Method
	urlValue, err := r.RenderString(spec.URLTemplate, data)
	if err != nil {
		return ResolvedRequest{}, err
	}
	body, err := r.RenderString(spec.BodyTemplate, data)
	if err != nil {
		return ResolvedRequest{}, err
	}
	headers := make(map[string]string, len(spec.Headers))
	for key, value := range spec.Headers {
		rendered, err := r.RenderString(value, data)
		if err != nil {
			return ResolvedRequest{}, err
		}
		headers[key] = rendered
	}
	if len(spec.Query) > 0 {
		parsedURL, err := url.Parse(urlValue)
		if err != nil {
			return ResolvedRequest{}, &TemplateError{Template: spec.URLTemplate, Message: fmt.Sprintf("rendered URL is invalid: %v", err)}
		}
		query := parsedURL.Query()
		for key, value := range spec.Query {
			rendered, err := r.RenderString(value, data)
			if err != nil {
				return ResolvedRequest{}, err
			}
			query.Set(key, rendered)
		}
		parsedURL.RawQuery = query.Encode()
		urlValue = parsedURL.String()
	}
	return ResolvedRequest{Method: method, URL: urlValue, Headers: headers, Body: body}, nil
}

func (r *DefaultTemplateRenderer) RenderString(template string, data RenderData) (string, error) {
	matches := templateExpression.FindAllStringSubmatchIndex(template, -1)
	if len(matches) == 0 {
		return template, nil
	}
	var out strings.Builder
	last := 0
	for _, match := range matches {
		out.WriteString(template[last:match[0]])
		expr := strings.TrimSpace(template[match[2]:match[3]])
		resolved, err := r.resolveExpression(expr, data)
		if err != nil {
			return "", err
		}
		out.WriteString(resolved)
		last = match[1]
	}
	out.WriteString(template[last:])
	return out.String(), nil
}

func (r *DefaultTemplateRenderer) ValidateString(template string, data RenderData) error {
	_, err := r.RenderString(template, data)
	return err
}

func (r *DefaultTemplateRenderer) resolveExpression(expr string, data RenderData) (string, error) {
	switch {
	case expr == "uuid":
		value, err := r.uuid()
		if err != nil {
			return "", &TemplateError{Reference: expr, Message: fmt.Sprintf("generate uuid: %v", err)}
		}
		return value, nil
	case expr == "timestamp":
		return r.now().Format(time.RFC3339), nil
	case expr == "run.id":
		if data.Run.ID == "" {
			return "", &TemplateError{Reference: expr, Message: "run.id is empty"}
		}
		return data.Run.ID, nil
	case strings.HasPrefix(expr, "workspace.vars."):
		name := strings.TrimPrefix(expr, "workspace.vars.")
		value, ok := data.Workspace.Vars[name]
		if !ok {
			return "", &TemplateError{Reference: expr, Message: "workspace variable does not exist"}
		}
		return value, nil
	case strings.HasPrefix(expr, "secret."):
		name := strings.TrimPrefix(expr, "secret.")
		value, ok := data.Secrets[name]
		if !ok {
			return "", &TemplateError{Reference: expr, Message: "secret does not exist"}
		}
		return value, nil
	case strings.HasPrefix(expr, "vars."):
		value, ok, err := resolveJSONPath(data.Vars, "$."+strings.TrimPrefix(expr, "vars."))
		if err != nil {
			return "", &TemplateError{Reference: expr, Message: err.Error()}
		}
		if !ok {
			return "", &TemplateError{Reference: expr, Message: "runtime variable does not exist"}
		}
		return stringifyTemplateValue(value), nil
	case strings.HasPrefix(expr, "run.input."):
		value, ok, err := resolveJSONPath(data.Run.Input, "$."+strings.TrimPrefix(expr, "run.input."))
		if err != nil {
			return "", &TemplateError{Reference: expr, Message: err.Error()}
		}
		if !ok {
			return "", &TemplateError{Reference: expr, Message: "variable does not exist"}
		}
		return stringifyTemplateValue(value), nil
	case strings.HasPrefix(expr, "steps."):
		parts := strings.SplitN(strings.TrimPrefix(expr, "steps."), ".", 2)
		if len(parts) != 2 {
			return "", &TemplateError{Reference: expr, Message: "step reference must have form steps.step_name.value"}
		}
		stepValues, ok := data.Steps[parts[0]]
		if !ok {
			return "", &TemplateError{Reference: expr, Message: "step output does not exist"}
		}
		value, ok, err := resolveJSONPath(stepValues, "$."+parts[1])
		if err != nil {
			return "", &TemplateError{Reference: expr, Message: err.Error()}
		}
		if !ok {
			return "", &TemplateError{Reference: expr, Message: "step value does not exist"}
		}
		return stringifyTemplateValue(value), nil
	default:
		return "", &TemplateError{Reference: expr, Message: "unsupported template expression"}
	}
}

func stringifyTemplateValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case nil:
		return ""
	default:
		return fmt.Sprint(typed)
	}
}

func newUUID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	hexValue := hex.EncodeToString(buf)
	return fmt.Sprintf("%s-%s-%s-%s-%s", hexValue[:8], hexValue[8:12], hexValue[12:16], hexValue[16:20], hexValue[20:32]), nil
}
