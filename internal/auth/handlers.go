package auth

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/CyberKube-ISEN/cyberkube/internal/metrics"
	"github.com/CyberKube-ISEN/cyberkube/internal/store"
)

const minPasswordLength = 8

// UserStore is the subset of store.Store the auth handlers need.
type UserStore interface {
	CreateUser(ctx context.Context, username, email, passwordHash string) (*store.User, error)
	GetUserByLogin(ctx context.Context, login string) (*store.User, error)
	GetUserByID(ctx context.Context, id string) (*store.User, error)
}

// Handler serves registration, login, and verification endpoints.
type Handler struct {
	Store        UserStore
	Tokens       *TokenIssuer
	CookieDomain string // e.g. ".ctf.rokhnir.dev" so instance hosts receive the cookie
	SecureCookie bool
}

type registerRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

// Register handles POST /api/register.
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	req.Email = strings.TrimSpace(req.Email)

	if req.Username == "" || req.Email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "username, email, and password are required")
		return
	}
	if _, err := mail.ParseAddress(req.Email); err != nil {
		writeError(w, http.StatusBadRequest, "invalid email address")
		return
	}
	if len(req.Password) < minPasswordLength {
		writeError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		slog.Error("bcrypt hash failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	user, err := h.Store.CreateUser(r.Context(), req.Username, req.Email, string(hash))
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, "username or email already taken")
		return
	}
	if err != nil {
		slog.Error("create user failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	metrics.RegistrationsTotal.Inc()
	writeJSON(w, http.StatusCreated, map[string]string{
		"id":       user.ID,
		"username": user.Username,
	})
}

type loginRequest struct {
	Login    string `json:"login"` // username or email
	Password string `json:"password"`
}

// Login handles POST /api/login.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	user, err := h.Store.GetUserByLogin(r.Context(), strings.TrimSpace(req.Login))
	if err != nil {
		// Hash anyway so response timing does not reveal whether the
		// account exists.
		_ = bcrypt.CompareHashAndPassword(
			[]byte("$2a$10$7EqJtq98hPqEX7fNZaFWoOhi5B0Zx0FSqzrnB/Cvyg4wRy9nQ4Ap6"), []byte(req.Password))
		metrics.LoginsTotal.WithLabelValues("failure").Inc()
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)) != nil {
		metrics.LoginsTotal.WithLabelValues("failure").Inc()
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	token, err := h.Tokens.Issue(user.ID, user.Username, user.TeamID)
	if err != nil {
		slog.Error("token issue failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Domain:   h.CookieDomain,
		Path:     "/",
		Expires:  time.Now().Add(24 * time.Hour),
		HttpOnly: true,
		Secure:   h.SecureCookie,
		SameSite: http.SameSiteLaxMode,
	})
	metrics.LoginsTotal.WithLabelValues("success").Inc()
	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}

// Verify handles GET /auth/verify — used both by the frontend and as the
// NGINX Ingress auth-url subrequest target for challenge instances. It
// returns 200 with identity headers when the token is valid, 401 otherwise.
func (h *Handler) Verify(w http.ResponseWriter, r *http.Request) {
	claims, err := h.claimsFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	w.Header().Set("X-User-Id", claims.Subject)
	w.Header().Set("X-Username", claims.Username)
	w.Header().Set("X-Team-Id", claims.TeamID)
	w.WriteHeader(http.StatusOK)
}

// Me handles GET /api/me.
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	user, err := h.Store.GetUserByID(r.Context(), claims.Subject)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"id":       user.ID,
		"username": user.Username,
		"email":    user.Email,
		"teamId":   user.TeamID,
	})
}

type contextKey struct{}

// ClaimsFromContext returns the verified claims stored by Middleware.
func ClaimsFromContext(ctx context.Context) *Claims {
	claims, _ := ctx.Value(contextKey{}).(*Claims)
	return claims
}

// ContextWithClaims returns a child context carrying the given claims. Used
// by Middleware and by tests that exercise authenticated handlers directly.
func ContextWithClaims(ctx context.Context, claims *Claims) context.Context {
	return context.WithValue(ctx, contextKey{}, claims)
}

// Middleware rejects requests without a valid token and stores the claims in
// the request context.
func (h *Handler) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, err := h.claimsFromRequest(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r.WithContext(ContextWithClaims(r.Context(), claims)))
	})
}

// OptionalClaims best-effort extracts and verifies claims from the request
// without enforcing authentication: it returns nil (never an error) when no
// token is present or the token is invalid. Used by the request logging
// middleware to attach user_id/team_id to log lines for authenticated
// requests, including ones the auth middleware has not yet run on in the
// same middleware chain.
func (h *Handler) OptionalClaims(r *http.Request) *Claims {
	claims, err := h.claimsFromRequest(r)
	if err != nil {
		return nil
	}
	return claims
}

func (h *Handler) claimsFromRequest(r *http.Request) (*Claims, error) {
	token := ""
	if header := r.Header.Get("Authorization"); strings.HasPrefix(header, "Bearer ") {
		token = strings.TrimPrefix(header, "Bearer ")
	} else if cookie, err := r.Cookie(CookieName); err == nil {
		token = cookie.Value
	}
	if token == "" {
		return nil, errors.New("no token")
	}
	return h.Tokens.Verify(token)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
