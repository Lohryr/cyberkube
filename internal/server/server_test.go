package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/CyberKube-ISEN/cyberkube/internal/auth"
	"github.com/CyberKube-ISEN/cyberkube/internal/engine"
	"github.com/CyberKube-ISEN/cyberkube/internal/k8s"
	"github.com/CyberKube-ISEN/cyberkube/internal/store"
	"github.com/CyberKube-ISEN/cyberkube/internal/teams"
)

// --- minimal fakes satisfying the handler package interfaces ---

type fakeUserStore struct{ users map[string]*store.User }

func newFakeUserStore() *fakeUserStore { return &fakeUserStore{users: map[string]*store.User{}} }

func (f *fakeUserStore) CreateUser(_ context.Context, username, email, hash string) (*store.User, error) {
	u := &store.User{ID: "user-" + username, Username: username, Email: email, PasswordHash: hash}
	f.users[u.ID] = u
	return u, nil
}

func (f *fakeUserStore) GetUserByLogin(_ context.Context, login string) (*store.User, error) {
	for _, u := range f.users {
		if u.Username == login || u.Email == login {
			return u, nil
		}
	}
	return nil, store.ErrNotFound
}

func (f *fakeUserStore) GetUserByID(_ context.Context, id string) (*store.User, error) {
	if u, ok := f.users[id]; ok {
		return u, nil
	}
	return nil, store.ErrNotFound
}

type fakeTeamStore struct{ *fakeUserStore }

func (f *fakeTeamStore) CreateTeam(context.Context, string, string, string) (*store.Team, error) {
	return &store.Team{ID: "team-1", Name: "team"}, nil
}
func (f *fakeTeamStore) GetTeamByInviteCode(context.Context, string) (*store.Team, error) {
	return nil, store.ErrNotFound
}
func (f *fakeTeamStore) GetTeamByID(context.Context, string) (*store.Team, error) {
	return nil, store.ErrNotFound
}
func (f *fakeTeamStore) JoinTeam(context.Context, string, string) error { return nil }

type fakeChallengeSource struct{}

func (fakeChallengeSource) ListChallenges(context.Context) ([]k8s.Challenge, error) { return nil, nil }
func (fakeChallengeSource) GetChallenge(context.Context, string) (*k8s.Challenge, error) {
	return nil, k8s.ErrNotFound
}
func (fakeChallengeSource) GetInstance(context.Context, string) (*k8s.Instance, error) {
	return nil, k8s.ErrNotFound
}
func (fakeChallengeSource) CreateInstance(context.Context, string, string, string, int64) (*k8s.Instance, error) {
	return nil, nil
}
func (fakeChallengeSource) DeleteInstance(context.Context, string) error     { return nil }
func (fakeChallengeSource) MarkInstanceSolved(context.Context, string) error { return nil }

type fakeScoreStore struct{}

func (fakeScoreStore) RecordSubmission(context.Context, string, string, string, string, bool) error {
	return nil
}
func (fakeScoreStore) InsertSolve(context.Context, string, string, int) (bool, error) {
	return false, nil
}
func (fakeScoreStore) CountSolves(context.Context, string) (int, error) { return 0, nil }
func (fakeScoreStore) TeamSolvedChallenges(context.Context, string) (map[string]bool, error) {
	return nil, nil
}
func (fakeScoreStore) Scoreboard(context.Context) ([]store.ScoreboardEntry, error) { return nil, nil }
func (fakeScoreStore) TeamSolves(context.Context, string) ([]store.Solve, error)   { return nil, nil }

func newTestConfig() Config {
	users := newFakeUserStore()
	tokens, err := auth.NewTokenIssuer(bytes.Repeat([]byte("a"), 32), time.Hour)
	if err != nil {
		panic(err)
	}
	return Config{
		Auth:   &auth.Handler{Store: users, Tokens: tokens},
		Teams:  &teams.Handler{Store: &fakeTeamStore{fakeUserStore: users}, Tokens: tokens},
		Engine: &engine.Handler{Challenges: fakeChallengeSource{}, Scores: fakeScoreStore{}, WorldSeed: "seed-1"},
	}
}

func TestHealthz(t *testing.T) {
	h := New(newTestConfig())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestMetricsEndpointUnauthenticated(t *testing.T) {
	h := New(newTestConfig())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "cyberkube_") {
		t.Error("metrics output missing cyberkube_ prefixed series")
	}
}

func TestAPIv1AndLegacyAliasShareHandlers(t *testing.T) {
	h := New(newTestConfig())

	for _, prefix := range []string{"/api/v1", "/api"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, prefix+"/challenges", nil))
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s/challenges unauthenticated: status = %d, want 401", prefix, w.Code)
		}
	}

	// Register through /api/v1, log in through the legacy /api alias: same
	// underlying store proves both prefixes hit the same handler instance.
	registerBody := `{"username":"leo","email":"leo@example.com","password":"password123"}`
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/register", strings.NewReader(registerBody)))
	if w.Code != http.StatusCreated {
		t.Fatalf("register via /api/v1: status = %d, want 201: %s", w.Code, w.Body)
	}

	loginBody := `{"login":"leo","password":"password123"}`
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(loginBody)))
	if w.Code != http.StatusOK {
		t.Fatalf("login via /api alias: status = %d, want 200: %s", w.Code, w.Body)
	}
}

func TestWorldRouteMountedUnderV1(t *testing.T) {
	cfg := newTestConfig()
	h := New(cfg)

	tokens := cfg.Auth.Tokens
	token, err := tokens.Issue("user-1", "leo", "team-1")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/world", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body)
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["seed"] != "seed-1" {
		t.Errorf("seed = %v, want seed-1", body["seed"])
	}
}

func TestRequestLoggerEmitsStructuredFields(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(&buf, "json")
	cfg := newTestConfig()
	cfg.Logger = logger
	h := New(cfg)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("log line not valid JSON: %v (%s)", err, buf.String())
	}
	if line["route"] != "/healthz" {
		t.Errorf("route = %v, want /healthz", line["route"])
	}
	if line["method"] != "GET" {
		t.Errorf("method = %v, want GET", line["method"])
	}
	if _, ok := line["duration_ms"]; !ok {
		t.Error("missing duration_ms field")
	}
	if _, ok := line["request_id"]; !ok {
		t.Error("missing request_id field")
	}
}
