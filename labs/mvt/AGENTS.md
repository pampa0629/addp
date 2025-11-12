# Repository Guidelines

## Project Structure & Module Organization
- Scope: only the `mvt/` directory.
- Backend (Go): `backend/` with `cmd/server/main.go`, `internal/`, and config in `backend/config/` (`*.yaml` and `*.yaml.example`).
- Frontend (Vite + JS/TS): `frontend/` with `src/`, `public/`, `package.json`, `vite.config.js`.
- Orchestration: `Makefile`, `docker-compose.yml` at repo root; logs in `logs/`; build outputs in `dist/` and `frontend/dist/`.

## Build, Test, and Development Commands
- Init: `make init` — copy default configs and install backend/frontend deps.
- Start infra: `make up` to run Redis; `make down` to stop; `make logs` to tail.
- Dev servers: `make dev` to start backend (8090) and frontend (5180) together.
  - Backend only: `make dev-backend` or `cd backend && go run cmd/server/main.go`.
  - Frontend only: `make dev-frontend` or `cd frontend && npm run dev`.
- Build: `make build` (backend to `dist/mvt-server`, frontend to `frontend/dist/`).
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
