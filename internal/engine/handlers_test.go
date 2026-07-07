package engine

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/CyberKube-ISEN/cyberkube/internal/auth"
	"github.com/CyberKube-ISEN/cyberkube/internal/k8s"
	"github.com/CyberKube-ISEN/cyberkube/internal/store"
)

// --- fakes ---

type fakeChallenges struct {
	challenges map[string]*k8s.Challenge
	instances  map[string]*k8s.Instance
	created    []string
	deleted    []string
	solved     []string
}

func (f *fakeChallenges) ListChallenges(context.Context) ([]k8s.Challenge, error) {
	out := make([]k8s.Challenge, 0, len(f.challenges))
	for _, c := range f.challenges {
		out = append(out, *c)
	}
	return out, nil
}

func (f *fakeChallenges) GetChallenge(_ context.Context, name string) (*k8s.Challenge, error) {
	if c, ok := f.challenges[name]; ok {
		return c, nil
	}
	return nil, k8s.ErrNotFound
}

func (f *fakeChallenges) GetInstance(_ context.Context, name string) (*k8s.Instance, error) {
	if i, ok := f.instances[name]; ok {
		return i, nil
	}
	return nil, k8s.ErrNotFound
}

func (f *fakeChallenges) CreateInstance(_ context.Context, name, challengeName, sourceID string, _ int64) (*k8s.Instance, error) {
	inst := &k8s.Instance{Name: name, ChallengeName: challengeName, SourceID: sourceID, Phase: "Pending"}
	f.instances[name] = inst
	f.created = append(f.created, name)
	return inst, nil
}

func (f *fakeChallenges) DeleteInstance(_ context.Context, name string) error {
	delete(f.instances, name)
	f.deleted = append(f.deleted, name)
	return nil
}

func (f *fakeChallenges) MarkInstanceSolved(_ context.Context, name string) error {
	f.solved = append(f.solved, name)
	return nil
}

type fakeScores struct {
	solveCounts map[string]int            // challenge -> distinct team solves
	teamSolves  map[string]map[string]int // teamID -> challenge -> points
	submissions int
}

func newFakeScores() *fakeScores {
	return &fakeScores{solveCounts: map[string]int{}, teamSolves: map[string]map[string]int{}}
}

func (f *fakeScores) RecordSubmission(context.Context, string, string, string, string, bool) error {
	f.submissions++
	return nil
}

func (f *fakeScores) InsertSolve(_ context.Context, teamID, challenge string, points int) (bool, error) {
	if f.teamSolves[teamID] == nil {
		f.teamSolves[teamID] = map[string]int{}
	}
	if _, done := f.teamSolves[teamID][challenge]; done {
		return false, nil
	}
	f.teamSolves[teamID][challenge] = points
	f.solveCounts[challenge]++
	return true, nil
}

func (f *fakeScores) CountSolves(_ context.Context, challenge string) (int, error) {
	return f.solveCounts[challenge], nil
}

func (f *fakeScores) TeamSolvedChallenges(_ context.Context, teamID string) (map[string]bool, error) {
	out := map[string]bool{}
	for c := range f.teamSolves[teamID] {
		out[c] = true
	}
	return out, nil
}

func (f *fakeScores) Scoreboard(context.Context) ([]store.ScoreboardEntry, error) {
	return nil, nil
}

func (f *fakeScores) TeamSolves(context.Context, string) ([]store.Solve, error) {
	return nil, nil
}

type fakeFetcher struct{ content []byte }

func (f *fakeFetcher) Fetch(context.Context, string, string) ([]byte, error) {
	if f.content == nil {
		return nil, errors.New("no content")
	}
	return f.content, nil
}

type publishedEvent struct {
	eventType string
	payload   any
}

type fakePublisher struct{ events []publishedEvent }

func (f *fakePublisher) Publish(_ context.Context, eventType string, payload any) {
	f.events = append(f.events, publishedEvent{eventType: eventType, payload: payload})
}

// --- helpers ---

func withClaims(req *http.Request, subject, teamID string) *http.Request {
	claims := &auth.Claims{Username: "player", TeamID: teamID}
	claims.Subject = subject
	return req.WithContext(auth.ContextWithClaims(req.Context(), claims))
}

// route builds a chi context so URLParam works in tests.
func route(req *http.Request, params map[string]string) *http.Request {
	rc := chi.NewRouteContext()
	for k, v := range params {
		rc.URLParams.Add(k, v)
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rc))
}

