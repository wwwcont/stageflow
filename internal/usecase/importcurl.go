package usecase

import (
	"context"
	"fmt"
	"strings"

	"stageflow/internal/domain"
)

type CurlCommandParser interface {
	Parse(command string) (domain.RequestSpec, error)
}

type CurlImportService struct {
	parser CurlCommandParser
}

func NewCurlImportService(parser CurlCommandParser) (*CurlImportService, error) {
	if parser == nil {
		return nil, fmt.Errorf("curl parser is required")
	}
	return &CurlImportService{parser: parser}, nil
}

func (s *CurlImportService) ImportCurl(_ context.Context, input ImportCurlInput) (ImportCurlResult, error) {
	if strings.TrimSpace(input.Command) == "" {
		return ImportCurlResult{}, &domain.ValidationError{Message: "curl command is required"}
	}
	requestSpec, err := s.parser.Parse(input.Command)
	if err != nil {
		return ImportCurlResult{}, fmt.Errorf("import curl: %w", err)
	}
	return ImportCurlResult{RequestSpec: requestSpec}, nil
}

var _ CurlImportUseCase = (*CurlImportService)(nil)
