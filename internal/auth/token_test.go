package auth

import (
	"strings"
	"testing"
	"time"
)

var testSecret = []byte("0123456789abcdef0123456789abcdef")

func TestIssueVerifyRoundTrip(t *testing.T) {
	issuer, err := NewTokenIssuer(testSecret, time.Hour)
	if err != nil {
		t.Fatalf("NewTokenIssuer: %v", err)
	}
	token, err := issuer.Issue("user-1", "leo", "team-1")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	claims, err := issuer.Verify(token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.Subject != "user-1" || claims.Username != "leo" || claims.TeamID != "team-1" {
		t.Errorf("claims = %+v, want user-1/leo/team-1", claims)
	}
}

func TestVerifyExpiredToken(t *testing.T) {
	issuer, err := NewTokenIssuer(testSecret, -time.Minute)
	if err != nil {
		t.Fatalf("NewTokenIssuer: %v", err)
	}
	token, err := issuer.Issue("user-1", "leo", "")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := issuer.Verify(token); err == nil {
		t.Error("expired token verified, want error")
	}
}

func TestVerifyWrongSecret(t *testing.T) {
	issuer, _ := NewTokenIssuer(testSecret, time.Hour)
	other, _ := NewTokenIssuer([]byte(strings.Repeat("x", 32)), time.Hour)
	token, err := other.Issue("user-1", "leo", "")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := issuer.Verify(token); err == nil {
		t.Error("token with wrong secret verified, want error")
	}
}

func TestVerifyGarbage(t *testing.T) {
	issuer, _ := NewTokenIssuer(testSecret, time.Hour)
	if _, err := issuer.Verify("not.a.token"); err == nil {
		t.Error("garbage token verified, want error")
	}
}

func TestShortSecretRejected(t *testing.T) {
	if _, err := NewTokenIssuer([]byte("short"), time.Hour); err == nil {
		t.Error("short secret accepted, want error")
	}
}
