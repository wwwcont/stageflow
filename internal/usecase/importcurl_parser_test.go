package usecase

import (
	"context"
	"testing"

	"stageflow/internal/domain"
)

func TestDefaultCurlCommandParser_Parse_BasicPOST(t *testing.T) {
	parser := NewDefaultCurlCommandParser()

	spec, err := parser.Parse(`curl -X POST -H "Content-Type: application/json" -H "Authorization: Bearer token" -d '{"name":"john doe"}' 'https://api.example.com/v1/users?env=staging'`)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if spec.Method != "POST" {
		t.Fatalf("Method = %q, want POST", spec.Method)
	}
	if spec.URLTemplate != "https://api.example.com/v1/users" {
		t.Fatalf("URLTemplate = %q", spec.URLTemplate)
	}
	if got := spec.Query["env"]; got != "staging" {
		t.Fatalf("Query env = %q, want staging", got)
	}
	if got := spec.Headers["Content-Type"]; got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	if got := spec.Headers["Authorization"]; got != "Bearer token" {
		t.Fatalf("Authorization = %q, want Bearer token", got)
	}
	if spec.BodyTemplate != `{"name":"john doe"}` {
		t.Fatalf("BodyTemplate = %q", spec.BodyTemplate)
	}
}

func TestDefaultCurlCommandParser_Parse_InferPOSTFromData(t *testing.T) {
	parser := NewDefaultCurlCommandParser()

	spec, err := parser.Parse(`curl --data-raw 'name=alice smith' https://example.com/forms`)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if spec.Method != "POST" {
		t.Fatalf("Method = %q, want POST", spec.Method)
	}
	if spec.BodyTemplate != "name=alice smith" {
		t.Fatalf("BodyTemplate = %q", spec.BodyTemplate)
	}
}

func TestDefaultCurlCommandParser_Parse_SupportsInlineFlagValues(t *testing.T) {
	parser := NewDefaultCurlCommandParser()

	spec, err := parser.Parse(`curl --request=PATCH --header=X-Trace-ID:abc-123 --data-raw=active=true --url=https://example.com/api/users/42`)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if spec.Method != "PATCH" {
		t.Fatalf("Method = %q, want PATCH", spec.Method)
	}
	if spec.URLTemplate != "https://example.com/api/users/42" {
		t.Fatalf("URLTemplate = %q", spec.URLTemplate)
	}
	if got := spec.Headers["X-Trace-ID"]; got != "abc-123" {
		t.Fatalf("X-Trace-ID = %q, want abc-123", got)
	}
	if spec.BodyTemplate != "active=true" {
		t.Fatalf("BodyTemplate = %q", spec.BodyTemplate)
	}
}

func TestDefaultCurlCommandParser_Parse_RejectUnsupportedFlag(t *testing.T) {
	parser := NewDefaultCurlCommandParser()

	_, err := parser.Parse(`curl --compressed https://example.com`)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if _, ok := err.(*UnsupportedCurlFlagError); !ok {
		t.Fatalf("error type = %T, want *UnsupportedCurlFlagError", err)
	}
}

func TestDefaultCurlCommandParser_Parse_RejectDuplicateQueryValues(t *testing.T) {
	parser := NewDefaultCurlCommandParser()

	_, err := parser.Parse(`curl 'https://example.com/search?q=one&q=two'`)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestDefaultCurlCommandParser_Parse_UnclosedQuote(t *testing.T) {
	parser := NewDefaultCurlCommandParser()

	_, err := parser.Parse(`curl -H 'Content-Type: application/json https://example.com`)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestDefaultCurlCommandParser_Parse_MultilineWithLineContinuations(t *testing.T) {
	parser := NewDefaultCurlCommandParser()

	command := "curl -X POST http://localhost:8091/api/users \\\n  -H \"Content-Type: application/json\" \\\n  -d '{\n    \"name\": \"Ivan Ivanov\",\n    \"email\": \"ivan@example.com\"\n  }'"

	spec, err := parser.Parse(command)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if spec.Method != "POST" {
		t.Fatalf("Method = %q, want POST", spec.Method)
	}
	if spec.URLTemplate != "http://localhost:8091/api/users" {
		t.Fatalf("URLTemplate = %q", spec.URLTemplate)
	}
	if got := spec.Headers["Content-Type"]; got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	if spec.BodyTemplate != "{\n    \"name\": \"Ivan Ivanov\",\n    \"email\": \"ivan@example.com\"\n  }" {
		t.Fatalf("BodyTemplate = %q", spec.BodyTemplate)
	}
}

func TestDefaultCurlCommandParser_Parse_CRLFLineContinuations(t *testing.T) {
	parser := NewDefaultCurlCommandParser()

	command := "curl -X POST http://localhost:8091/api/reset \\\r\n  -H \"X-Test: 1\" \\\r\n  --data-raw '{}'"

	spec, err := parser.Parse(command)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if spec.URLTemplate != "http://localhost:8091/api/reset" {
		t.Fatalf("URLTemplate = %q", spec.URLTemplate)
	}
	if got := spec.Headers["X-Test"]; got != "1" {
		t.Fatalf("X-Test = %q, want 1", got)
	}
	if spec.BodyTemplate != "{}" {
		t.Fatalf("BodyTemplate = %q", spec.BodyTemplate)
	}
}

func TestCurlImportService_ImportCurl(t *testing.T) {
	service, err := NewCurlImportService(NewDefaultCurlCommandParser())
	if err != nil {
		t.Fatalf("NewCurlImportService() error = %v", err)
	}

	result, err := service.ImportCurl(context.Background(), ImportCurlInput{Command: `curl https://example.com/health`})
	if err != nil {
		t.Fatalf("ImportCurl() error = %v", err)
	}
	if result.RequestSpec.Method != "GET" {
		t.Fatalf("Method = %q, want GET", result.RequestSpec.Method)
	}
	if result.RequestSpec.URLTemplate != "https://example.com/health" {
		t.Fatalf("URLTemplate = %q", result.RequestSpec.URLTemplate)
	}
}

func TestTokenizeCurlCommand_PreservesQuotedSpaces(t *testing.T) {
	tokens, err := tokenizeCurlCommand(`curl -H "X-Title: hello world" -d 'name=john doe' https://example.com`)
	if err != nil {
		t.Fatalf("tokenizeCurlCommand() error = %v", err)
	}
	if len(tokens) != 6 {
		t.Fatalf("token count = %d, want 6", len(tokens))
	}
	if tokens[2] != "X-Title: hello world" {
		t.Fatalf("header token = %q", tokens[2])
	}
	if tokens[4] != "name=john doe" {
		t.Fatalf("body token = %q", tokens[4])
	}
}

var _ domain.RequestSpec
