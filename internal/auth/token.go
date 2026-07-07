// Package auth implements native user authentication: bcrypt-hashed
// credentials in PostgreSQL and HS256 JWT sessions. No external identity
// provider is involved.
package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// CookieName is the session cookie checked by Verify alongside the
// Authorization header. It is set domain-wide so NGINX auth-url subrequests
// from challenge instance hosts carry it.
const CookieName = "cyberkube_token"

// Claims are the JWT claims carried by a session token.
type Claims struct {
	Username string `json:"username"`
	TeamID   string `json:"team,omitempty"`
	jwt.RegisteredClaims
}

// TokenIssuer signs and verifies session tokens.
type TokenIssuer struct {
	secret []byte
	ttl    time.Duration
}

// NewTokenIssuer creates an issuer with the given HS256 secret and token TTL.
func NewTokenIssuer(secret []byte, ttl time.Duration) (*TokenIssuer, error) {
	if len(secret) < 32 {
		return nil, errors.New("JWT secret must be at least 32 bytes")
	}
	return &TokenIssuer{secret: secret, ttl: ttl}, nil
}

// Issue signs a token for the given user.
func (t *TokenIssuer) Issue(userID, username, teamID string) (string, error) {
	now := time.Now()
	claims := Claims{
		Username: username,
		TeamID:   teamID,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(t.ttl)),
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(t.secret)
	if err != nil {
		return "", fmt.Errorf("sign token: %w", err)
	}
	return signed, nil
}

// Verify parses and validates a token, returning its claims.
func (t *TokenIssuer) Verify(token string) (*Claims, error) {
	claims := &Claims{}
	parsed, err := jwt.ParseWithClaims(token, claims, func(tok *jwt.Token) (any, error) {
		if _, ok := tok.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method %v", tok.Header["alg"])
		}
		return t.secret, nil
	})
	if err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}
	if !parsed.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}
