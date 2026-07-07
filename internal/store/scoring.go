package store

import (
	"context"
	"fmt"
	"time"
)

// Submission records one flag attempt.
type Submission struct {
	ID        string
	TeamID    string
	UserID    string
	Challenge string
	Correct   bool
	CreatedAt time.Time
}

// Solve records a team's successful solve and the points awarded at that
// moment.
type Solve struct {
	TeamID    string
	Challenge string
	Points    int
	CreatedAt time.Time
}

// ScoreboardEntry is one team's total on the scoreboard.
type ScoreboardEntry struct {
	TeamID    string
	TeamName  string
	Points    int
	LastSolve *time.Time
}

// RecordSubmission stores a flag attempt.
func (s *Store) RecordSubmission(ctx context.Context, teamID, userID, challenge, flag string, correct bool) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO submissions (team_id, user_id, challenge, flag, correct) VALUES ($1, $2, $3, $4, $5)`,
		teamID, userID, challenge, flag, correct)
	if err != nil {
		return fmt.Errorf("record submission: %w", err)
	}
	return nil
}

// InsertSolve awards points to a team for a challenge exactly once. It
// returns false when the team had already solved the challenge (no points
// awarded again).
func (s *Store) InsertSolve(ctx context.Context, teamID, challenge string, points int) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`INSERT INTO solves (team_id, challenge, points) VALUES ($1, $2, $3)
		 ON CONFLICT (team_id, challenge) DO NOTHING`,
		teamID, challenge, points)
	if err != nil {
		return false, fmt.Errorf("insert solve: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// CountSolves returns how many teams have solved the challenge.
func (s *Store) CountSolves(ctx context.Context, challenge string) (int, error) {
	var n int
	if err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM solves WHERE challenge = $1`, challenge).Scan(&n); err != nil {
		return 0, fmt.Errorf("count solves: %w", err)
	}
	return n, nil
}

// TeamSolvedChallenges returns the set of challenges the team has solved.
func (s *Store) TeamSolvedChallenges(ctx context.Context, teamID string) (map[string]bool, error) {
	rows, err := s.pool.Query(ctx, `SELECT challenge FROM solves WHERE team_id = $1`, teamID)
	if err != nil {
		return nil, fmt.Errorf("team solves: %w", err)
	}
	defer rows.Close()
	solved := map[string]bool{}
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, fmt.Errorf("scan solve: %w", err)
		}
		solved[c] = true
	}
	return solved, rows.Err()
}

// Scoreboard returns teams ordered by total points (desc), ties broken by
// earliest last solve.
func (s *Store) Scoreboard(ctx context.Context) ([]ScoreboardEntry, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT t.id, t.name, COALESCE(SUM(sv.points), 0) AS points, MAX(sv.created_at) AS last_solve
		 FROM teams t
		 LEFT JOIN solves sv ON sv.team_id = t.id
		 GROUP BY t.id, t.name
		 ORDER BY points DESC, last_solve ASC NULLS LAST, t.name ASC`)
	if err != nil {
		return nil, fmt.Errorf("scoreboard: %w", err)
	}
	defer rows.Close()

	var entries []ScoreboardEntry
	for rows.Next() {
		var e ScoreboardEntry
		if err := rows.Scan(&e.TeamID, &e.TeamName, &e.Points, &e.LastSolve); err != nil {
			return nil, fmt.Errorf("scan scoreboard row: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// TeamSolves returns a team's solve history, most recent first.
func (s *Store) TeamSolves(ctx context.Context, teamID string) ([]Solve, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT team_id, challenge, points, created_at FROM solves
		 WHERE team_id = $1 ORDER BY created_at DESC`, teamID)
	if err != nil {
		return nil, fmt.Errorf("team solve history: %w", err)
	}
	defer rows.Close()

	var solves []Solve
	for rows.Next() {
		var sv Solve
		if err := rows.Scan(&sv.TeamID, &sv.Challenge, &sv.Points, &sv.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan solve row: %w", err)
		}
		solves = append(solves, sv)
	}
	return solves, rows.Err()
}
