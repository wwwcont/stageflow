package sandbox

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type Store struct {
	mu            sync.RWMutex
	users         map[string]User
	accounts      map[string]Account
	orders        map[string]Order
	nextUserID    int
	nextAccountID int
	nextOrderID   int
	unstableCalls int
	seeded        bool
	lastResetAt   time.Time
}

func NewStore() *Store {
	now := time.Now().UTC()
	return &Store{
		users:       make(map[string]User),
		accounts:    make(map[string]Account),
		orders:      make(map[string]Order),
		lastResetAt: now,
	}
}

func (s *Store) Reset() DebugState {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.users = make(map[string]User)
	s.accounts = make(map[string]Account)
	s.orders = make(map[string]Order)
	s.nextUserID = 0
	s.nextAccountID = 0
	s.nextOrderID = 0
	s.unstableCalls = 0
	s.seeded = false
	s.lastResetAt = time.Now().UTC()
	return s.snapshotLocked()
}

func (s *Store) Seed() DebugState {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.users = make(map[string]User)
	s.accounts = make(map[string]Account)
	s.orders = make(map[string]Order)
	s.nextUserID = 0
	s.nextAccountID = 0
	s.nextOrderID = 0
	s.unstableCalls = 0
	s.lastResetAt = time.Now().UTC()
	user := s.createUserLocked(CreateUserRequest{Name: "Demo User", Email: "demo@example.test", Labels: map[string]string{"tier": "gold", "region": "eu"}})
	account, _ := s.createAccountLocked(CreateAccountRequest{UserID: user.ID, Type: "checking", Currency: "USD", AvailableCents: 125000})
	order, _ := s.createOrderLocked(CreateOrderRequest{UserID: user.ID, AccountID: account.ID, Currency: "USD", AmountCents: 2599})
	order.Status = "completed"
	order.Links.Order = fmt.Sprintf("/api/orders/%s", order.ID)
	s.orders[order.ID] = order
	s.seeded = true
	return s.snapshotLocked()
}

func (s *Store) CreateUser(input CreateUserRequest) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.createUserLocked(input), nil
}

func (s *Store) createUserLocked(input CreateUserRequest) User {
	s.nextUserID++
	id := fmt.Sprintf("user-%04d", s.nextUserID)
	email := strings.TrimSpace(strings.ToLower(input.Email))
	domain := ""
	if idx := strings.LastIndex(email, "@"); idx >= 0 && idx < len(email)-1 {
		domain = email[idx+1:]
	}
	user := User{
		ID:        id,
		Status:    "active",
		CreatedAt: time.Now().UTC(),
		Profile: UserProfile{
			Name:        strings.TrimSpace(input.Name),
			Email:       email,
			EmailDomain: domain,
			Labels:      cloneMap(input.Labels),
		},
		Links: LinkSet{Self: fmt.Sprintf("/api/users/%s", id)},
	}
	s.users[id] = user
	return user
}

func (s *Store) GetUser(id string) (User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.users[id]
	return item, ok
}

func (s *Store) CreateAccount(input CreateAccountRequest) (Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.createAccountLocked(input)
}

func (s *Store) createAccountLocked(input CreateAccountRequest) (Account, error) {
	user, ok := s.users[input.UserID]
	if !ok {
		return Account{}, fmt.Errorf("user %q not found", input.UserID)
	}
	s.nextAccountID++
	id := fmt.Sprintf("acct-%04d", s.nextAccountID)
	currency := strings.ToUpper(strings.TrimSpace(input.Currency))
	if currency == "" {
		currency = "USD"
	}
	accountType := strings.TrimSpace(input.Type)
	if accountType == "" {
		accountType = "checking"
	}
	account := Account{
		ID:        id,
		UserID:    user.ID,
		Status:    "active",
		CreatedAt: time.Now().UTC(),
		Type:      accountType,
		Balance: AccountBalance{
			Currency:       currency,
			AvailableCents: input.AvailableCents,
		},
		Owner: AccountOwner{
			UserID:     user.ID,
			UserStatus: user.Status,
		},
		Links: LinkSet{Self: fmt.Sprintf("/api/accounts/%s", id), User: fmt.Sprintf("/api/users/%s", user.ID)},
	}
	s.accounts[id] = account
	return account, nil
}

func (s *Store) GetAccount(id string) (Account, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.accounts[id]
	return item, ok
}

func (s *Store) CreateOrder(input CreateOrderRequest) (Order, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.createOrderLocked(input)
}

func (s *Store) createOrderLocked(input CreateOrderRequest) (Order, error) {
	user, ok := s.users[input.UserID]
	if !ok {
		return Order{}, fmt.Errorf("user %q not found", input.UserID)
	}
	account, ok := s.accounts[input.AccountID]
	if !ok {
		return Order{}, fmt.Errorf("account %q not found", input.AccountID)
	}
	if account.UserID != user.ID {
		return Order{}, fmt.Errorf("account %q does not belong to user %q", account.ID, user.ID)
	}
	s.nextOrderID++
	id := fmt.Sprintf("order-%04d", s.nextOrderID)
	currency := strings.ToUpper(strings.TrimSpace(input.Currency))
	if currency == "" {
		currency = "USD"
	}
	order := Order{
		ID:        id,
		UserID:    user.ID,
		AccountID: account.ID,
		Status:    "pending",
		CreatedAt: time.Now().UTC(),
		Amount: OrderAmount{
			Currency:   currency,
			ValueCents: input.AmountCents,
		},
		Summary: OrderLinks{UserID: user.ID, AccountID: account.ID},
		Links: LinkSet{
			Self:    fmt.Sprintf("/api/orders/%s", id),
			User:    fmt.Sprintf("/api/users/%s", user.ID),
			Account: fmt.Sprintf("/api/accounts/%s", account.ID),
			Order:   fmt.Sprintf("/api/orders/%s", id),
		},
	}
	s.orders[id] = order
	return order, nil
}

func (s *Store) GetOrder(id string) (Order, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.orders[id]
	return item, ok
}

func (s *Store) CompleteOrder(id string) (Order, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	order, ok := s.orders[id]
	if !ok {
		return Order{}, fmt.Errorf("order %q not found", id)
	}
	order.Status = "completed"
	s.orders[id] = order
	return order, nil
}

func (s *Store) RecordUnstableCall() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.unstableCalls++
	return s.unstableCalls
}

func (s *Store) Snapshot() DebugState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshotLocked()
}

func (s *Store) snapshotLocked() DebugState {
	users := make([]User, 0, len(s.users))
	for _, item := range s.users {
		users = append(users, item)
	}
	accounts := make([]Account, 0, len(s.accounts))
	for _, item := range s.accounts {
		accounts = append(accounts, item)
	}
	orders := make([]Order, 0, len(s.orders))
	for _, item := range s.orders {
		orders = append(orders, item)
	}
	sort.Slice(users, func(i, j int) bool { return users[i].ID < users[j].ID })
	sort.Slice(accounts, func(i, j int) bool { return accounts[i].ID < accounts[j].ID })
	sort.Slice(orders, func(i, j int) bool { return orders[i].ID < orders[j].ID })
	return DebugState{
		Users:    users,
		Accounts: accounts,
		Orders:   orders,
		Counters: DebugCounters{
			NextUserID:    s.nextUserID,
			NextAccountID: s.nextAccountID,
			NextOrderID:   s.nextOrderID,
			UnstableCalls: s.unstableCalls,
		},
		Seeded:      s.seeded,
		LastResetAt: s.lastResetAt,
	}
}

func cloneMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}
