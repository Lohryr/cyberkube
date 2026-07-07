// Package teams implements team creation and membership. Scoring and
// challenge instances are team-scoped, so every player joins (or creates) a
// team before playing.
package teams

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/CyberKube-ISEN/cyberkube/internal/auth"
	"github.com/CyberKube-ISEN/cyberkube/internal/store"
)

// TeamStore is the subset of store.Store the team handlers need.
type TeamStore interface {
	CreateTeam(ctx context.Context, name, inviteCode, creatorID string) (*store.Team, error)
	GetTeamByInviteCode(ctx context.Context, code string) (*store.Team, error)
	GetTeamByID(ctx context.Context, id string) (*store.Team, error)
	GetUserByID(ctx context.Context, id string) (*store.User, error)
	JoinTeam(ctx context.Context, userID, teamID string) error
}

// Handler serves team endpoints. All routes require auth.Middleware.
type Handler struct {
	Store        TeamStore
	Tokens       *auth.TokenIssuer
	CookieDomain string
	SecureCookie bool
}

type createRequest struct {
	Name string `json:"name"`
}

// Create handles POST /api/teams.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())

	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "team name is required")
		return
	}
	if user, err := h.Store.GetUserByID(r.Context(), claims.Subject); err != nil || user.TeamID != "" {
		writeError(w, http.StatusConflict, "already in a team")
		return
	}

	team, err := h.Store.CreateTeam(r.Context(), req.Name, newInviteCode(), claims.Subject)
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, "team name already taken")
		return
	}
	if err != nil {
		slog.Error("create team failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	h.refreshToken(w, claims, team.ID)
	writeJSON(w, http.StatusCreated, map[string]string{
		"id":         team.ID,
		"name":       team.Name,
		"inviteCode": team.InviteCode,
	})
}

type joinRequest struct {
	InviteCode string `json:"inviteCode"`
}

// Join handles POST /api/teams/join.
func (h *Handler) Join(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())

	var req joinRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if user, err := h.Store.GetUserByID(r.Context(), claims.Subject); err != nil || user.TeamID != "" {
		writeError(w, http.StatusConflict, "already in a team")
		return
	}

	team, err := h.Store.GetTeamByInviteCode(r.Context(), strings.TrimSpace(req.InviteCode))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "invalid invite code")
		return
	}
	if err != nil {
		slog.Error("lookup invite code failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := h.Store.JoinTeam(r.Context(), claims.Subject, team.ID); err != nil {
		slog.Error("join team failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	h.refreshToken(w, claims, team.ID)
	writeJSON(w, http.StatusOK, map[string]string{
		"id":   team.ID,
		"name": team.Name,
	})
}

// Mine handles GET /api/teams/mine.
func (h *Handler) Mine(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	user, err := h.Store.GetUserByID(r.Context(), claims.Subject)
	if err != nil || user.TeamID == "" {
		writeError(w, http.StatusNotFound, "no team")
		return
	}
	team, err := h.Store.GetTeamByID(r.Context(), user.TeamID)
	if err != nil {
		writeError(w, http.StatusNotFound, "no team")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"id":         team.ID,
		"name":       team.Name,
		"inviteCode": team.InviteCode,
	})
}

// refreshToken re-issues the session token so the team claim reflects the
// new membership immediately.
func (h *Handler) refreshToken(w http.ResponseWriter, claims *auth.Claims, teamID string) {
	token, err := h.Tokens.Issue(claims.Subject, claims.Username, teamID)
	if err != nil {
		slog.Error("token refresh failed", "err", err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     auth.CookieName,
		Value:    token,
		Domain:   h.CookieDomain,
		Path:     "/",
		Expires:  time.Now().Add(24 * time.Hour),
		HttpOnly: true,
		Secure:   h.SecureCookie,
		SameSite: http.SameSiteLaxMode,
	})
	w.Header().Set("X-New-Token", token)
}

func newInviteCode() string {
	buf := make([]byte, 5)
	if _, err := rand.Read(buf); err != nil {
		panic(err) // crypto/rand failure is unrecoverable
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
