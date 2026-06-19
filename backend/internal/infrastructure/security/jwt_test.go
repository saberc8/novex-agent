package security

import (
	"testing"
	"time"
)

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
