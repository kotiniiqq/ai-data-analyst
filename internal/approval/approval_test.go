package approval

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func TestTakeReturnsThePendingQueryOnce(t *testing.T) {
	s := NewStore()
	tok := s.Put(2, "select 1", t0)
	if len(tok) != 64 {
		t.Fatalf("token %q", tok)
	}
	p, err := s.Take(2, tok, t0.Add(time.Minute))
	if err != nil || p.SQL != "select 1" {
		t.Fatalf("got %+v, %v", p, err)
	}
	if _, err := s.Take(2, tok, t0.Add(time.Minute)); err != ErrNotFound {
		t.Fatalf("second take: %v", err)
	}
}

func TestOtherUserCannotTakeAndTokenSurvives(t *testing.T) {
	s := NewStore()
	tok := s.Put(2, "select 1", t0)
	if _, err := s.Take(1, tok, t0); err != ErrWrongUser {
		t.Fatalf("got %v", err)
	}
	if _, err := s.Take(2, tok, t0); err != nil {
		t.Fatalf("owner of the token cannot use it after a foreign attempt: %v", err)
	}
}

func TestExpiredAfterTenMinutes(t *testing.T) {
	s := NewStore()
	tok := s.Put(2, "select 1", t0)
	if _, err := s.Take(2, tok, t0.Add(10*time.Minute+time.Second)); err != ErrExpired {
		t.Fatalf("got %v", err)
	}
}

func TestUnknownToken(t *testing.T) {
	if _, err := NewStore().Take(2, "nope", t0); err != ErrNotFound {
		t.Fatalf("got %v", err)
	}
}

func TestTokensAreUnique(t *testing.T) {
	s := NewStore()
	if s.Put(2, "a", t0) == s.Put(2, "a", t0) {
		t.Fatal("same token twice")
	}
}
