# Repository Guidelines

## Project Structure & Module Organization
This monorepo splits shared assets into `common/` and service-specific code under `system/`, `manager/`, `meta/`, `transfer/`, `gateway/`, and `portal/`. Backends follow the Go layout `cmd/`, `internal/`, and `pkg/`, while Vue frontends live in `system/frontend` and `portal/frontend` with seeding data in `system/data`. Place integration scripts in `scripts/`, reference docs in `docs/`, and keep orchestration artifacts—`Makefile`, `docker-compose.yml`—at the root. Mirror the existing layout when adding services, and colocate tests alongside the code they cover.

## Build, Test, and Development Commands
Run `make init` once after cloning to copy `.env.example` values and prepare data folders. Use `make dev-system` or `cd system/backend && go run cmd/server/main.go` to launch the primary API, and `cd system/frontend && npm install && npm run dev` for the UI. `make up`/`make up-full` boot the Docker stack; `make status` inspects container health. Build binaries with `make build`, produce container images using `make docker-build` or `make docker-build-all`, and rely on `make fmt`, `make lint`, and `make test` to format, lint, and validate Go code across the repo.

## Coding Style & Naming Conventions
Keep Go code gofmt-clean with lowercase package names, PascalCase exports, and snake_case file names. Group handlers, services, repositories, and models inside `internal/<domain>` and prefer importing helpers from `common/` over re-implementing. For Vue, store components as `ComponentName.vue`, while composables and Pinia stores use camelCase `.ts` files. Avoid committing generated secrets or local overrides; track only samples such as `.env.example`.

## Testing Guidelines
Write table-driven Go tests in `_test.go` files beside their targets and stick to the standard `testing` package unless testify is already in use. Execute `make test` or `go test ./...` before each push, covering any service layer you changed. Frontend tests are not yet wired; document manual scenarios exercised when touching Vue code, and avoid adding placeholder frameworks.

## Commit & Pull Request Guidelines
Follow Conventional Commits (`feat:`, `fix:`, `chore:`, `refactor:`) with concise, lowercase scopes. Each PR should describe the change, note impacted services, and link relevant issues; attach UI screenshots when altering frontends. Summarize verification steps, including `make fmt`, `make lint`, `make test`, Docker interactions, or manual walkthroughs. Squash WIP commits prior to merge to keep history clean.

## Security & Configuration Tips
Copy `.env.example` to `.env`, rotate secrets locally, and never commit populated env files. With Docker running, lean on `make db-migrate`, `make db-shell`, `make redis-cli`, and `make minio-setup` to manage infrastructure. Store backups and credentials outside the repository, and sanitize any shared datasets before uploading.
