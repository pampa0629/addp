# Repository Guidelines

## Project Structure & Module Organization
Core assets live in `common/`, while domain services reside under `system/`, `manager/`, `meta/`, `transfer/`, `gateway/`, and `portal/`. Back-end Go projects follow the `cmd/`, `internal/`, `pkg/` convention; colocate tests beside their targets. Vue frontends sit in `system/frontend` and `portal/frontend`, with seed data in `system/data`. Place automation scripts in `scripts/`, reference material under `docs/`, and keep orchestration artifacts such as `Makefile` and `docker-compose.yml` at the repository root. New modules must mirror this layout to stay consistent.

## Build, Test, and Development Commands
Run `make init` after cloning to hydrate `.env` files and prepare data directories. Start the primary API with `make dev-system` or `cd system/backend && go run cmd/server/main.go`, and launch the SPA via `cd system/frontend && npm install && npm run dev`. Use `make up` (or `make up-full`) to bring up the full Docker stack, `make status` to inspect containers, `make build` for binaries, and `make docker-build-all` to assemble images. Prefer helpers in `scripts/` for local workflows whenever possible.

## Coding Style & Naming Conventions
Keep Go packages lowercase with snake_case filenames; run `make fmt` or `go fmt ./...` before committing. Exported identifiers use PascalCase and imports should reuse helpers from `common/`. For Vue projects, store components as `ComponentName.vue`, composables as camelCase `.ts`, and keep styles encapsulated. Avoid re-implementing shared logic that already exists elsewhere in the monorepo.

## Testing Guidelines
Adopt table-driven Go tests in `_test.go` files adjacent to the code under test. Execute `make test` or `go test ./...` prior to pushing to ensure all services remain green. Frontend automation is not yet wired; document manual scenarios in PR descriptions after validating UI flows locally.

## Commit & Pull Request Guidelines
Follow Conventional Commits such as `feat(meta): add scanner config` or `fix(system): handle nil payload`. PRs should describe scope, list affected services, link related issues, and include screenshots for UI updates. Summarize verification steps—`make fmt`, `make lint`, `make test`, Docker runs, or manual walkthroughs—so reviewers can trust the change. Squash WIP commits before merge to preserve history clarity.

## Security & Configuration Tips
Copy `.env.example` to `.env` but never commit populated secrets. Rotate local credentials regularly and keep backups outside the repo. With Docker running, rely on `make db-migrate`, `make db-shell`, `make redis-cli`, and `make minio-setup` for infrastructure tasks. Sanitize any datasets before sharing to protect sensitive information.
