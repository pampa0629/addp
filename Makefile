.PHONY: help build build-images select-image-services test test-changed test-module test-platform test-book test-engine-startup-isolation test-integration test-online test-online-runner test-release test-release-runner test-go test-agent-frontend test-asset-frontend test-console-frontend test-copilot test-develop-frontend test-graph-frontend test-inference-frontend test-manager-frontend test-model-frontend test-quality-frontend test-meta-frontend test-monitor-frontend test-orchestrator-frontend test-portal-frontend test-service-frontend test-standard-frontend test-system-frontend test-transfer-frontend test-execution-fixtures test-authorization authorization-generate test-agent-eval test-agent-eval-release compare-agent-eval compare-agent-eval-release test-common-python test-common-python-cli-release test-system-iam-postgres test-quality-postgres test-standard-postgres test-arcgis-open-formats \
        build-iam-bootstrap build-iam-recovery \
        dev-start dev-restart dev-stop infra-up infra-down infra-restart infra-status prod-start prod-restart prod-stop prod-health ports-validate

# 默认目标
.DEFAULT_GOAL := help

# 颜色定义
RED := \033[0;31m
GREEN := \033[0;32m
YELLOW := \033[0;33m
NC := \033[0m # No Color

help: ## 显示帮助信息
	@echo "$(GREEN)全域数据平台 (ADDP) - Makefile 命令$(NC)"
	@echo ""
	@echo "$(YELLOW)可用命令:$(NC)"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  $(GREEN)%-20s$(NC) %s\n", $$1, $$2}'

# ===== 统一构建产物目录与变量 =====
# 扁平化输出目录：dist/{type}-{build}-{os}-{arch}/
OUT_DIR ?= dist
BUILD_TYPE ?= release
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
BIN_SUFFIX := $(if $(filter windows,$(GOOS)),.exe,)

# 本地 Go 构建缓存目录，避免写入系统 GOPATH 并降低权限/网络问题
LOCAL_GOMODCACHE := $(abspath .gomodcache)
LOCAL_GOPATH := $(abspath .gopath)
LOCAL_GOCACHE := $(abspath .cache/go-build)
# 优先使用本机 Go 工具链，避免自动拉取 toolchain
GOTOOLCHAIN ?= local

# 一次性 IAM CLI 使用 release 精简符号
GOFLAGS_RELEASE := -ldflags "-s -w"

# 内部函数仅供一次性 IAM CLI 使用；平台服务统一由 scripts/build/compile.sh 编译。
define build_one_service
  @if [ -d $(1)/cmd ]; then \
    name=$(2); \
    cmd_path=$(if $(3),$(3),cmd/server/main.go); \
    outdir=$(CURDIR)/$(OUT_DIR)/$(BUILD_TYPE)-$(GOOS)-$(GOARCH); \
    mkdir -p $$outdir $(LOCAL_GOCACHE); \
    echo "$(GREEN)编译 $$name ($(BUILD_TYPE)) → $$outdir/$$name$(NC)"; \
    (GOMODCACHE=$(LOCAL_GOMODCACHE) GOPATH=$(LOCAL_GOPATH) GOCACHE=$(LOCAL_GOCACHE) GOTOOLCHAIN=$(GOTOOLCHAIN) \
     cd $(1) && GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS_RELEASE) -o $$outdir/$$name$(BIN_SUFFIX) $$cmd_path 2>&1) || exit 1; \
  else \
    true; \
  fi
endef

build-iam-bootstrap: ## 构建一次性离线 IAM Bootstrap CLI
	$(call build_one_service,system/backend,addp-iam-bootstrap,cmd/iam-bootstrap/main.go)

build-iam-recovery: ## 构建离线 IAM 三员凭据恢复 CLI
	$(call build_one_service,system/backend,addp-iam-recovery,cmd/iam-recovery/main.go)

dev-start: ## 开发模式启动所有服务（按正确顺序）
	@bash scripts/dev/start.sh

dev-restart: ## 重启开发环境；参数使用 ARGS="-<模块名>"
	@bash scripts/dev/restart.sh $(ARGS)

dev-stop: ## 停止所有开发模式服务
	@bash scripts/dev/stop.sh

build: ## 编译全部正式 Go 服务与 Worker；附加参数使用 BUILD_ARGS="..."
	@bash scripts/build/compile.sh $(BUILD_ARGS)

build-images: ## 构建全部正式 ADDP 镜像；附加参数使用 IMAGE_BUILD_ARGS="..."
	@bash scripts/build/build-images.sh $(IMAGE_BUILD_ARGS)

select-image-services: ## 输出 CI 基线及当前改动影响的镜像服务列表
	@python3 scripts/ci/select-image-services.py --repository "$(CURDIR)"

.PHONY: prepare-cli-release check-cli-release
prepare-cli-release: ## 一致更新 CLI 版本事实（需 RELEASE_VERSION=X.Y.Z；不提交、不打 Tag）
	@test -n "$(RELEASE_VERSION)" || (echo "RELEASE_VERSION is required" >&2; exit 2)
	@python3 scripts/ci/update-cli-version.py --repository "$(CURDIR)" --version "$(RELEASE_VERSION)"

check-cli-release: ## 创建 Tag 前校验 CLI 版本、main HEAD 和 Platform CI（需 RELEASE_TAG=v<version>）
	@test -n "$(RELEASE_TAG)" || (echo "RELEASE_TAG is required" >&2; exit 2)
	@git fetch origin main --tags
	@python3 scripts/ci/check-release-eligibility.py --repository "$(CURDIR)" --pre-tag --tag "$(RELEASE_TAG)" --sha "$$(git rev-parse HEAD)"

# ===== 基础设施脚本入口 =====
infra-up: ## 启动系统库基础设施（带端口预检与健康检查）
	@bash scripts/infra/up.sh

infra-down: ## 停止系统库基础设施（可选 --rm） 用法：make infra-down ARGS=--rm
	@bash scripts/infra/down.sh $(ARGS)

infra-restart: ## 重启系统库基础设施（先停再启）
	@bash scripts/infra/down.sh || true
	@bash scripts/infra/up.sh

infra-status: ## 查看系统库基础设施状态与健康
	@bash scripts/infra/status.sh

ports-validate: ## 校验 System/Business 端口分配是否符合策略
	@bash scripts/utils/ports-validate.sh

test-agent-eval: ## 运行 Agent 统一离线评测门禁
	@bash scripts/test/agent-evaluation-gate.sh offline

# T5 owner 内部目标；公共入口统一由 test-release 分发。
test-agent-eval-release:
	@bash scripts/test/agent-evaluation-gate.sh release

test-common-python: ## 运行 common-python 全量测试
	@cd common-python && .venv/bin/pytest -q

test-copilot: ## 运行 Copilot 后端全量确定性测试
	@cd copilot/backend && venv/bin/python -m pytest -q tests

test-common-python-cli-release:
	@bash scripts/test/common-python-cli-release-gate.sh

test-module: ## 运行指定模块的 T0-T3 门禁；用法：make test-module MODULE=standard
	@python3 scripts/test/module-gate.py --repository "$(CURDIR)" --module "$(MODULE)"

test-changed: ## 根据相对 HEAD 或 BASE_REF 的改动运行受影响 T0-T3 门禁
	@python3 scripts/test/changed-gate.py --repository "$(CURDIR)" $(if $(BASE_REF),--base-ref "$(BASE_REF)",)

test-integration: ## 严格串行运行所有已登记的 disposable 基础设施集成门禁
	@$(MAKE) test-system-iam-postgres
	@$(MAKE) test-quality-postgres
	@$(MAKE) test-standard-postgres

test-system-iam-postgres: ## 使用一次性 PostgreSQL 数据库运行 System IAM 发布门禁
	@bash scripts/test/system-iam-postgres-gate.sh

test-quality-postgres: ## 使用一次性 PostgreSQL 数据库运行 Quality 集成门禁
	@bash scripts/test/quality-postgres-gate.sh

test-standard-postgres: ## 使用一次性 PostgreSQL 数据库运行 Standard 集成门禁
	@bash scripts/test/standard-postgres-gate.sh

test-arcgis-open-formats: ## 使用真实 Access/PGeo 样本和 Oracle Spatial 运行集成门禁
	@bash scripts/test/arcgis-open-formats-integration-gate.sh

test-execution-fixtures: ## 校验统一执行存储测试夹具
	@bash scripts/test/check-execution-test-fixtures.sh

test-online: ## 运行指定 Online suite（必须设置 ONLINE_SUITE 和 ADDP_ONLINE_TEST=1）
	@test -n "$(ONLINE_SUITE)" || (echo "ONLINE_SUITE is required" >&2; exit 2)
	@python3 scripts/test/online-gate.py --repository "$(CURDIR)" --suite "$(ONLINE_SUITE)"

test-online-runner: ## 运行 Online 分发器和预检器的确定性测试
	@python3 -m unittest scripts/test/online-gate_test.py scripts/test/online-preflight_test.py scripts/test/online-host-gate_test.py scripts/test/module-registry-recovery-online_test.py scripts/test/standard-model-reference-deletion-online_test.py scripts/ci/check-online-ci-registration_test.py
	@python3 scripts/ci/check-online-ci-registration.py --repository "$(CURDIR)"

test-release: ## 运行指定 T5 发布套件；用法：make test-release RELEASE_SUITE=common-python-cli
	@python3 scripts/test/release-gate.py --repository "$(CURDIR)" --suite "$(RELEASE_SUITE)"

test-release-runner: ## 运行 T5 分发器和 CI 登记检查的确定性测试
	@python3 -m unittest scripts/test/release-gate_test.py scripts/ci/check-release-ci-registration_test.py
	@python3 scripts/ci/check-release-ci-registration.py --repository "$(CURDIR)"

test-platform: ## 运行无外部服务依赖的平台一致性门禁
	@$(MAKE) test-book
	@bash scripts/utils/check-deps-version.sh
	@python3 scripts/ci/check-build-registration_test.py
	@python3 scripts/ci/select-image-services_test.py
	@python3 scripts/ci/check-build-registration.py --repository "$(CURDIR)"
	@python3 scripts/ci/check-frontend-ci-registration_test.py
	@python3 scripts/ci/check-frontend-ci-registration.py --repository "$(CURDIR)"
	@python3 scripts/ci/check-python-ci-registration_test.py
	@python3 scripts/ci/check-python-ci-registration.py --repository "$(CURDIR)"
	@python3 scripts/ci/check-t2-ci-registration_test.py
	@python3 scripts/ci/check-t2-ci-registration.py --repository "$(CURDIR)"
	@python3 scripts/ci/check-release-eligibility_test.py
	@$(MAKE) test-release-runner
	@python3 scripts/ci/select-module-gate_test.py
	@python3 scripts/ci/update-cli-version_test.py
	@python3 scripts/test/module-gate_test.py
	@python3 scripts/test/changed-gate_test.py
	@$(MAKE) test-engine-startup-isolation
	@$(MAKE) test-execution-fixtures
	@$(MAKE) test-online-runner
	@$(MAKE) test-authorization

test-book: ## 校验《数据治理100问》源稿目录、编号和延伸阅读链接
	@python3 docs/books/数据治理100问/tools/validate.py
	@python3 docs/books/数据治理100问/tools/book_sources_test.py
	@python3 docs/books/数据治理100问/tools/release_tools_test.py

test-engine-startup-isolation: ## 校验模块启动不依赖 Engine Instance 或可选 Engine Runtime
	@python3 scripts/ci/check-engine-startup-isolation_test.py
	@python3 scripts/ci/check-engine-startup-isolation.py --repository "$(CURDIR)"
	@python3 -m py_compile common-python/addp_common/client/runtime_registration.py engines/geopython-workflow/api_server.py engines/spark-workflow/api_server.py engines/model3d-workflow/api_server.py engines/pointcloud-workflow/api_server.py
	@cd common && go test ./client -run 'TestRegisterRuntimeEngine' -count=1
	@cd system/backend && go test ./internal/service ./internal/api -run 'Test(UpdateMetadataAndLifecycleDoesNotProbeOfflineEngine|HealthCheckerRetriesOfflineRuntimeUntilItIsReady|HealthCheckerIsolatesOfflineEngineFromOtherInstances|RegisterRuntimeEnginePreservesStableAdvertisedHost)' -count=1
	@cd engines/duckdb && go test ./cmd/server ./internal/config -count=1
	@cd inference/backend && go test ./cmd/server ./internal/config -count=1

test-model-frontend: ## 运行 Model 前端状态、交互与浏览器回归测试
	@cd model/frontend && npm test
	@cd model/frontend && npm run test:e2e
	@cd model/frontend && npm run build

test-quality-frontend: ## 运行 Quality 前端路由、浏览器与构建门禁
	@cd quality/frontend && npm run test:route
	@cd quality/frontend && npm run test:e2e
	@cd quality/frontend && npm run build

test-agent-frontend: ## 运行 Agent 前端确定性测试与构建门禁
	@cd agent/frontend && npm test
	@cd agent/frontend && npm run build

test-asset-frontend: ## 运行 Asset 前端确定性测试与构建
	@cd asset/frontend && npm test
	@cd asset/frontend && npm run build

test-console-frontend: ## 运行 Console 前端确定性测试与构建
	@cd console/frontend && npm test
	@cd console/frontend && npm run build

test-develop-frontend: ## 运行 Develop 前端确定性测试与构建
	@cd develop/frontend && npm run test:workflow
	@cd develop/frontend && npm run build

test-graph-frontend: ## 运行 Graph 前端确定性测试与构建
	@cd graph/frontend && npm test
	@cd graph/frontend && npm run build

test-inference-frontend: ## 运行 Inference 前端确定性测试与构建
	@cd inference/frontend && npm test
	@cd inference/frontend && npm run build

test-manager-frontend: ## 运行 Manager 前端确定性测试与构建
	@cd manager/frontend && npm test
	@cd manager/frontend && npm run build

test-meta-frontend: ## 运行 Meta 前端确定性测试与构建
	@cd meta/frontend && npm test
	@cd meta/frontend && npm run build

test-monitor-frontend: ## 运行 Monitor 前端确定性测试与构建
	@cd monitor/frontend && npm test
	@cd monitor/frontend && npm run build

test-orchestrator-frontend: ## 运行 Orchestrator 前端确定性测试与构建
	@cd orchestrator/frontend && npm test
	@cd orchestrator/frontend && npm run build

test-portal-frontend: ## 运行 Portal 前端确定性测试与构建
	@cd portal/frontend && npm test
	@cd portal/frontend && npm run build

test-service-frontend: ## 运行 Service 前端确定性测试与构建
	@cd service/frontend && npm test
	@cd service/frontend && npm run build

test-standard-frontend: ## 运行 Standard 前端确定性测试与构建
	@cd standard/frontend && npm test
	@cd standard/frontend && npm run build

test-system-frontend: ## 运行 System 前端确定性测试与构建
	@cd system/frontend && npm test
	@cd system/frontend && npm run build

test-transfer-frontend: ## 运行 Transfer 前端确定性测试与构建
	@cd transfer/frontend && npm test
	@cd transfer/frontend && npm run build

test-go: ## 校验依赖文件并使用临时 workspace 运行全部已跟踪 Go 模块测试
	@set -e; \
	workspace_dir="$$(mktemp -d)"; \
	trap 'rm -rf "$$workspace_dir"' EXIT; \
	workspace_file="$$workspace_dir/go.work"; \
	modules="$$(git ls-files -- 'go.mod' '**/go.mod' | sed 's#/go.mod$$##; s#^go.mod$$#.#')"; \
	summary_file="$${ADDP_CI_SUMMARY_FILE:-}"; \
	write_summary() { \
		[ -n "$$summary_file" ] || return 0; \
		{ \
			echo "### Go workspace details"; \
			echo; \
			echo "- Result: $$1"; \
			echo "- Module: $$2"; \
			echo "- Phase: $$3"; \
		} > "$$summary_file"; \
	}; \
	if [ -z "$$modules" ]; then \
		echo "$(RED)仓库中没有已跟踪的 Go 模块$(NC)" >&2; \
		write_summary failure repository module-discovery; \
		exit 1; \
	fi; \
	GOWORK="$$workspace_file" go work init $$(printf '%s\n' "$$modules" | sed "s#^#$(CURDIR)/#"); \
	for module in $$modules; do \
		echo "$(GREEN)校验 $$module go.mod/go.sum...$(NC)"; \
		if ! (cd "$$module" && GOWORK=off go mod tidy -diff); then \
			write_summary failure "$$module" go-mod-tidy; \
			exit 1; \
		fi; \
		echo "$(GREEN)运行 $$module 测试...$(NC)"; \
		if ! (cd "$$module" && GOWORK="$$workspace_file" go test ./...); then \
			write_summary failure "$$module" go-test; \
			exit 1; \
		fi; \
	done; \
	module_count="$$(printf '%s\n' "$$modules" | wc -l | tr -d ' ')"; \
	write_summary success "$$module_count modules" tidy-and-test

compare-agent-eval: ## 比较两份仓库外 Agent v2 评测报告
	@bash scripts/test/agent-evaluation-gate.sh compare

compare-agent-eval-release: ## 按正式发布基线策略比较两份 Agent v2 报告
	@bash scripts/test/agent-evaluation-gate.sh compare-release

authorization-generate: ## 从 Manifest 生成 owner-local 常量和 System Tool Catalog
	@cd common && go run ./authorization/cmd/manifest --generate-owner-constants --repository-root ..
	@cd common && go run ./authorization/cmd/manifest --generate-tool-catalog --repository-root ..

test-authorization: ## 校验 IAM Manifest、生成常量和授权覆盖报告
	@cd common && go test ./authorization/... -count=1
	@cd common && go run ./authorization/cmd/manifest --check --repository-root .. > /tmp/addp-authorization-catalog.json
	@cd common && go run ./authorization/cmd/manifest --check-owner-constants --repository-root .. > /tmp/addp-owner-constants.json
	@cd common && go run ./authorization/cmd/manifest --check-tool-catalog --repository-root .. > /tmp/addp-system-tool-catalog.json
	@cd common && go run ./authorization/cmd/manifest --check-sql-seed --repository-root .. > /tmp/addp-iam-catalog-seed.json
	@cd common && go run ./authorization/cmd/manifest --coverage-report --repository-root .. > /tmp/addp-authorization-coverage.json
	@SWAGGER_COVERAGE_WARN_ONLY=1 bash scripts/swagger/check-route-coverage.sh all

test: test-platform test-go test-common-python test-agent-eval test-copilot \
	test-agent-frontend test-asset-frontend test-console-frontend test-develop-frontend \
	test-graph-frontend test-inference-frontend test-manager-frontend test-meta-frontend \
	test-model-frontend test-monitor-frontend test-orchestrator-frontend test-portal-frontend \
	test-quality-frontend test-service-frontend test-standard-frontend test-system-frontend \
	test-transfer-frontend ## 运行全部无外部服务的确定性测试与构建门禁
	@echo "$(GREEN)全部确定性测试与构建门禁完成$(NC)"

init-minio: ## 初始化 MinIO buckets（包括 PMTiles 快显缓存等）
	@./scripts/infra/init-minio.sh

init-redis: ## 检查 Redis 缓存、事件和分布式锁
	@./scripts/infra/init-redis.sh

registry-start: ## 启动本地 Docker Registry（镜像构建必需）
	@echo "$(GREEN)启动本地 Docker Registry...$(NC)"
	@./scripts/registry/start.sh

registry-status: ## 检查本地 Docker Registry 状态
	@./scripts/registry/check.sh

# ==================== 生产环境脚本入口 ====================

prod-start: ## 启动完整生产环境
	@bash scripts/prod/start.sh

prod-restart: ## 重启完整生产环境
	@bash scripts/prod/stop.sh
	@bash scripts/prod/start.sh

prod-stop: ## 停止生产环境；参数使用 ARGS="--remove|--volumes"
	@bash scripts/prod/stop.sh $(ARGS)

prod-health: ## 检查生产环境服务健康状态
	@bash scripts/prod/health-check.sh
