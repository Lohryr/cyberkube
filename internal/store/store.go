// Package store provides PostgreSQL persistence for cyberkube.
package store

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"time"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed all:migrations
var migrationsFS embed.FS

// ErrNotFound is returned when a requested row does not exist.
var ErrNotFound = errors.New("not found")

// ErrConflict is returned when a uniqueness constraint is violated.
var ErrConflict = errors.New("already exists")

// Store wraps a pgx connection pool.
type Store struct {
	pool *pgxpool.Pool
}

// New connects to PostgreSQL and runs pending migrations. Every query on the
// pool is traced via otelpgx against the global OTel TracerProvider (a
// no-op, and therefore free, until tracing.Setup registers a real one).
func New(ctx context.Context, databaseURL string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse postgres config: %w", err)
	}
	cfg.ConnConfig.Tracer = otelpgx.NewTracer()

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	s := &Store{pool: pool}
	if err := s.migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the connection pool.
func (s *Store) Close() { s.pool.Close() }

// migrate applies embedded SQL migrations in filename order, tracking
// progress in a schema_migrations table.
func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx,
		`CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		var exists bool
		if err := s.pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)`, name).Scan(&exists); err != nil {
			return fmt.Errorf("check migration %s: %w", name, err)
		}
		if exists {
			continue
		}
		sql, err := fs.ReadFile(migrationsFS, "migrations/"+name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, name); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("record migration %s: %w", name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %s: %w", name, err)
		}
	}
	return nil
}

// User is a registered player.
type User struct {
	ID           string
	Username     string
	Email        string
	PasswordHash string
	TeamID       string // empty when the user has not joined a team
	CreatedAt    time.Time
}

// Team is a group of users competing together.
type Team struct {
	ID         string
	Name       string
	InviteCode string
	CreatedAt  time.Time
}

// CreateUser inserts a new user. Returns ErrConflict when the username or
// email is already taken.
func (s *Store) CreateUser(ctx context.Context, username, email, passwordHash string) (*User, error) {
	u := &User{Username: username, Email: email, PasswordHash: passwordHash}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO users (username, email, password_hash) VALUES ($1, $2, $3) RETURNING id, created_at`,
		username, email, passwordHash).Scan(&u.ID, &u.CreatedAt)
	if isUniqueViolation(err) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, fmt.Errorf("insert user: %w", err)
	}
	return u, nil
}

// GetUserByLogin fetches a user by username or email.
func (s *Store) GetUserByLogin(ctx context.Context, login string) (*User, error) {
	return s.scanUser(s.pool.QueryRow(ctx,
		`SELECT id, username, email, password_hash, COALESCE(team_id::text, ''), created_at
		 FROM users WHERE username = $1 OR email = $1`, login))
}

// GetUserByID fetches a user by id.
func (s *Store) GetUserByID(ctx context.Context, id string) (*User, error) {
	return s.scanUser(s.pool.QueryRow(ctx,
		`SELECT id, username, email, password_hash, COALESCE(team_id::text, ''), created_at
		 FROM users WHERE id = $1`, id))
}

func (s *Store) scanUser(row pgx.Row) (*User, error) {
	u := &User{}
	err := row.Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.TeamID, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan user: %w", err)
	}
	return u, nil
}

// CreateTeam inserts a new team and adds the creator to it.
func (s *Store) CreateTeam(ctx context.Context, name, inviteCode, creatorID string) (*Team, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	t := &Team{Name: name, InviteCode: inviteCode}
	err = tx.QueryRow(ctx,
		`INSERT INTO teams (name, invite_code) VALUES ($1, $2) RETURNING id, created_at`,
		name, inviteCode).Scan(&t.ID, &t.CreatedAt)
	if isUniqueViolation(err) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, fmt.Errorf("insert team: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE users SET team_id = $1 WHERE id = $2`, t.ID, creatorID); err != nil {
		return nil, fmt.Errorf("join creator to team: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return t, nil
}

// GetTeamByInviteCode fetches a team by its invite code.
func (s *Store) GetTeamByInviteCode(ctx context.Context, code string) (*Team, error) {
	return s.scanTeam(s.pool.QueryRow(ctx,
		`SELECT id, name, invite_code, created_at FROM teams WHERE invite_code = $1`, code))
}

// GetTeamByID fetches a team by id.
func (s *Store) GetTeamByID(ctx context.Context, id string) (*Team, error) {
	return s.scanTeam(s.pool.QueryRow(ctx,
		`SELECT id, name, invite_code, created_at FROM teams WHERE id = $1`, id))
}

func (s *Store) scanTeam(row pgx.Row) (*Team, error) {
	t := &Team{}
	err := row.Scan(&t.ID, &t.Name, &t.InviteCode, &t.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan team: %w", err)
	}
	return t, nil
}

// JoinTeam sets the user's team.
func (s *Store) JoinTeam(ctx context.Context, userID, teamID string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE users SET team_id = $1 WHERE id = $2`, teamID, userID)
	if err != nil {
		return fmt.Errorf("join team: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// NotifyEvent publishes payload on a PostgreSQL NOTIFY channel. Every
// replica's ListenEvents loop — including this one's — receives it and
// rebroadcasts to its own WebSocket clients; this is the fan-out mechanism
// that keeps /api/v1/events consistent across replicas without Redis.
func (s *Store) NotifyEvent(ctx context.Context, channel, payload string) error {
	if _, err := s.pool.Exec(ctx, `SELECT pg_notify($1, $2)`, channel, payload); err != nil {
		return fmt.Errorf("notify %s: %w", channel, err)
	}
	return nil
}

// ListenEvents blocks, invoking onNotify for every payload received on
// channel, until ctx is done or the connection is lost (in which case it
// returns an error so the caller can retry with backoff). LISTEN state is
// per-session, so this holds one dedicated pool connection for its entire
// lifetime rather than borrowing one per call.
func (s *Store) ListenEvents(ctx context.Context, channel string, onNotify func(payload string)) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire listen connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "LISTEN "+pgx.Identifier{channel}.Sanitize()); err != nil {
		return fmt.Errorf("listen %s: %w", channel, err)
	}

	for {
		notification, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return fmt.Errorf("wait for notification on %s: %w", channel, err)
		}
		onNotify(notification.Payload)
	}
}

// GetOrCreateSetting returns the persisted value for key, inserting
// defaultValue on first use. When multiple replicas race on the first call
// (e.g. all generating a random world seed at boot), the INSERT ... ON
// CONFLICT DO NOTHING plus re-read makes them converge on whichever value
// won the race, rather than each replica keeping its own.
func (s *Store) GetOrCreateSetting(ctx context.Context, key, defaultValue string) (string, error) {
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO settings (key, value) VALUES ($1, $2) ON CONFLICT (key) DO NOTHING`,
		key, defaultValue); err != nil {
		return "", fmt.Errorf("insert setting %s: %w", key, err)
	}
	var value string
	if err := s.pool.QueryRow(ctx,
		`SELECT value FROM settings WHERE key = $1`, key).Scan(&value); err != nil {
		return "", fmt.Errorf("read setting %s: %w", key, err)
	}
	return value, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
