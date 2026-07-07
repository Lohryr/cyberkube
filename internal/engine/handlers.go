package engine

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/CyberKube-ISEN/cyberkube/internal/auth"
	"github.com/CyberKube-ISEN/cyberkube/internal/events"
	"github.com/CyberKube-ISEN/cyberkube/internal/k8s"
	"github.com/CyberKube-ISEN/cyberkube/internal/metrics"
	"github.com/CyberKube-ISEN/cyberkube/internal/store"
)

// WorldGeneratorVersion is served in GET /api/v1/world so clients can adapt
// their procedural generation algorithm without breaking reproducibility for
// older world descriptors.
const WorldGeneratorVersion = 1

// ChallengeSource reads challenges and manages instances (implemented by
// *k8s.Client; faked in tests).
type ChallengeSource interface {
	ListChallenges(ctx context.Context) ([]k8s.Challenge, error)
	GetChallenge(ctx context.Context, name string) (*k8s.Challenge, error)
	GetInstance(ctx context.Context, name string) (*k8s.Instance, error)
	CreateInstance(ctx context.Context, name, challengeName, sourceID string, timeout int64) (*k8s.Instance, error)
	DeleteInstance(ctx context.Context, name string) error
	MarkInstanceSolved(ctx context.Context, name string) error
}

// ScoreStore persists submissions, solves, and the scoreboard (implemented
// by *store.Store; faked in tests).
type ScoreStore interface {
	RecordSubmission(ctx context.Context, teamID, userID, challenge, flag string, correct bool) error
	InsertSolve(ctx context.Context, teamID, challenge string, points int) (bool, error)
	CountSolves(ctx context.Context, challenge string) (int, error)
	TeamSolvedChallenges(ctx context.Context, teamID string) (map[string]bool, error)
	Scoreboard(ctx context.Context) ([]store.ScoreboardEntry, error)
	TeamSolves(ctx context.Context, teamID string) ([]store.Solve, error)
}

// AttachmentFetcher retrieves attachment content by OCI reference.
type AttachmentFetcher interface {
	Fetch(ctx context.Context, ociRef, sha256hex string) ([]byte, error)
}

// EventPublisher emits real-time events for WS /api/v1/events subscribers
// (implemented by *events.Publisher). Optional: a nil Events field simply
// disables real-time notifications without touching engine logic.
type EventPublisher interface {
	Publish(ctx context.Context, eventType string, payload any)
}

// Handler serves the challenge engine endpoints. All routes require
// auth.Middleware; team membership is enforced per-handler where needed.
type Handler struct {
	Challenges  ChallengeSource
	Scores      ScoreStore
	Attachments AttachmentFetcher
	Events      EventPublisher

	// WorldSeed governs the frontend's procedural world generation. It is
	// fixed at process start (env WORLD_SEED, or generated once and
	// persisted in the settings table) so every replica and every restart
	// serves the same seed.
	WorldSeed string
}

type challengeView struct {
	Name         string   `json:"name"`
	DisplayName  string   `json:"displayName"`
	Category     string   `json:"category"`
	Description  string   `json:"description"`
	Mode         string   `json:"mode"`
	Value        int      `json:"value"`
	Solves       int      `json:"solves"`
	SolvedByTeam bool     `json:"solvedByTeam"`
	Attachments  []string `json:"attachments,omitempty"`
}