func decode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode: %v (%s)", err, w.Body)
	}
	return v
}

// --- tests ---

func TestListExcludesHiddenChallenges(t *testing.T) {
	ch := &fakeChallenges{challenges: map[string]*k8s.Challenge{
		"visible-one": {Name: "visible-one", Mode: "static", State: "visible", Value: 100},
		"hidden-one":  {Name: "hidden-one", Mode: "static", State: "hidden", Value: 100},
	}}
	h := &Handler{Challenges: ch, Scores: newFakeScores()}

	req := withClaims(httptest.NewRequest(http.MethodGet, "/api/challenges", nil), "u1", "t1")
	w := httptest.NewRecorder()
	h.List(w, req)

	views := decode[[]challengeView](t, w)
	if len(views) != 1 || views[0].Name != "visible-one" {
		t.Errorf("List returned %+v, want only visible-one", views)
	}
}

func TestSubmitStaticCorrectAwardsPointsOncePerTeam(t *testing.T) {
	ch := &fakeChallenges{challenges: map[string]*k8s.Challenge{
		"stego": {Name: "stego", Mode: "static", State: "visible", Value: 50, StaticFlag: "CTF{correct}"},
	}}
	scores := newFakeScores()
	pub := &fakePublisher{}
	h := &Handler{Challenges: ch, Scores: scores, Events: pub}

	do := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"flag":"CTF{correct}"}`))
		req = withClaims(route(req, map[string]string{"name": "stego"}), "u1", "team-1")
		w := httptest.NewRecorder()
		h.Submit(w, req)
		return w
	}

	first := decode[submitResponse](t, do())
	if !first.Correct || first.Points != 50 {
		t.Errorf("first submit = %+v, want correct/50", first)
	}
	second := decode[submitResponse](t, do())
	if !second.Correct || second.Points != 0 {
		t.Errorf("second submit = %+v, want correct/0 (no double award)", second)
	}
	if scores.solveCounts["stego"] != 1 {
		t.Errorf("solve count = %d, want 1", scores.solveCounts["stego"])
	}

	// Only the first (awarded) submit publishes real-time events; the
	// second is a correct-but-already-solved resubmit and must not.
	if len(pub.events) != 2 {
		t.Fatalf("published %d events, want 2 (solved + scoreboard)", len(pub.events))
	}
	if pub.events[0].eventType != "challenge.solved" {
		t.Errorf("first event type = %q, want challenge.solved", pub.events[0].eventType)
	}
	if pub.events[1].eventType != "scoreboard.updated" {
		t.Errorf("second event type = %q, want scoreboard.updated", pub.events[1].eventType)
	}
}

func TestSubmitDoesNotPublishOnWrongFlag(t *testing.T) {
	ch := &fakeChallenges{challenges: map[string]*k8s.Challenge{
		"stego": {Name: "stego", Mode: "static", State: "visible", Value: 50, StaticFlag: "CTF{correct}"},
	}}
	pub := &fakePublisher{}
	h := &Handler{Challenges: ch, Scores: newFakeScores(), Events: pub}

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"flag":"CTF{nope}"}`))
	req = withClaims(route(req, map[string]string{"name": "stego"}), "u1", "team-1")
	w := httptest.NewRecorder()
	h.Submit(w, req)

	if len(pub.events) != 0 {
		t.Errorf("published %d events for a wrong flag, want 0", len(pub.events))
	}
}

