package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/CyberKube-ISEN/cyberkube/internal/store"
)

// fakeStore is an in-memory UserStore.
type fakeStore struct {
	users map[string]*store.User // by id
}

func newFakeStore() *fakeStore {
	return &fakeStore{users: map[string]*store.User{}}
}

func (f *fakeStore) CreateUser(_ context.Context, username, email, hash string) (*store.User, error) {
	for _, u := range f.users {
		if u.Username == username || u.Email == email {
			return nil, store.ErrConflict
		}
	}
	u := &store.User{
		ID:           "user-" + username,
		Username:     username,
		Email:        email,
		PasswordHash: hash,
		CreatedAt:    time.Now(),
	}
	f.users[u.ID] = u
	return u, nil
}

func (f *fakeStore) GetUserByLogin(_ context.Context, login string) (*store.User, error) {
	for _, u := range f.users {
		if u.Username == login || u.Email == login {
			return u, nil
		}
	}
	return nil, store.ErrNotFound
}

func (f *fakeStore) GetUserByID(_ context.Context, id string) (*store.User, error) {
	if u, ok := f.users[id]; ok {
		return u, nil
	}
	return nil, store.ErrNotFound
}

func newTestHandler(t *testing.T) (*Handler, *fakeStore) {
	t.Helper()
	tokens, err := NewTokenIssuer(testSecret, time.Hour)
	if err != nil {
		t.Fatalf("NewTokenIssuer: %v", err)
	}
	fs := newFakeStore()
	return &Handler{Store: fs, Tokens: tokens}, fs
}

func postJSON(t *testing.T, handler http.HandlerFunc, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler(w, req)
	return w
}

func TestRegisterCreatesUserWithBcryptHash(t *testing.T) {
	h, fs := newTestHandler(t)

	w := postJSON(t, h.Register, `{"username":"leo","email":"leo@example.com","password":"password123"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", w.Code, w.Body)
	}

	u, err := fs.GetUserByLogin(context.Background(), "leo")
	if err != nil {
		t.Fatalf("user not stored: %v", err)
	}
	if u.PasswordHash == "password123" || !strings.HasPrefix(u.PasswordHash, "$2") {
		t.Errorf("password not bcrypt-hashed: %q", u.PasswordHash)
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte("password123")) != nil {
		t.Error("stored hash does not match password")
	}
}

func TestRegisterRejectsDuplicateUsername(t *testing.T) {
	h, _ := newTestHandler(t)
	postJSON(t, h.Register, `{"username":"leo","email":"leo@example.com","password":"password123"}`)
	w := postJSON(t, h.Register, `{"username":"leo","email":"other@example.com","password":"password123"}`)
	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", w.Code)
	}
}

func TestRegisterRejectsShortPassword(t *testing.T) {
	h, _ := newTestHandler(t)
	w := postJSON(t, h.Register, `{"username":"leo","email":"leo@example.com","password":"short"}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestRegisterRejectsInvalidEmail(t *testing.T) {
	h, _ := newTestHandler(t)
	w := postJSON(t, h.Register, `{"username":"leo","email":"not-an-email","password":"password123"}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestLoginIssuesValidJWT(t *testing.T) {
	h, _ := newTestHandler(t)
	postJSON(t, h.Register, `{"username":"leo","email":"leo@example.com","password":"password123"}`)

	w := postJSON(t, h.Login, `{"login":"leo","password":"password123"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body)
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	claims, err := h.Tokens.Verify(resp["token"])
	if err != nil {
		t.Fatalf("returned token invalid: %v", err)
	}
	if claims.Username != "leo" {
		t.Errorf("claims.Username = %q, want leo", claims.Username)
	}
	if claims.ExpiresAt == nil || time.Until(claims.ExpiresAt.Time) <= 0 {
		t.Error("token has no future expiry")
	}
}

func TestLoginWrongPasswordAndUnknownUserAreIndistinguishable(t *testing.T) {
	h, _ := newTestHandler(t)
	postJSON(t, h.Register, `{"username":"leo","email":"leo@example.com","password":"password123"}`)

	wrong := postJSON(t, h.Login, `{"login":"leo","password":"wrongpass1"}`)
	unknown := postJSON(t, h.Login, `{"login":"ghost","password":"wrongpass1"}`)

	if wrong.Code != http.StatusUnauthorized || unknown.Code != http.StatusUnauthorized {
		t.Fatalf("statuses = %d/%d, want 401/401", wrong.Code, unknown.Code)
	}
	if wrong.Body.String() != unknown.Body.String() {
		t.Errorf("bodies differ, existence leak: %q vs %q", wrong.Body, unknown.Body)
	}
}

func TestVerifyAcceptsBearerAndCookie(t *testing.T) {
	h, _ := newTestHandler(t)
	token, err := h.Tokens.Issue("user-1", "leo", "team-1")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/auth/verify", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.Verify(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("bearer: status = %d, want 200", w.Code)
	}
	if got := w.Header().Get("X-Team-Id"); got != "team-1" {
		t.Errorf("X-Team-Id = %q, want team-1", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/auth/verify", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: token})
	w = httptest.NewRecorder()
	h.Verify(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("cookie: status = %d, want 200", w.Code)
	}
}

func TestVerifyRejectsMissingAndInvalidToken(t *testing.T) {
	h, _ := newTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/auth/verify", nil)
	w := httptest.NewRecorder()
	h.Verify(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("no token: status = %d, want 401", w.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/auth/verify", nil)
	req.Header.Set("Authorization", "Bearer garbage")
	w = httptest.NewRecorder()
	h.Verify(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("garbage token: status = %d, want 401", w.Code)
	}
}

func TestMiddlewareStoresClaims(t *testing.T) {
	h, _ := newTestHandler(t)
	token, _ := h.Tokens.Issue("user-1", "leo", "team-1")

	var got *Claims
	inner := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = ClaimsFromContext(r.Context())
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.Middleware(inner).ServeHTTP(w, req)

	if got == nil || got.Subject != "user-1" {
		t.Errorf("claims in context = %+v, want subject user-1", got)
	}
}
