# cyberkube

All-in-one CTF platform backend (Go): native JWT+bcrypt auth, teams, scoring,
challenge catalog, and dynamic instance orchestration against the
chall-operator CRDs. Replaces CTFd + auth-proxy + password-validator.

## Layout

| Package | Responsibility |
|---------|----------------|
| `cmd/cyberkube` | entrypoint, config, wiring |
| `internal/store` | PostgreSQL: users, teams, submissions, solves, settings (+ migrations), LISTEN/NOTIFY |
| `internal/auth` | register/login, bcrypt, JWT issue/verify, `/auth/verify` |
| `internal/teams` | team create/join (membership scopes scoring + instances) |
| `internal/k8s` | Challenge CR informer cache (reads) + dynamic-client ChallengeInstance CRUD (writes) |
| `internal/engine` | catalog, world descriptor, flag submission (static + dynamic), scoring, scoreboard |
| `internal/oci` | pull static-challenge attachments (OCI artifacts), cache by digest |
| `internal/events` | real-time event hub + WS `/api/v1/events`, PostgreSQL LISTEN/NOTIFY fan-out across replicas |
| `internal/metrics` | Prometheus collectors shared by every package (`/metrics`) |
| `internal/server` | chi router (`/api/v1` + legacy `/api` alias), structured request logging |

## API

The contractual API is `/api/v1/*`, documented in [`api/openapi.yaml`](api/openapi.yaml).
`/api/*` mounts the exact same handlers as a temporary alias for the v2->v3
frontend cutover and will be dropped once that completes.
`/healthz` and `/auth/verify` stay unprefixed (infra contracts: kubelet probes
and the NGINX Ingress `auth-url` subrequest target for challenge instances).
`/metrics` is the unauthenticated Prometheus scrape endpoint, same port.

Key routes (see the OpenAPI doc for full request/response schemas):

| Route | Notes |
|-------|-------|
| `POST /api/v1/register`, `POST /api/v1/login` | public |
| `GET /api/v1/me` | current user profile |
| `POST /api/v1/teams`, `POST /api/v1/teams/join`, `GET /api/v1/teams/mine` | team membership |
| `GET /api/v1/world` | `{seed, generatorVersion, teamMode, challenges}` — the single descriptor a client needs to render the procedural world identically to any other client |
| `GET /api/v1/challenges` | visible challenges, scored for the caller's team |
| `GET /api/v1/challenges/{name}/attachments/{attachment}` | static-challenge file download |
| `POST /api/v1/challenges/{name}/submit` | flag submission (static + dynamic) |
| `POST /api/v1/challenges/{name}/launch`, `GET /api/v1/challenges/{name}/instance` | dynamic instance lifecycle |
| `GET /api/v1/scoreboard` | team scoreboard |
| `GET /api/v1/events` | WebSocket upgrade — `scoreboard.updated`, `challenge.solved`, `instance.status` |

All routes above except register/login require the `cyberkube_token` session
cookie or a `Bearer` JWT (same token, either transport).

## Challenge cache

`internal/k8s` no longer lists Challenge CRs from the Kubernetes API per
request: it runs a `dynamicinformer` (watch-based) and serves reads
(`ListChallenges`/`GetChallenge`) from its local cache, refreshed by watch
events plus a 5-minute resync safety net. Writes (ChallengeInstance create/
delete/mark-solved) still go straight to the dynamic client. `main` calls
`StartInformer` at boot and blocks (up to `informerSyncTimeout`, 30s) until
the initial sync completes; reads made before that sync (or after a cache
failure) return `k8s.ErrCacheNotSynced` explicitly rather than silently
falling back to a live API call, which is exactly the per-request load this
cache exists to avoid.

## Real-time events

`GET /api/v1/events` is a server-push-only WebSocket feed, authenticated the
same way as the rest of `/api/v1`. Each pod keeps an in-memory `events.Hub`
of its own connected clients. Fan-out across replicas uses PostgreSQL
LISTEN/NOTIFY (no Redis): `events.Publisher.Publish` issues `NOTIFY
cyberkube_events`, and every pod — including the one that published — runs
an `events.Listen` loop that rebroadcasts every notification to its local
hub. This keeps delivery uniform regardless of which replica produced the
event, and needs no coordination beyond the existing PostgreSQL connection.

Known gap: only `challenge.solved` + `scoreboard.updated` (on solve) and
`instance.status` (on launch) are published today. Ready/expired/deleted
instance transitions driven by the operator (not by a player request) are
not yet watched and rebroadcast — the frontend should keep polling
`GET /api/v1/challenges/{name}/instance` until that is added.

## Observability

- **Logs**: `log/slog`, JSON when `LOG_FORMAT=json` (prod), text otherwise
  (dev). One line per request: `request_id`, `route` (chi pattern, not raw
  path), `method`, `status`, `duration_ms`, plus `user_id`/`team_id` when the
  request carried a valid session.
- **Metrics** (`GET /metrics`, unauthenticated): `cyberkube_http_request_duration_seconds{route,method,status}`,
  `cyberkube_logins_total{result}`, `cyberkube_registrations_total`,
  `cyberkube_submissions_total{correct}`, `cyberkube_active_ws_clients`,
  `cyberkube_challenge_cache_size`.

## Configuration (env)

| Var | Required | Default | Notes |
|-----|----------|---------|-------|
| `DATABASE_URL` | yes | — | pgx connection string |
| `JWT_SECRET` | yes | — | ≥ 32 bytes |
| `COOKIE_DOMAIN` | no | "" | e.g. `.ctf.rokhnir.dev` so instance hosts get the session cookie |
| `CHALLENGE_NAMESPACE` | no | `ctf-instances` | where Challenge/ChallengeInstance CRs live |
| `LISTEN_ADDR` | no | `:8080` | |
| `INSECURE_COOKIE` | no | `false` | set `true` for local HTTP dev |
| `KUBECONFIG` | no | in-cluster | dev fallback |
| `LOG_FORMAT` | no | text | set `json` in prod so Alloy/Loki gets structured lines |
| `WORLD_SEED` | no | generated once, persisted | pins the procedural world seed (e.g. for a reproducible staging env); otherwise generated on first boot and stored in `settings` so every replica/restart converges on the same value |

## Tests

```
go test ./...                                   # unit + fakes (no external deps)
TEST_DATABASE_URL=postgres://... go test ./internal/store  # real PostgreSQL
```
