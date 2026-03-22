package execution

import (
	"context"
	"strings"
	"testing"

	"stageflow/internal/domain"
)

func TestDefaultExtractor_Extract(t *testing.T) {
	extractor := NewDefaultExtractor()
	response := HTTPResponse{
		StatusCode: 201,
		Headers:    map[string][]string{"X-Request-ID": {"req-123"}},
		Body:       []byte(`{"id":"user-1","data":{"account":{"number":"ACC-42"}}}`),
	}

	tests := []struct {
		name    string
		spec    domain.ExtractionSpec
		want    map[string]any
		wantErr string
	}{
		{
			name: "extract body header and status",
			spec: domain.ExtractionSpec{Rules: []domain.ExtractionRule{
				{Name: "id", Source: domain.ExtractionSourceBody, Selector: "$.id", Required: true},
				{Name: "account_number", Source: domain.ExtractionSourceBody, Selector: "$.data.account.number", Required: true},
				{Name: "request_id", Source: domain.ExtractionSourceHeader, Selector: "header.X-Request-ID", Required: true},
				{Name: "status", Source: domain.ExtractionSourceStatus, Selector: "status_code", Required: true},
			}},
			want: map[string]any{"id": "user-1", "account_number": "ACC-42", "request_id": "req-123", "status": 201},
		},
		{
			name:    "missing required value",
			spec:    domain.ExtractionSpec{Rules: []domain.ExtractionRule{{Name: "missing", Source: domain.ExtractionSourceBody, Selector: "$.missing", Required: true}}},
			wantErr: "required value not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractor.Extract(context.Background(), tt.spec, response)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Extract() error = %v, want contains %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Extract() error = %v", err)
			}
			for key, want := range tt.want {
				if got[key] != want {
					t.Fatalf("Extract()[%q] = %#v, want %#v", key, got[key], want)
				}
			}
		})
	}
}
