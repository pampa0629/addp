# Repository Guidelines

## Project Structure & Module Organization
- Scope: only the `mvt/` directory.
- Backend (Go): `backend/` with `cmd/server/main.go`, `internal/`, and config in `backend/config/` (`*.yaml` and `*.yaml.example`). A small utility lives at `cmd/debug-cache/main.go` for inspecting PG cache rows.
- Frontend (Vite + JS/TS): `frontend/` with `src/`, `public/`, `package.json`, `vite.config.js`.
- Orchestration: `Makefile`, `docker-compose.yml` at repo root; logs in `logs/`; build outputs in `dist/` and `frontend/dist/`.

## Build, Test, and Development Commands
- Init: `make init` — copy `backend/config/*.example` to active files and install backend/frontend deps.
- Start infra: `make up` to run Redis; `make down` to stop; `make logs` to tail.
- Dev servers: `make dev` starts backend (8090) and frontend (5180) together.
  - Backend only: `make dev-backend` or `cd backend && go run cmd/server/main.go`.
  - Frontend only: `make dev-frontend` or `cd frontend && npm run dev`.
  - Hot reload: editing `backend/config/datasources.yaml` triggers live reload and clears cache.
- Helpers: `make redis-cli` to open a Redis shell; `make redis-flush` to purge Redis keys.
- Build: `make build` (backend → `dist/mvt-server`, frontend → `frontend/dist/`). Also available: `make build-backend`, `make build-frontend`.
- Test: `make test` or `cd backend && go test -v ./...`.
- Clean: `make clean` to remove temp/log artifacts.

## Coding Style & Naming Conventions
- Go: lowercase package names, `snake_case` filenames; exported identifiers in PascalCase.
- Formatting: run `go fmt ./...` (add before PR). Keep code inside `backend/internal/` when not exported.
- Frontend: components `ComponentName.vue`, composables `camelCase.ts`; keep styles scoped.

## Testing Guidelines
- Backend: table-driven tests colocated as `*_test.go` near the code under test.
- Aim for meaningful coverage on handlers, services, and config parsing.
- Frontend: no automation yet; document key manual flows in PRs after local validation.

## Commit & Pull Request Guidelines
- Use Conventional Commits, e.g., `feat(backend): add tile endpoint`, `fix(frontend): guard empty layer list`.
- PRs must include: scope, affected areas (backend/frontend), linked issues, screenshots for UI, and verification steps (`make init`, `make up`, `make dev`, `make test`).
- Squash WIP commits before merge to keep history clean.

## Security & Configuration Tips
- Do not commit secrets. Config lives in `backend/config/`; `make init` copies `*.example` to active files.
- Infrastructure uses Redis (via Docker). Use `make up`, `make redis-cli`, and `make redis-flush` for maintenance.

## Configuration & Runtime Behavior
- Config files:
  - App: `backend/config/app.yaml` (port, DB, Redis, CORS, cache policy, prewarm)
  - Data sources: `backend/config/datasources.yaml` (per-layer extents, pixel rules, etc.)
- Env overrides: `APP_CONFIG`, `DATASOURCES_CONFIG`, `PORT`, `FRONTEND_URL`, `DATABASE_*`, `REDIS_*`, `CACHE_TTL`, `CACHE_PERSIST_MIN_DURATION`, `CACHE_PERSIST_MIN_RAW_KB`, `PREWARM_*`.
- Endpoints:
  - Tiles: `/tiles/:datasource_id/:z/:x/:y.mvt` (gzipped MVT; sets `Content-Encoding: gzip` and `X-Cache` HIT/MISS)
  - API: `/api/datasources`, `/api/datasources/:id`, `/api/cache/clear[/:datasource_id]`, `/api/cache/stats`
  - Health: `/health`
- Gzip policy: JSON API responses are gzip-compressed via middleware; tile responses are pre-gzipped in the handler (no double gzip).
- CORS: allowed origin comes from `frontend_url` in `app.yaml`.

## Caching & Prewarm
- Multi-tier cache: in-memory LRU → Redis → Postgres. Key format `mvt:<ds>:<z>:<x>:<y>`. Redis TTL from `redis.cache_ttl`.
- Persistence policy: when generation duration ≥ `cache_policy.persist_min_duration` or raw MVT size ≥ `cache_policy.persist_min_raw_kb` (KB), the gzipped tile is also persisted into Postgres.
- Prewarm: when enabled, tiles for z=0..`prewarm.max_zoom` are generated across each datasource extent using a dedicated DB pool and fully persisted to Postgres; concurrency controlled by `prewarm.concurrency`.
- Live reload: changes to `backend/config/datasources.yaml` are detected at runtime; datasources hot-reload and all caches are cleared to avoid stale tiles.

## Utilities
- Inspect PG cache rows: `cd backend && go run cmd/debug-cache/main.go`.
