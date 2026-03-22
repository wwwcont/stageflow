package sandbox

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
)

type Server struct {
	store  *Store
	logger *zap.Logger
	mux    *http.ServeMux
}

func NewServer(store *Store, logger *zap.Logger) *Server {
	if store == nil {
		store = NewStore()
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	s := &Server{store: store, logger: logger, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return s.loggingMiddleware(s.mux)
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/health", s.health)
	s.mux.HandleFunc("POST /api/reset", s.reset)
	s.mux.HandleFunc("POST /api/seed", s.seed)
	s.mux.HandleFunc("GET /api/debug/state", s.debugState)

	s.mux.HandleFunc("POST /api/users", s.createUser)
	s.mux.HandleFunc("GET /api/users/{id}", s.getUser)
	s.mux.HandleFunc("POST /api/accounts", s.createAccount)
	s.mux.HandleFunc("GET /api/accounts/{id}", s.getAccount)
	s.mux.HandleFunc("POST /api/orders", s.createOrder)
	s.mux.HandleFunc("GET /api/orders/{id}", s.getOrder)
	s.mux.HandleFunc("POST /api/orders/{id}/complete", s.completeOrder)

	s.mux.HandleFunc("GET /api/unstable", s.unstable)
	s.mux.HandleFunc("GET /api/always-500", s.always500)
	s.mux.HandleFunc("GET /api/delay/{ms}", s.delay)
	s.mux.HandleFunc("GET /api/headers", s.headers)
	s.mux.HandleFunc("/api/echo", s.echo)
	s.mux.HandleFunc("GET /api/assert/ok", s.assertOK)
	s.mux.HandleFunc("GET /api/assert/wrong-value", s.assertWrongValue)
	s.mux.HandleFunc("GET /api/assert/missing-field", s.assertMissingField)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "service": "stageflow-sandbox"})
}

func (s *Server) reset(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "reset", "state": s.store.Reset()})
}

func (s *Server) seed(w http.ResponseWriter, _ *http.Request) {
	state := s.store.Seed()
	writeJSON(w, http.StatusCreated, map[string]any{"status": "seeded", "state": state})
}

func (s *Server) debugState(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.store.Snapshot())
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	var req CreateUserRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Email) == "" {
		writeAPIError(w, http.StatusBadRequest, "validation_error", "name and email are required")
		return
	}
	user, _ := s.store.CreateUser(req)
	writeJSON(w, http.StatusCreated, user)
}

func (s *Server) getUser(w http.ResponseWriter, r *http.Request) {
	user, ok := s.store.GetUser(r.PathValue("id"))
	if !ok {
		writeAPIError(w, http.StatusNotFound, "not_found", "user not found")
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (s *Server) createAccount(w http.ResponseWriter, r *http.Request) {
	var req CreateAccountRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if strings.TrimSpace(req.UserID) == "" {
		writeAPIError(w, http.StatusBadRequest, "validation_error", "user_id is required")
		return
	}
	account, err := s.store.CreateAccount(req)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, account)
}

func (s *Server) getAccount(w http.ResponseWriter, r *http.Request) {
	account, ok := s.store.GetAccount(r.PathValue("id"))
	if !ok {
		writeAPIError(w, http.StatusNotFound, "not_found", "account not found")
		return
	}
	writeJSON(w, http.StatusOK, account)
}

func (s *Server) createOrder(w http.ResponseWriter, r *http.Request) {
	var req CreateOrderRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if strings.TrimSpace(req.UserID) == "" || strings.TrimSpace(req.AccountID) == "" {
		writeAPIError(w, http.StatusBadRequest, "validation_error", "user_id and account_id are required")
		return
	}
	order, err := s.store.CreateOrder(req)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, order)
}

func (s *Server) getOrder(w http.ResponseWriter, r *http.Request) {
	order, ok := s.store.GetOrder(r.PathValue("id"))
	if !ok {
		writeAPIError(w, http.StatusNotFound, "not_found", "order not found")
		return
	}
	writeJSON(w, http.StatusOK, order)
}

func (s *Server) completeOrder(w http.ResponseWriter, r *http.Request) {
	order, err := s.store.CompleteOrder(r.PathValue("id"))
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, order)
}

func (s *Server) unstable(w http.ResponseWriter, _ *http.Request) {
	attempt := s.store.RecordUnstableCall()
	if attempt <= 2 {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{
			"status":  "temporary_failure",
			"attempt": attempt,
			"retry":   true,
			"message": "unstable endpoint failed on purpose",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"attempt": attempt,
		"retry":   false,
		"message": "unstable endpoint has recovered",
	})
}

func (s *Server) always500(w http.ResponseWriter, _ *http.Request) {
	writeJSONStatus(w, http.StatusInternalServerError, map[string]any{
		"status":  "error",
		"message": "intentional sandbox failure",
		"retry":   false,
	})
}

func (s *Server) delay(w http.ResponseWriter, r *http.Request) {
	ms, err := strconv.Atoi(r.PathValue("ms"))
	if err != nil || ms < 0 {
		writeAPIError(w, http.StatusBadRequest, "validation_error", "delay must be a non-negative integer")
		return
	}
	select {
	case <-time.After(time.Duration(ms) * time.Millisecond):
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "delay_ms": ms, "completed_at": time.Now().UTC()})
	case <-r.Context().Done():
		writeAPIError(w, http.StatusRequestTimeout, "request_cancelled", r.Context().Err().Error())
	}
}

func (s *Server) headers(w http.ResponseWriter, r *http.Request) {
	traceID := fmt.Sprintf("trace-%d", time.Now().UTC().UnixNano())
	w.Header().Set("X-Trace-ID", traceID)
	w.Header().Set("X-Sandbox-Version", "v1")
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"trace": map[string]any{
			"id":     traceID,
			"header": "X-Trace-ID",
		},
		"request": map[string]any{
			"authorization_present": strings.TrimSpace(r.Header.Get("Authorization")) != "",
			"user_agent":            r.Header.Get("User-Agent"),
		},
	})
}

func (s *Server) echo(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "read_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"method":      r.Method,
		"path":        r.URL.Path,
		"query":       r.URL.Query(),
		"headers":     r.Header,
		"body":        string(body),
		"body_length": len(body),
	})
}

func (s *Server) assertOK(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"result": map[string]any{
			"value":  "expected",
			"count":  1,
			"nested": map[string]any{"flag": true, "status": "ready"},
		},
	})
}

func (s *Server) assertWrongValue(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"result": map[string]any{
			"value":  "unexpected",
			"count":  2,
			"nested": map[string]any{"flag": false, "status": "wrong"},
		},
	})
}

func (s *Server) assertMissingField(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"result": map[string]any{
			"count": 3,
		},
	})
}

func decodeJSON(r *http.Request, target any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return nil
}

func writeAPIError(w http.ResponseWriter, status int, code, details string) {
	writeJSONStatus(w, status, APIError{Error: http.StatusText(status), Code: code, Details: details})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	writeJSONStatus(w, status, payload)
}

func writeJSONStatus(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		s.logger.Info("sandbox request completed",
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.Int("status", recorder.status),
			zap.Duration("duration", time.Since(start)),
		)
	})
}
