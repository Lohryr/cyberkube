package store

import (
	"context"
	"errors"
	"os"
	"testing"
)

// newTestStore connects to the database from TEST_DATABASE_URL, skipping the
// test when unset (unit runs stay green without a database; CI/staging sets
// the variable to exercise the real thing).
func newTestStore(t *testing.T) *Store {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	s, err := New(context.Background(), url)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

func TestUserLifecycle(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	u, err := s.CreateUser(ctx, "alice", "alice@example.com", "$2a$fakehash")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if u.ID == "" {
		t.Fatal("user has no id")
	}

	if _, err := s.CreateUser(ctx, "alice", "other@example.com", "h"); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate username: err = %v, want ErrConflict", err)
	}
	if _, err := s.CreateUser(ctx, "other", "alice@example.com", "h"); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate email: err = %v, want ErrConflict", err)
	}

	byName, err := s.GetUserByLogin(ctx, "alice")
	if err != nil || byName.ID != u.ID {
		t.Errorf("GetUserByLogin(username) = %+v, %v", byName, err)
	}
	byEmail, err := s.GetUserByLogin(ctx, "alice@example.com")
	if err != nil || byEmail.ID != u.ID {
		t.Errorf("GetUserByLogin(email) = %+v, %v", byEmail, err)
	}
	if _, err := s.GetUserByLogin(ctx, "ghost"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown login: err = %v, want ErrNotFound", err)
	}
}

func TestTeamLifecycle(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	creator, err := s.CreateUser(ctx, "bob", "bob@example.com", "h")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	team, err := s.CreateTeam(ctx, "les-tocards", "INV123", creator.ID)
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}

	// Creator is auto-joined.
	got, err := s.GetUserByID(ctx, creator.ID)
	if err != nil || got.TeamID != team.ID {
		t.Errorf("creator team = %q, want %q (err %v)", got.TeamID, team.ID, err)
	}

	if _, err := s.CreateTeam(ctx, "les-tocards", "OTHER", creator.ID); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate team name: err = %v, want ErrConflict", err)
	}

	byCode, err := s.GetTeamByInviteCode(ctx, "INV123")
	if err != nil || byCode.ID != team.ID {
		t.Errorf("GetTeamByInviteCode = %+v, %v", byCode, err)
	}

	member, err := s.CreateUser(ctx, "carol", "carol@example.com", "h")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := s.JoinTeam(ctx, member.ID, team.ID); err != nil {
		t.Fatalf("JoinTeam: %v", err)
	}
	got, err = s.GetUserByID(ctx, member.ID)
	if err != nil || got.TeamID != team.ID {
		t.Errorf("member team = %q, want %q (err %v)", got.TeamID, team.ID, err)
	}
}

func TestGetOrCreateSettingConverges(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	first, err := s.GetOrCreateSetting(ctx, "world_seed", "generated-a")
	if err != nil {
		t.Fatalf("GetOrCreateSetting: %v", err)
	}
	if first != "generated-a" {
		t.Errorf("first call = %q, want generated-a", first)
	}

	// A second replica racing with a different generated default must
	// converge on the value already persisted, not overwrite it.
	second, err := s.GetOrCreateSetting(ctx, "world_seed", "generated-b")
	if err != nil {
		t.Fatalf("GetOrCreateSetting (race): %v", err)
	}
	if second != first {
		t.Errorf("second call = %q, want %q (converge)", second, first)
	}
}
