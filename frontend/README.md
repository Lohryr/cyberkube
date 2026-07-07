# cyberkube frontend

A Three.js 3D world (Vite + TypeScript, strict) where each CTF challenge is a
building you drive up to and enter — inspired by bruno-simon.com. The world is
**generated from the live challenge catalog**: nothing is hand-placed.

## How it works

- On load: authenticate (register/login), then create or join a team (scoring
  is team-scoped). See `src/ui/auth.ts`.
- `GET /api/challenges` → `World.rebuild()` regenerates the scene. Placement is
  **deterministic** (`src/world/placement.ts`, pure + unit-tested): a challenge's
  position comes from `hash(name)`, its district from `category`, its scale from
  point value. Same challenge → same spot every reload; a new challenge merged
  via GitOps just appears, no code change.
- Drive the rover with WASD; press **E** near a building to open it, **Tab** for
  the scoreboard (`src/controls.ts`, `src/main.ts`).
- Interaction panel (`src/ui/challengePanel.ts`) adapts to mode:
  - **static** → description + attachment downloads + flag submit.
  - **dynamic** → description + "Launch instance" → polls until ready → shows
    connection info + expiry + flag submit.

## Layout

| Path | Responsibility |
|------|----------------|
| `src/api.ts` | typed backend client (cookie auth, `ApiError` with status) |
| `src/world/placement.ts` | pure deterministic layout (hash → position/scale/color) |
| `src/world/world.ts` | scene graph, `rebuild(challenges)` |
| `src/world/building.ts` | building + district meshes |
| `src/controls.ts` | drivable rover + chase camera |
| `src/ui/*` | auth, team gate, challenge panel, scoreboard, HUD |

## Config

`VITE_API_BASE` — backend origin (default `http://localhost:8080`). Inlined at
build time; pass `--build-arg VITE_API_BASE=...` to the Docker image.

## Commands

```
npm install
npm run dev        # dev server (localhost:5173)
npm run build      # typecheck + production bundle → dist/
npm run test       # vitest (deterministic-placement tests)
npm run typecheck  # tsc --noEmit
```
