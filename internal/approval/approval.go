// Package approval holds expensive queries until the user who asked confirms them.
package approval

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

// TTL is how long a pending query can be approved.
const TTL = 10 * time.Minute

var (
	ErrNotFound  = errors.New("approval not found or already used")
	ErrWrongUser = errors.New("approval belongs to another user")
	ErrExpired   = errors.New("approval expired")
)

type Pending struct {
	UserID  int64
	SQL     string
	Created time.Time
}

type Store struct {
	mu      sync.Mutex
	pending map[string]Pending
}

func NewStore() *Store { return &Store{pending: map[string]Pending{}} }

// Put stores a query and returns a random single-use token for it.
func (s *Store) Put(userID int64, sql string, now time.Time) string {
	b := make([]byte, 32)
	rand.Read(b)
	tok := hex.EncodeToString(b)
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, p := range s.pending { // drop expired ones as we go
		if now.Sub(p.Created) > TTL {
			delete(s.pending, k)
		}
	}
	s.pending[tok] = Pending{UserID: userID, SQL: sql, Created: now}
	return tok
}

// Take consumes the token. A wrong user does not consume it.
func (s *Store) Take(userID int64, token string, now time.Time) (Pending, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pending[token]
	if !ok {
		return Pending{}, ErrNotFound
	}
	if p.UserID != userID {
		return Pending{}, ErrWrongUser
	}
	delete(s.pending, token)
	if now.Sub(p.Created) > TTL {
		return Pending{}, ErrExpired
	}
	return p, nil
}
