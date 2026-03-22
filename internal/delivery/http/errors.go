package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"stageflow/internal/domain"
)

type errorResponse struct {
	Error     apiError `json:"error"`
	RequestID string   `json:"request_id,omitempty"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

func writeError(w http.ResponseWriter, r *http.Request, err error) {
	status, body := classifyError(err)
	body.RequestID = requestIDFromContext(r.Context())
	writeJSON(w, status, body)
}

func writeValidationError(w http.ResponseWriter, r *http.Request, message string, details any) {
	writeJSON(w, http.StatusBadRequest, errorResponse{
		Error:     apiError{Code: "validation_error", Message: message, Details: details},
		RequestID: requestIDFromContext(r.Context()),
	})
}

func classifyError(err error) (int, errorResponse) {
	var (
		validationErr *domain.ValidationError
		notFoundErr   *domain.NotFoundError
		conflictErr   *domain.ConflictError
	)
	switch {
	case err == nil:
		return http.StatusInternalServerError, errorResponse{Error: apiError{Code: "internal_error", Message: "unexpected empty error"}}
	case errors.As(err, &validationErr):
		return http.StatusBadRequest, errorResponse{Error: apiError{Code: "validation_error", Message: validationErr.Error()}}
	case errors.As(err, &notFoundErr):
		return http.StatusNotFound, errorResponse{Error: apiError{Code: "not_found", Message: notFoundErr.Error()}}
	case errors.As(err, &conflictErr):
		return http.StatusConflict, errorResponse{Error: apiError{Code: "conflict", Message: conflictErr.Error()}}
	case errors.Is(err, domain.ErrAssertionFailure):
		return http.StatusUnprocessableEntity, errorResponse{Error: apiError{Code: "assertion_failure", Message: err.Error()}}
	case errors.Is(err, domain.ErrTransport):
		return http.StatusBadGateway, errorResponse{Error: apiError{Code: "transport_error", Message: err.Error()}}
	default:
		return http.StatusInternalServerError, errorResponse{Error: apiError{Code: "internal_error", Message: err.Error()}}
	}
}

func decodeJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}
