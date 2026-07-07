// cyberkube is the all-in-one CTF platform backend: native auth, teams,
// scoring, and challenge orchestration against the chall-operator CRDs.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/CyberKube-ISEN/cyberkube/internal/auth"
	"github.com/CyberKube-ISEN/cyberkube/internal/engine"
	"github.com/CyberKube-ISEN/cyberkube/internal/events"
	"github.com/CyberKube-ISEN/cyberkube/internal/k8s"
	"github.com/CyberKube-ISEN/cyberkube/internal/oci"
	"github.com/CyberKube-ISEN/cyberkube/internal/server"
	"github.com/CyberKube-ISEN/cyberkube/internal/store"
	"github.com/CyberKube-ISEN/cyberkube/internal/teams"
	"github.com/CyberKube-ISEN/cyberkube/internal/tracing"
)

// version is the service.version reported on every trace's resource
// attributes. Overridden at build time via
// -ldflags "-X main.version=$(git describe)"; no such flag is wired into the
// Dockerfile/CI yet, so this stays "dev" until that's added.
var version = "dev"

// informerSyncTimeout bounds how long startup waits for the Challenge
// informer's initial cache sync before failing fast (better a crash-loop
// with a clear reason than a pod that silently serves ErrCacheNotSynced
// forever behind a passing readiness probe).
const informerSyncTimeout = 30 * time.Second

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	logger := server.NewLogger(os.Stdout, os.Getenv("LOG_FORMAT"))
	slog.SetDefault(logger)

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return errors.New("DATABASE_URL is required")
	}
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		return errors.New("JWT_SECRET is required")
	}
	cookieDomain := os.Getenv("COOKIE_DOMAIN") // e.g. ".ctf.rokhnir.dev"
	listenAddr := os.Getenv("LISTEN_ADDR")
	if listenAddr == "" {
		listenAddr = ":8080"
	}
	secureCookie := os.Getenv("INSECURE_COOKIE") != "true"
	challengeNamespace := os.Getenv("CHALLENGE_NAMESPACE")
	if challengeNamespace == "" {
		challengeNamespace = "ctf-instances"
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Must run before store.New: otelpgx captures the global TracerProvider
	// at construction time, so the pool's queries would stay untraced
	// forever if Setup ran after it.
	tracerShutdown, err := tracing.Setup(ctx, version)
	if err != nil {
		return fmt.Errorf("tracing setup: %w", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := tracerShutdown(shutdownCtx); err != nil {
			slog.Error("tracer shutdown", "err", err)
		}
	}()

	st, err := store.New(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	defer st.Close()

	worldSeed, err := resolveWorldSeed(ctx, st)
	if err != nil {
		return fmt.Errorf("world seed: %w", err)
	}

	tokens, err := auth.NewTokenIssuer([]byte(jwtSecret), 24*time.Hour)
	if err != nil {
		return fmt.Errorf("token issuer: %w", err)
	}

	kubeClient, err := k8s.New(os.Getenv("KUBECONFIG"), challengeNamespace)
	if err != nil {
		return fmt.Errorf("kubernetes client: %w", err)
	}

	syncCtx, cancelSync := context.WithTimeout(ctx, informerSyncTimeout)
	defer cancelSync()
	if err := kubeClient.StartInformer(syncCtx); err != nil {
		return fmt.Errorf("start challenge informer: %w", err)
	}
	slog.Info("challenge informer cache synced", "namespace", challengeNamespace)

	// Real-time events (WS /api/v1/events): a per-pod hub fed by PostgreSQL
	// LISTEN/NOTIFY so every replica's clients see the same events
	// regardless of which pod produced them. The listen loop runs for the
	// process lifetime and retries on connection loss.
	hub := events.NewHub()
	publisher := events.NewPublisher(st)
	go events.Listen(ctx, st, hub)

	handler := server.New(server.Config{
		Auth: &auth.Handler{
			Store:        st,
			Tokens:       tokens,
			CookieDomain: cookieDomain,
			SecureCookie: secureCookie,
		},
		Teams: &teams.Handler{
			Store:        st,
			Tokens:       tokens,
			CookieDomain: cookieDomain,
			SecureCookie: secureCookie,
		},
		Engine: &engine.Handler{
			Challenges:  kubeClient,
			Scores:      st,
			Attachments: oci.NewFetcher(nil),
			WorldSeed:   worldSeed,
			Events:      publisher,
		},
		Events: &events.Handler{Hub: hub},
		Logger: logger,
	})

	srv := &http.Server{
		Addr:              listenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	slog.Info("cyberkube listening", "addr", listenAddr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// resolveWorldSeed returns the procedural world seed: the WORLD_SEED env var
// if set, otherwise a value generated once and persisted in the settings
// table so every replica and every restart converges on the same seed.
func resolveWorldSeed(ctx context.Context, st *store.Store) (string, error) {
	if seed := os.Getenv("WORLD_SEED"); seed != "" {
		return seed, nil
	}
	generated, err := randomSeed()
	if err != nil {
		return "", fmt.Errorf("generate seed: %w", err)
	}
	seed, err := st.GetOrCreateSetting(ctx, "world_seed", generated)
	if err != nil {
		return "", fmt.Errorf("persist seed: %w", err)
	}
	return seed, nil
}

func randomSeed() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
