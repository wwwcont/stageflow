package sandbox

import "time"

type User struct {
	ID        string      `json:"id"`
	Status    string      `json:"status"`
	CreatedAt time.Time   `json:"created_at"`
	Profile   UserProfile `json:"profile"`
	Links     LinkSet     `json:"links"`
}

type UserProfile struct {
	Name        string            `json:"name"`
	Email       string            `json:"email"`
	EmailDomain string            `json:"email_domain"`
	Labels      map[string]string `json:"labels,omitempty"`
}

type Account struct {
	ID        string         `json:"id"`
	UserID    string         `json:"user_id"`
	Status    string         `json:"status"`
	CreatedAt time.Time      `json:"created_at"`
	Type      string         `json:"type"`
	Balance   AccountBalance `json:"balance"`
	Owner     AccountOwner   `json:"owner"`
	Links     LinkSet        `json:"links"`
}

type AccountBalance struct {
	Currency       string `json:"currency"`
	AvailableCents int64  `json:"available_cents"`
}

type AccountOwner struct {
	UserID     string `json:"user_id"`
	UserStatus string `json:"user_status"`
}

type Order struct {
	ID        string      `json:"id"`
	UserID    string      `json:"user_id"`
	AccountID string      `json:"account_id"`
	Status    string      `json:"status"`
	CreatedAt time.Time   `json:"created_at"`
	Amount    OrderAmount `json:"amount"`
	Summary   OrderLinks  `json:"summary"`
	Links     LinkSet     `json:"links"`
}

type OrderAmount struct {
	Currency   string `json:"currency"`
	ValueCents int64  `json:"value_cents"`
}

type OrderLinks struct {
	UserID    string `json:"user_id"`
	AccountID string `json:"account_id"`
}

type LinkSet struct {
	Self    string `json:"self"`
	User    string `json:"user,omitempty"`
	Account string `json:"account,omitempty"`
	Order   string `json:"order,omitempty"`
}

type CreateUserRequest struct {
	Name   string            `json:"name"`
	Email  string            `json:"email"`
	Labels map[string]string `json:"labels,omitempty"`
}

type CreateAccountRequest struct {
	UserID         string `json:"user_id"`
	Type           string `json:"type"`
	Currency       string `json:"currency"`
	AvailableCents int64  `json:"available_cents"`
}

type CreateOrderRequest struct {
	UserID      string `json:"user_id"`
	AccountID   string `json:"account_id"`
	Currency    string `json:"currency"`
	AmountCents int64  `json:"amount_cents"`
}

type DebugState struct {
	Users       []User        `json:"users"`
	Accounts    []Account     `json:"accounts"`
	Orders      []Order       `json:"orders"`
	Counters    DebugCounters `json:"counters"`
	Seeded      bool          `json:"seeded"`
	LastResetAt time.Time     `json:"last_reset_at"`
}

type DebugCounters struct {
	NextUserID    int `json:"next_user_id"`
	NextAccountID int `json:"next_account_id"`
	NextOrderID   int `json:"next_order_id"`
	UnstableCalls int `json:"unstable_calls"`
}

type APIError struct {
	Error   string `json:"error"`
	Code    string `json:"code"`
	Details string `json:"details,omitempty"`
}
