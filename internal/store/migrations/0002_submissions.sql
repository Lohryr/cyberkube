CREATE TABLE submissions (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id    UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    challenge  TEXT NOT NULL, -- Challenge CR metadata.name
    flag       TEXT NOT NULL,
    correct    BOOLEAN NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX submissions_team_challenge_idx ON submissions(team_id, challenge);

-- points are recorded at solve time: later decay never retroactively
-- changes a team's score
CREATE TABLE solves (
    team_id    UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    challenge  TEXT NOT NULL,
    points     INTEGER NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (team_id, challenge)
);

CREATE INDEX solves_challenge_idx ON solves(challenge);
