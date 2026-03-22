package sandbox

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestServer_StatefulUserAccountOrderFlow(t *testing.T) {
	h := NewServer(NewStore(), nil).Handler()

	userResp := postJSON(t, h, "/api/users", map[string]any{"name": "Alice", "email": "alice@example.test"})
	if userResp.Code != http.StatusCreated {
		t.Fatalf("create user status = %d", userResp.Code)
	}
	var user User
	decodeBody(t, userResp, &user)
	if user.ID == "" || user.Profile.EmailDomain != "example.test" {
		t.Fatalf("unexpected user payload: %+v", user)
	}

	accountResp := postJSON(t, h, "/api/accounts", map[string]any{"user_id": user.ID, "type": "checking", "currency": "usd", "available_cents": 5000})
	if accountResp.Code != http.StatusCreated {
		t.Fatalf("create account status = %d", accountResp.Code)
	}
	var account Account
	decodeBody(t, accountResp, &account)
	if account.UserID != user.ID {
		t.Fatalf("account user_id = %q, want %q", account.UserID, user.ID)
	}

	orderResp := postJSON(t, h, "/api/orders", map[string]any{"user_id": user.ID, "account_id": account.ID, "currency": "usd", "amount_cents": 1999})
	if orderResp.Code != http.StatusCreated {
		t.Fatalf("create order status = %d", orderResp.Code)
	}
	var order Order
	decodeBody(t, orderResp, &order)
	if order.Status != "pending" || order.Summary.AccountID != account.ID {
		t.Fatalf("unexpected order payload: %+v", order)
	}

	completeResp := postJSON(t, h, "/api/orders/"+order.ID+"/complete", map[string]any{})
	if completeResp.Code != http.StatusOK {
		t.Fatalf("complete order status = %d", completeResp.Code)
	}
	decodeBody(t, completeResp, &order)
	if order.Status != "completed" {
		t.Fatalf("order status = %q, want completed", order.Status)
	}
}

func TestServer_UnstableEndpointRecoversAfterTwoFailures(t *testing.T) {
	h := NewServer(NewStore(), nil).Handler()
	for attempt := 1; attempt <= 3; attempt++ {
		req := httptest.NewRequest(http.MethodGet, "/api/unstable", nil)
		resp := httptest.NewRecorder()
		h.ServeHTTP(resp, req)
		if attempt < 3 && resp.Code != http.StatusInternalServerError {
			t.Fatalf("attempt %d status = %d, want %d", attempt, resp.Code, http.StatusInternalServerError)
		}
		if attempt == 3 && resp.Code != http.StatusOK {
			t.Fatalf("attempt %d status = %d, want %d", attempt, resp.Code, http.StatusOK)
		}
	}
}

func TestServer_HeadersAndEchoSupportExtraction(t *testing.T) {
	h := NewServer(NewStore(), nil).Handler()
	req := httptest.NewRequest(http.MethodGet, "/api/headers", nil)
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("User-Agent", "stageflow-test")
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("headers status = %d", resp.Code)
	}
	if resp.Header().Get("X-Trace-ID") == "" {
		t.Fatalf("expected X-Trace-ID header")
	}

	echoReq := httptest.NewRequest(http.MethodPost, "/api/echo?mode=debug", bytes.NewBufferString(`{"hello":"world"}`))
	echoReq.Header.Set("Content-Type", "application/json")
	echoResp := httptest.NewRecorder()
	h.ServeHTTP(echoResp, echoReq)
	if echoResp.Code != http.StatusOK {
		t.Fatalf("echo status = %d", echoResp.Code)
	}
	var payload map[string]any
	decodeBody(t, echoResp, &payload)
	if payload["method"] != http.MethodPost {
		t.Fatalf("echo method = %v, want POST", payload["method"])
	}
}

func TestServer_ResetSeedAndDelay(t *testing.T) {
	h := NewServer(NewStore(), nil).Handler()
	seedResp := postJSON(t, h, "/api/seed", map[string]any{})
	if seedResp.Code != http.StatusCreated {
		t.Fatalf("seed status = %d", seedResp.Code)
	}
	resetResp := postJSON(t, h, "/api/reset", map[string]any{})
	if resetResp.Code != http.StatusOK {
		t.Fatalf("reset status = %d", resetResp.Code)
	}

	started := time.Now()
	req := httptest.NewRequest(http.MethodGet, "/api/delay/15", nil)
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("delay status = %d", resp.Code)
	}
	if time.Since(started) < 15*time.Millisecond {
		t.Fatalf("delay endpoint returned too quickly")
	}
}

func postJSON(t *testing.T, h http.Handler, path string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(encoded))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, req)
	return resp
}

func decodeBody(t *testing.T, resp *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.Unmarshal(resp.Body.Bytes(), target); err != nil {
		t.Fatalf("decode body: %v\nbody=%s", err, resp.Body.String())
	}
}
