# Repository Guidelines

## Project Structure & Module Organization
- `common/` holds shared Go clients, config, and utilities.
- `system/backend` is the reference layout (`cmd/`, `internal/`, `pkg/`); `system/frontend` is the Vite UI with seeds in `system/data`.
- `manager/backend`, `meta/backend`, and `transfer/backend` mirror this structure; align new code accordingly.
- `gateway/` (edge API) and `portal/frontend` (unified shell) sit with `docs/` and `scripts/` such as `scripts/init-db.sql`; orchestration lives in the root `docker-compose.yml` and Makefile.

## Build, Test, and Development Commands
- `make init` seeds `.env` and data folders once per clone.
- `make dev-system` or `cd system/backend && go run cmd/server/main.go` starts the API; `cd system/frontend && npm install && npm run dev` serves the UI.
- `make up`, `make up-full`, and `make status` manage Docker; `make build` compiles Go binaries, and `make docker-build[-all]` builds images.
- `make test`, `make lint`, and `make fmt` run Go tests, `golangci-lint`, and gofmt repository-wide.

## Coding Style & Naming Conventions
- Keep Go code gofmt clean with lowercase packages, PascalCase exports, and snake_case file names.
- Cluster handlers, services, repositories, and models inside `internal/*`, reusing `common/` helpers rather than duplicating logic.
- Vue components in `system/frontend/src` or `portal/frontend/src` stay in `PascalCase.vue`; composables and stores use camelCase TypeScript files.
- Ship only samples such as `.env.example`; never commit live secrets or overrides.

## Testing Guidelines
- Place `_test.go` files beside the code and favour table-driven cases with the standard `testing` package, adding testify only when already used.
- Run `make test` or `go test ./...` before each push, covering handlers, services, and repositories touched by the change.
- Frontend tests are not wired yet; describe manual coverage in the PR and skip placeholder scripts until Vitest lands.

## Commit & Pull Request Guidelines
- Use Conventional Commits (`feat:`, `fix:`, `chore:`, `refactor:`) with concise lowercase scopes.
- Squash WIP commits, link issues, and list impacted services plus setup steps in the PR body; attach UI screenshots when relevant.
- Document the results of `make fmt`, `make lint`, and `make test`, and note any manual checks against Docker services.

## Configuration & Security Notes
- Copy `.env.example` to `.env`, rotate secrets locally, and keep the file out of commits.
- Leverage `make db-migrate`, `make db-shell`, `make redis-cli`, and `make minio-setup` once Docker Compose is running.
- Store backups and credentials outside the repo and sanitise any shared dumps.