// List handles GET /api/challenges.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	views, err := h.challengeViews(r)
	if err != nil {
		slog.Error("list challenges failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, views)
}

type worldView struct {
	Seed             string          `json:"seed"`
	GeneratorVersion int             `json:"generatorVersion"`
	TeamMode         bool            `json:"teamMode"`
	Challenges       []challengeView `json:"challenges"`
}

// World handles GET /api/v1/world: the single descriptor a client needs to
// deterministically render the procedural world (seed + generator version)
// alongside the same challenge projection served by List.
func (h *Handler) World(w http.ResponseWriter, r *http.Request) {
	views, err := h.challengeViews(r)
	if err != nil {
		slog.Error("world challenges failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, worldView{
		Seed:             h.WorldSeed,
		GeneratorVersion: WorldGeneratorVersion,
		TeamMode:         true, // instances and scoring are always team-scoped today
		Challenges:       views,
	})
}

// challengeViews builds the visible-challenge projection for the requesting
// user's team, shared by List and World so they never drift apart.
func (h *Handler) challengeViews(r *http.Request) ([]challengeView, error) {
	claims := auth.ClaimsFromContext(r.Context())

	challenges, err := h.Challenges.ListChallenges(r.Context())
	if err != nil {
		return nil, fmt.Errorf("list challenges: %w", err)
	}
	metrics.ChallengeCacheSize.Set(float64(len(challenges)))

	solved := map[string]bool{}
	if claims.TeamID != "" {
		if solved, err = h.Scores.TeamSolvedChallenges(r.Context(), claims.TeamID); err != nil {
			return nil, fmt.Errorf("team solves lookup: %w", err)
		}
	}

	views := make([]challengeView, 0, len(challenges))
	for i := range challenges {
		ch := &challenges[i]
		if ch.State != "visible" {
			continue
		}
		solveCount, err := h.Scores.CountSolves(r.Context(), ch.Name)
		if err != nil {
			return nil, fmt.Errorf("count solves for %s: %w", ch.Name, err)
		}
		views = append(views, h.view(ch, solveCount, solved[ch.Name]))
	}
	return views, nil
}

func (h *Handler) view(ch *k8s.Challenge, solveCount int, solvedByTeam bool) challengeView {
	v := challengeView{
		Name:         ch.Name,
		DisplayName:  ch.DisplayName,
		Category:     ch.Category,
		Description:  ch.Description,
		Mode:         ch.Mode,
		Value:        CurrentValue(ch, solveCount),
		Solves:       solveCount,
		SolvedByTeam: solvedByTeam,
	}
	for _, a := range ch.Attachments {
		v.Attachments = append(v.Attachments, a.Name)
	}
	return v
}

// visibleChallenge loads a challenge and 404s hidden/unknown ones so hidden
// challenges are indistinguishable from nonexistent ones.
func (h *Handler) visibleChallenge(w http.ResponseWriter, r *http.Request) *k8s.Challenge {
	name := chi.URLParam(r, "name")
	ch, err := h.Challenges.GetChallenge(r.Context(), name)
	if errors.Is(err, k8s.ErrNotFound) || (err == nil && ch.State != "visible") {
		writeError(w, http.StatusNotFound, "challenge not found")
		return nil
	}
	if err != nil {
		slog.Error("get challenge failed", "challenge", name, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return nil
	}
	return ch
}

// requireTeam extracts claims and rejects users without a team.
func requireTeam(w http.ResponseWriter, r *http.Request) *auth.Claims {
	claims := auth.ClaimsFromContext(r.Context())
	if claims.TeamID == "" {
		writeError(w, http.StatusForbidden, "join a team first")
		return nil
	}
	return claims
}

// Attachment handles GET /api/challenges/{name}/attachments/{attachment}.
func (h *Handler) Attachment(w http.ResponseWriter, r *http.Request) {
	ch := h.visibleChallenge(w, r)
	if ch == nil {
		return
	}
	attName := chi.URLParam(r, "attachment")
	for _, a := range ch.Attachments {
		if a.Name != attName {
			continue
		}
		content, err := h.Attachments.Fetch(r.Context(), a.OCIRef, a.SHA256)
		if err != nil {
			slog.Error("attachment fetch failed", "ref", a.OCIRef, "err", err)
			writeError(w, http.StatusBadGateway, "attachment unavailable")
			return
		}
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", a.Name))
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(content)
		return
	}
	writeError(w, http.StatusNotFound, "attachment not found")
}

type submitRequest struct {
	Flag string `json:"flag"`
}

type submitResponse struct {
	Correct bool `json:"correct"`
	Points  int  `json:"points,omitempty"`
}

// Submit handles POST /api/challenges/{name}/submit for both modes.
func (h *Handler) Submit(w http.ResponseWriter, r *http.Request) {
	claims := requireTeam(w, r)
	if claims == nil {
		return
	}
	ch := h.visibleChallenge(w, r)
	if ch == nil {
		return
	}

	var req submitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	submitted := strings.TrimSpace(req.Flag)
	if submitted == "" {
		writeError(w, http.StatusBadRequest, "flag is required")
		return
	}

	correct := false
	var solvedInstance string
	switch ch.Mode {
	case "static":
		if ch.StaticFlag == "" {
			// Operator has not (yet) decrypted the flag; challenge is not
			// submittable.
			writeError(w, http.StatusConflict, "challenge not ready for submissions")
			return
		}
		correct = subtle.ConstantTimeCompare([]byte(submitted), []byte(ch.StaticFlag)) == 1
	default: // dynamic: only the requesting team's own instance counts
		inst, err := h.Challenges.GetInstance(r.Context(), instanceName(ch, claims))
		if err == nil {
			for _, f := range inst.Flags {
				if subtle.ConstantTimeCompare([]byte(submitted), []byte(f)) == 1 {
					correct = true
					solvedInstance = inst.Name
					break
				}
			}
		} else if !errors.Is(err, k8s.ErrNotFound) {
			slog.Error("get instance failed", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}

	if err := h.Scores.RecordSubmission(r.Context(), claims.TeamID, claims.Subject, ch.Name, submitted, correct); err != nil {
		slog.Error("record submission failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	metrics.SubmissionsTotal.WithLabelValues(strconv.FormatBool(correct)).Inc()
	if !correct {
		writeJSON(w, http.StatusOK, submitResponse{Correct: false})
		return
	}

	solveCount, err := h.Scores.CountSolves(r.Context(), ch.Name)
	if err != nil {
		slog.Error("count solves failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	points := CurrentValue(ch, solveCount)
	awarded, err := h.Scores.InsertSolve(r.Context(), claims.TeamID, ch.Name, points)
	if err != nil {
		slog.Error("insert solve failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !awarded {
		// Correct flag, but the team already solved this challenge.
		writeJSON(w, http.StatusOK, submitResponse{Correct: true, Points: 0})
		return
	}

	if solvedInstance != "" && ch.DestroyOnFlag {
		if err := h.Challenges.MarkInstanceSolved(r.Context(), solvedInstance); err != nil {
			slog.Error("mark instance solved failed", "instance", solvedInstance, "err", err)
		}
	}
	if h.Events != nil {
		h.Events.Publish(r.Context(), events.TypeChallengeSolved, map[string]any{
			"teamId": claims.TeamID, "challenge": ch.Name, "points": points,
		})
		h.Events.Publish(r.Context(), events.TypeScoreboardUpdated, nil)
	}
	writeJSON(w, http.StatusOK, submitResponse{Correct: true, Points: points})
}

type instanceView struct {
	Status         string `json:"status"` // none | pending | ready
	ConnectionInfo string `json:"connectionInfo,omitempty"`
	ExpiresAt      string `json:"expiresAt,omitempty"`
}

// Launch handles POST /api/challenges/{name}/launch.
func (h *Handler) Launch(w http.ResponseWriter, r *http.Request) {
	claims := requireTeam(w, r)
	if claims == nil {
		return
	}
	ch := h.visibleChallenge(w, r)
	if ch == nil {
		return
	}
	if ch.Mode != "dynamic" {
		writeError(w, http.StatusBadRequest, "static challenges have no instances")
		return
	}

	name := instanceName(ch, claims)

	// Reuse an existing, non-expired instance instead of duplicating.
	if inst, err := h.Challenges.GetInstance(r.Context(), name); err == nil {
		if inst.Until == nil || time.Now().Before(*inst.Until) {
			writeJSON(w, http.StatusOK, viewInstance(inst))
			return
		}
		// Expired but not yet reaped: replace it.
		if err := h.Challenges.DeleteInstance(r.Context(), name); err != nil {
			slog.Error("delete expired instance failed", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	} else if !errors.Is(err, k8s.ErrNotFound) {
		slog.Error("get instance failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	timeout := ch.Timeout
	if timeout <= 0 {
		timeout = 600
	}
	inst, err := h.Challenges.CreateInstance(r.Context(), name, ch.Name, sourceID(ch, claims), timeout)
	if err != nil {
		slog.Error("create instance failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	view := viewInstance(inst)
	if h.Events != nil {
		h.Events.Publish(r.Context(), events.TypeInstanceStatus, map[string]any{
			"challenge": ch.Name, "teamId": claims.TeamID, "status": view.Status,
		})
	}
	writeJSON(w, http.StatusAccepted, view)
}

// InstanceStatus handles GET /api/challenges/{name}/instance.
func (h *Handler) InstanceStatus(w http.ResponseWriter, r *http.Request) {
	claims := requireTeam(w, r)
	if claims == nil {
		return
	}
	ch := h.visibleChallenge(w, r)
	if ch == nil {
		return
	}

	inst, err := h.Challenges.GetInstance(r.Context(), instanceName(ch, claims))
	if errors.Is(err, k8s.ErrNotFound) {
		writeJSON(w, http.StatusOK, instanceView{Status: "none"})
		return
	}
	if err != nil {
		slog.Error("get instance failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, viewInstance(inst))
}

// Scoreboard handles GET /api/scoreboard.
func (h *Handler) Scoreboard(w http.ResponseWriter, r *http.Request) {
	entries, err := h.Scores.Scoreboard(r.Context())
	if err != nil {
		slog.Error("scoreboard failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	type entryView struct {
		TeamID   string `json:"teamId"`
		TeamName string `json:"teamName"`
		Points   int    `json:"points"`
	}
	views := make([]entryView, 0, len(entries))
	for _, e := range entries {
		views = append(views, entryView{TeamID: e.TeamID, TeamName: e.TeamName, Points: e.Points})
	}
	writeJSON(w, http.StatusOK, views)
}

// instanceName derives the deterministic ChallengeInstance name for a
// team/user + challenge pair; determinism is what makes launch idempotent.
func instanceName(ch *k8s.Challenge, claims *auth.Claims) string {
	sum := sha256.Sum256([]byte(sourceID(ch, claims)))
	return fmt.Sprintf("%s-%s", ch.Name, hex.EncodeToString(sum[:])[:10])
}

// sourceID scopes an instance to the team, or to the individual user when
// the challenge is not shared.
func sourceID(ch *k8s.Challenge, claims *auth.Claims) string {
	if ch.Shared {
		return claims.TeamID
	}
	return claims.Subject
}

func viewInstance(inst *k8s.Instance) instanceView {
	v := instanceView{Status: "pending"}
	if inst.Ready {
		v.Status = "ready"
		v.ConnectionInfo = inst.ConnectionInfo
	}
	if inst.Until != nil {
		v.ExpiresAt = inst.Until.UTC().Format(time.RFC3339)
	}
	return v
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
