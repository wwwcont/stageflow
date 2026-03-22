package execution

import (
	"context"
	"errors"
	"strings"
	"testing"

	"stageflow/internal/domain"
)

func TestDefaultAsserter_Assert(t *testing.T) {
	asserter := NewDefaultAsserter()
	response := HTTPResponse{
		StatusCode: 200,
		Headers:    map[string][]string{"X-Request-ID": {"req-123"}},
		Body:       []byte(`{"id":"user-1","message":"hello world"}`),
	}

	tests := []struct {
		name    string
		spec    domain.AssertionSpec
		wantErr string
		wantIs  error
	}{
		{
			name: "all assertions pass",
			spec: domain.AssertionSpec{Rules: []domain.AssertionRule{
				{Name: "status", Target: domain.AssertionTargetStatusCode, Operator: domain.AssertionOperatorEquals, ExpectedValue: "200"},
				{Name: "id exists", Target: domain.AssertionTargetBodyJSON, Operator: domain.AssertionOperatorExists, Selector: "$.id"},
				{Name: "message contains", Target: domain.AssertionTargetBodyText, Operator: domain.AssertionOperatorContains, ExpectedValue: "hello"},
				{Name: "request id not empty", Target: domain.AssertionTargetHeader, Operator: domain.AssertionOperatorNotEmpty, Selector: "X-Request-ID"},
			}},
		},
		{
			name:    "assertion fails with dedicated error",
			spec:    domain.AssertionSpec{Rules: []domain.AssertionRule{{Name: "wrong status", Target: domain.AssertionTargetStatusCode, Operator: domain.AssertionOperatorEquals, ExpectedValue: "201"}}},
			wantErr: "wrong status",
			wantIs:  domain.ErrAssertionFailure,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := asserter.Assert(context.Background(), tt.spec, response)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Assert() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Assert() error = %v, want contains %q", err, tt.wantErr)
			}
			if tt.wantIs != nil && !errors.Is(err, tt.wantIs) {
				t.Fatalf("Assert() error = %v, want errors.Is(..., %v)", err, tt.wantIs)
			}
		})
	}
}
