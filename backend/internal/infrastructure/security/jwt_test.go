package security

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type memoryTokenSessionStore struct {
	mu       sync.Mutex
	sessions map[string]bool
}

func (s *memoryTokenSessionStore) Register(_ context.Context, claims *Claims, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[claims.ID] = true
	return nil
}

func (s *memoryTokenSessionStore) Validate(_ context.Context, claims *Claims) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.sessions[claims.ID] {
		return errors.New("revoked")
	}
	return nil
}

func (s *memoryTokenSessionStore) Active(_ context.Context, claims *Claims) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessions[claims.ID], nil
}

func (s *memoryTokenSessionStore) Revoke(_ context.Context, tokenID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, tokenID)
	return nil
}

func TestTokenServiceGenerateDefaultsToAdminSubject(t *testing.T) {
	svc := NewTokenService("secret", time.Hour)

	token, err := svc.Generate(10)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	claims, err := svc.Parse(token)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if claims.Subject != SubjectAdmin {
		t.Fatalf("subject = %q, want %q", claims.Subject, SubjectAdmin)
	}
}

func TestTokenServiceGenerateForClientSubject(t *testing.T) {
	svc := NewTokenService("secret", time.Hour)

	token, err := svc.GenerateForSubject(20, SubjectClient)
	if err != nil {
		t.Fatalf("GenerateForSubject: %v", err)
	}
	claims, err := svc.Parse(token)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if claims.Subject != SubjectClient {
		t.Fatalf("subject = %q, want %q", claims.Subject, SubjectClient)
	}
}

func TestTokenServiceRejectsUnknownSubject(t *testing.T) {
	svc := NewTokenService("secret", time.Hour)

	if _, err := svc.GenerateForSubject(20, "partner"); err == nil {
		t.Fatal("GenerateForSubject accepted an unknown subject")
	}
}

func TestTokenServiceServerSideSessionCanBeRevoked(t *testing.T) {
	svc := NewTokenService("secret", time.Hour)
	store := &memoryTokenSessionStore{sessions: make(map[string]bool)}
	svc.SetSessionStore(store)

	token, err := svc.Generate(10)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if err := svc.RegisterSession(context.Background(), token, time.Minute); err != nil {
		t.Fatalf("RegisterSession: %v", err)
	}
	if _, err := svc.Parse(token); err != nil {
		t.Fatalf("Parse before revoke: %v", err)
	}
	if err := svc.RevokeSession(context.Background(), token); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}
	if _, err := svc.Parse(token); err == nil {
		t.Fatal("Parse accepted a revoked session")
	}
}