func TestSubmitStaticWrongFlag(t *testing.T) {
	ch := &fakeChallenges{challenges: map[string]*k8s.Challenge{
		"stego": {Name: "stego", Mode: "static", State: "visible", Value: 50, StaticFlag: "CTF{correct}"},
	}}
	scores := newFakeScores()
	h := &Handler{Challenges: ch, Scores: scores}

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"flag":"CTF{nope}"}`))
	req = withClaims(route(req, map[string]string{"name": "stego"}), "u1", "team-1")
	w := httptest.NewRecorder()
	h.Submit(w, req)

	if resp := decode[submitResponse](t, w); resp.Correct {
		t.Error("wrong flag accepted")
	}
	if scores.solveCounts["stego"] != 0 {
		t.Error("wrong flag awarded a solve")
	}
}

func TestSubmitDynamicRejectsAnotherTeamsFlag(t *testing.T) {
	// A shared dynamic challenge: team-1 owns an instance whose flag is
	// CTF{team1_only}. team-2 submits that flag; since it is not team-2's own
	// instance flag, it must be rejected.
	web := &k8s.Challenge{Name: "web", Mode: "dynamic", State: "visible", Value: 100, Shared: true}
	ch := &fakeChallenges{
		challenges: map[string]*k8s.Challenge{"web": web},
		instances:  map[string]*k8s.Instance{},
	}
	h := &Handler{Challenges: ch, Scores: newFakeScores()}

	// Seed team-1's instance under its deterministic name.
	team1 := &auth.Claims{TeamID: "team-1"}
	team1.Subject = "u1"
	inst1 := instanceName(web, team1)
	ch.instances[inst1] = &k8s.Instance{Name: inst1, Flags: []string{"CTF{team1_only}"}}

	// team-2 submits team-1's flag; team-2 has no instance of its own.
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"flag":"CTF{team1_only}"}`))
	req = withClaims(route(req, map[string]string{"name": "web"}), "u2", "team-2")
	w := httptest.NewRecorder()
	h.Submit(w, req)

	if resp := decode[submitResponse](t, w); resp.Correct {
		t.Error("team-2 solved with team-1's flag")
	}
}

func TestLaunchReusesExistingInstance(t *testing.T) {
	ch := &fakeChallenges{
		challenges: map[string]*k8s.Challenge{
			"web": {Name: "web", Mode: "dynamic", State: "visible", Value: 100, Timeout: 600},
		},
		instances: map[string]*k8s.Instance{},
	}
	pub := &fakePublisher{}
	h := &Handler{Challenges: ch, Scores: newFakeScores(), Events: pub}

	launch := func() {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req = withClaims(route(req, map[string]string{"name": "web"}), "u1", "team-1")
		w := httptest.NewRecorder()
		h.Launch(w, req)
	}
	launch()
	launch()

	if len(ch.created) != 1 {
		t.Errorf("created %d instances, want 1 (reuse)", len(ch.created))
	}
	// Only the instance-creating launch publishes instance.status; the
	// reused-instance launch does not go through CreateInstance again.
	if len(pub.events) != 1 {
		t.Fatalf("published %d events, want 1 (instance.status on create)", len(pub.events))
	}
	if pub.events[0].eventType != "instance.status" {
		t.Errorf("event type = %q, want instance.status", pub.events[0].eventType)
	}
}

func TestWorldReturnsSeedAndChallenges(t *testing.T) {
	ch := &fakeChallenges{challenges: map[string]*k8s.Challenge{
		"visible-one": {Name: "visible-one", Mode: "static", State: "visible", Value: 100},
		"hidden-one":  {Name: "hidden-one", Mode: "static", State: "hidden", Value: 100},
	}}
	h := &Handler{Challenges: ch, Scores: newFakeScores(), WorldSeed: "deadbeef"}

	req := withClaims(httptest.NewRequest(http.MethodGet, "/api/v1/world", nil), "u1", "t1")
	w := httptest.NewRecorder()
	h.World(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body)
	}
	world := decode[struct {
		Seed             string          `json:"seed"`
		GeneratorVersion int             `json:"generatorVersion"`
		TeamMode         bool            `json:"teamMode"`
		Challenges       []challengeView `json:"challenges"`
	}](t, w)

	if world.Seed != "deadbeef" {
		t.Errorf("seed = %q, want deadbeef", world.Seed)
	}
	if world.GeneratorVersion != WorldGeneratorVersion {
		t.Errorf("generatorVersion = %d, want %d", world.GeneratorVersion, WorldGeneratorVersion)
	}
	if !world.TeamMode {
		t.Error("teamMode = false, want true")
	}
	if len(world.Challenges) != 1 || world.Challenges[0].Name != "visible-one" {
		t.Errorf("world.Challenges = %+v, want only visible-one", world.Challenges)
	}
}

func TestSubmitRequiresTeam(t *testing.T) {
	ch := &fakeChallenges{challenges: map[string]*k8s.Challenge{
		"stego": {Name: "stego", Mode: "static", State: "visible", StaticFlag: "CTF{x}"},
	}}
	h := &Handler{Challenges: ch, Scores: newFakeScores()}

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"flag":"CTF{x}"}`))
	req = withClaims(route(req, map[string]string{"name": "stego"}), "u1", "") // no team
	w := httptest.NewRecorder()
	h.Submit(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 (needs team)", w.Code)
	}
}
