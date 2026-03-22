package execution

import (
	"errors"
	"testing"
	"time"

	"stageflow/internal/domain"
)

func TestRetryAttempts(t *testing.T) {
	tests := []struct {
		name   string
		policy domain.RetryPolicy
		want   int
	}{
		{name: "disabled defaults to one", policy: domain.RetryPolicy{}, want: 1},
		{name: "enabled without max attempts still one", policy: domain.RetryPolicy{Enabled: true}, want: 1},
		{name: "enabled uses configured attempts", policy: domain.RetryPolicy{Enabled: true, MaxAttempts: 4}, want: 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := retryAttempts(tt.policy); got != tt.want {
				t.Fatalf("retryAttempts() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestRetryDelay(t *testing.T) {
	tests := []struct {
		name    string
		policy  domain.RetryPolicy
		attempt int
		want    time.Duration
	}{
		{name: "disabled policy", policy: domain.RetryPolicy{}, attempt: 1, want: 0},
		{name: "fixed delay", policy: domain.RetryPolicy{Enabled: true, InitialInterval: 150 * time.Millisecond}, attempt: 2, want: 150 * time.Millisecond},
		{name: "exponential delay", policy: domain.RetryPolicy{Enabled: true, BackoffStrategy: domain.BackoffStrategyExponential, InitialInterval: 100 * time.Millisecond}, attempt: 3, want: 400 * time.Millisecond},
		{name: "respects max interval", policy: domain.RetryPolicy{Enabled: true, BackoffStrategy: domain.BackoffStrategyExponential, InitialInterval: 200 * time.Millisecond, MaxInterval: 500 * time.Millisecond}, attempt: 4, want: 500 * time.Millisecond},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := retryDelay(tt.policy, tt.attempt); got != tt.want {
				t.Fatalf("retryDelay() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestShouldRetry(t *testing.T) {
	transportErr := &domain.TransportError{Operation: "request", Cause: errors.New("temporary")}
	assertionErr := errors.Join(domain.ErrAssertionFailure, errors.New("assertion failed"))

	tests := []struct {
		name          string
		policy        domain.RetryPolicy
		attempt       StepAttemptRecord
		err           error
		current       int
		total         int
		wantRetryable bool
	}{
		{name: "stops at max attempts", policy: domain.RetryPolicy{Enabled: true, MaxAttempts: 2}, err: transportErr, current: 2, total: 2, wantRetryable: false},
		{name: "transport retries when enabled", policy: domain.RetryPolicy{Enabled: true, MaxAttempts: 3}, err: transportErr, current: 1, total: 3, wantRetryable: true},
		{name: "assertion never retries", policy: domain.RetryPolicy{Enabled: true, MaxAttempts: 3}, err: assertionErr, current: 1, total: 3, wantRetryable: false},
		{name: "retryable status retries", policy: domain.RetryPolicy{Enabled: true, RetryableStatusCodes: []int{429}}, attempt: StepAttemptRecord{ResponseSnapshot: map[string]any{"status_code": 429}}, err: transportErr, current: 1, total: 2, wantRetryable: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldRetry(tt.policy, tt.attempt, tt.err, tt.current, tt.total); got != tt.wantRetryable {
				t.Fatalf("shouldRetry() = %v, want %v", got, tt.wantRetryable)
			}
		})
	}
}
