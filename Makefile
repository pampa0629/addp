.PHONY: test-raster-online-runner test-frontend-ci-registration help build build-images select-image-services local-ci test test-changed test-module test-platform test-local-ci-runner test-node-dependencies test-infra-postgresql-init test-book test-engine-plugin-registration test-engine-startup-isolation test-integration test-integration-hosted test-online test-online-runner test-release test-release-runner test-go test-agent-frontend test-asset-frontend test-catalog-frontend test-common-frontend test-console-frontend test-copilot test-document-workflow test-pointcloud-workflow test-pointcloud-native test-develop-frontend test-graph-frontend test-inference-frontend test-manager-frontend test-model-frontend test-quality-frontend test-security-frontend test-meta-frontend test-monitor-frontend test-orchestrator-frontend test-portal-frontend test-service-frontend test-standard-frontend test-system-frontend test-transfer-frontend test-workbench-frontend test-execution-fixtures test-projection-store-ownership test-authorization authorization-generate test-agent-eval test-agent-eval-release compare-agent-eval compare-agent-eval-release test-common-python test-common-python-cli-release test-common-postgres test-common-mysql-data-protection test-manager-postgres test-manager-mongodb-security test-system-iam-postgres test-asset-postgres test-meta-postgres test-catalog-postgres test-develop-postgres test-model-postgres test-quality-postgres test-security-postgres test-service-postgres test-standard-postgres test-transfer-postgres test-workbench-postgres test-arcgis-open-formats \
        build-iam-bootstrap build-iam-recovery build-iam-migration-repair \
        dev-start dev-restart dev-stop infra-up infra-down infra-restart infra-status infra-backup infra-restore-drill infra-cloud-backup test-infra-backup prod-start prod-restart prod-stop prod-health ports-validate

.PHONY: test-business-config test-common-oceanbase test-common-opengauss test-common-tidb test-common-elasticsearch test-common-elasticsearch-unit test-common-kingbase test-common-dameng test-common-oracle-decimal test-common-doris-decimal test-common-clickhouse-decimal test-opengauss-official-media-release test-kingbase-official-media-release test-dameng-official-media-release test-integration-owner-managed

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

build-iam-migration-repair: ## 构建精确状态校验的 IAM 定向迁移恢复 CLI
	$(call build_one_service,system/backend,addp-iam-migration-repair,cmd/iam-migration-repair/main.go)

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

infra-down: ## 停止系统库基础设施（保留数据卷；删除数据卷需交互确认）
	@bash scripts/infra/down.sh $(ARGS)

infra-restart: ## 重启系统库基础设施（先停再启）
	@bash scripts/infra/down.sh || true
	@bash scripts/infra/up.sh

infra-status: ## 查看系统库基础设施状态与健康
	@bash scripts/infra/status.sh

infra-backup: ## 只读备份本地 ADDP Infra；需 BACKUP_ROOT=/绝对路径
	@test -n "$(BACKUP_ROOT)" || (echo "BACKUP_ROOT is required" >&2; exit 2)
	@python3 scripts/infra/backup.py create --output-root "$(BACKUP_ROOT)" $(if $(MEILISEARCH_DUMP),--meilisearch-dump "$(MEILISEARCH_DUMP)") $(if $(MEILISEARCH_DUMP_TASK),--meilisearch-dump-task "$(MEILISEARCH_DUMP_TASK)")

infra-restore-drill: ## 在无端口、无持久卷的隔离容器中演练；需 BACKUP_DIR=/绝对路径
	@test -n "$(BACKUP_DIR)" || (echo "BACKUP_DIR is required" >&2; exit 2)
	@python3 scripts/infra/backup.py drill --backup-dir "$(BACKUP_DIR)" $(if $(MEILISEARCH_RESTORE_IMAGE),--meilisearch-restore-image "$(MEILISEARCH_RESTORE_IMAGE)") $(if $(filter 1,$(MEILISEARCH_ACCEPT_CONTENT_DRIFT)),--accept-meilisearch-content-drift)

infra-cloud-backup: ## 备份并隔离演练后加密到本地网盘监视目录；需 BACKUP_ROOT、EXPORT_ROOT、PRIVATE_ROOT
	@test -n "$(BACKUP_ROOT)" -a -n "$(EXPORT_ROOT)" -a -n "$(PRIVATE_ROOT)" || (echo "BACKUP_ROOT, EXPORT_ROOT and PRIVATE_ROOT are required" >&2; exit 2)
	@python3 scripts/infra/backup-to-netdisk.py daily --backup-root "$(BACKUP_ROOT)" --export-root "$(EXPORT_ROOT)" --private-root "$(PRIVATE_ROOT)"

ports-validate: ## 校验 System/Business 端口分配是否符合策略
	@bash scripts/utils/ports-validate.sh

test-agent-eval: ## 运行 Agent 统一离线评测门禁
	@bash scripts/test/agent-evaluation-gate.sh offline

# T5 owner 内部目标；公共入口统一由 test-release 分发。
test-agent-eval-release:
	@bash scripts/test/agent-evaluation-gate.sh release

COMMON_PYTHON ?= common-python/.venv/bin/python
test-common-python: ## 运行 common-python 全量测试
	@cd common-python && $(abspath $(COMMON_PYTHON)) -m pytest -q

POINTCLOUD_WORKFLOW_PYTHON ?= engines/pointcloud-workflow/.venv/bin/python
POINTCLOUD_NATIVE_PYTHON ?= engines/pointcloud-workflow/venv/bin/python
test-pointcloud-workflow: ## 运行 PointCloud HTTP/算子确定性测试
	@cd engines/pointcloud-workflow && $(abspath $(POINTCLOUD_WORKFLOW_PYTHON)) -m pytest -q tests

test-pointcloud-native: ## 验证独立 PDAL 原生环境与五种格式真实 COPC 转换
	@ROOT_DIR="$(CURDIR)" POINTCLOUD_NATIVE_PYTHON="$(abspath $(POINTCLOUD_NATIVE_PYTHON))" bash -c 'set -e; source scripts/dev/pointcloud-workflow.sh; prefix=$$(dirname "$$(dirname "$$POINTCLOUD_NATIVE_PYTHON")"); addp_pointcloud_native_environment "$$prefix"; "$$POINTCLOUD_NATIVE_PYTHON" engines/pointcloud-workflow/native_check.py "$$prefix"; cd engines/pointcloud-workflow; exec "$$POINTCLOUD_NATIVE_PYTHON" -m pytest -q native_tests'

DOCUMENT_WORKFLOW_PYTHON ?= engines/document-workflow/.venv/bin/python
test-document-workflow: ## 运行 Document Workflow Engine 确定性测试
	@cd engines/document-workflow && $(abspath $(DOCUMENT_WORKFLOW_PYTHON)) -m pytest -q tests

MODEL3D_WORKFLOW_PYTHON ?= engines/model3d-workflow/.venv/bin/python
.PHONY: test-supermap-workflow
test-supermap-workflow: ## 运行不加载厂商 SDK 的 SuperMap 协议、算子目录和资源主机测试
	@set -eu; work_dir=$$(mktemp -d "$${TMPDIR:-/tmp}/addp-supermap-unit.XXXXXX"); \
		trap 'rm -rf "$$work_dir"' EXIT; \
		cmake -S engines/supermap-workflow -B "$$work_dir" -DSUPERMAP_SDK_ROOT= -DBUILD_TESTING=ON; \
		cmake --build "$$work_dir" --parallel; \
		ctest --test-dir "$$work_dir" --output-on-failure

.PHONY: test-model3d-workflow
test-model3d-workflow: ## 运行 Model3D Runtime 格式边界和 GLB 发布校验测试
	@cd engines/model3d-workflow && $(abspath $(MODEL3D_WORKFLOW_PYTHON)) -m pytest -q tests

SPARK_WORKFLOW_PYTHON ?= engines/spark-workflow/venv/bin/python
.PHONY: test-spark-workflow
test-spark-workflow: ## 运行 Spark Workflow 元数据、租户身份和存储适配确定性测试
	@cd engines/spark-workflow && PYTHONPATH="$(CURDIR)/common-python" $(abspath $(SPARK_WORKFLOW_PYTHON)) -m unittest discover -s . -p 'test_*.py' -v

RASTER_MOSAIC_RUNTIME_PYTHON ?= manager/raster-mosaic-runtime/venv/bin/python
.PHONY: test-raster-mosaic-runtime
test-raster-mosaic-runtime: ## 运行 Manager 栅格镶嵌单测及真实 GDAL/COG 验证
	@ROOT_DIR="$(CURDIR)" RASTER_MOSAIC_RUNTIME_PYTHON="$(abspath $(RASTER_MOSAIC_RUNTIME_PYTHON))" bash -c 'source scripts/dev/gdal-env.sh; addp_gdal_native_environment || exit 1; export ADDP_GDAL_VERSION=$$(gdal-config --version); cd manager/raster-mosaic-runtime; exec "$$RASTER_MOSAIC_RUNTIME_PYTHON" -m unittest -v test_runtime_units test_native_gdal'

GEOPYTHON_WORKFLOW_PYTHON ?= engines/geopython-workflow/venv/bin/python
.PHONY: test-geopython-workflow
test-geopython-workflow: ## 运行 GeoPython GDAL 确定性回归测试
	@ROOT_DIR="$(CURDIR)" GEOPYTHON_WORKFLOW_PYTHON="$(abspath $(GEOPYTHON_WORKFLOW_PYTHON))" bash -c 'source scripts/dev/geopython-workflow.sh; addp_geopython_native_environment || exit 1; cd engines/geopython-workflow; PYTHONPATH="$(CURDIR)/common-python" exec "$$GEOPYTHON_WORKFLOW_PYTHON" -m pytest -q test_gdal_vector_dataset.py test_raster_compute.py test_online_raster_fixture.py test_engine.py test_multiport.py test_operator_metadata.py test_runtime_registration.py test_io_operators.py test_non_spatial_operators.py ../docs'

COPILOT_PYTHON ?= copilot/backend/venv/bin/python
test-copilot: ## 运行 Copilot 后端全量确定性测试
	@cd copilot/backend && $(abspath $(COPILOT_PYTHON)) -m pytest -q tests

test-common-python-cli-release:
	@bash scripts/test/common-python-cli-release-gate.sh

test-opengauss-official-media-release:
	@bash scripts/test/opengauss-official-media-release-gate.sh

test-kingbase-official-media-release:
	@bash scripts/test/kingbase-official-media-release-gate.sh

test-dameng-official-media-release:
	@bash scripts/test/dameng-official-media-release-gate.sh

test-module: ## 运行指定模块的 T0-T3 门禁；用法：make test-module MODULE=standard
	@python3 scripts/test/module-gate.py --repository "$(CURDIR)" --module "$(MODULE)"

test-changed: ## 根据相对 HEAD 或 BASE_REF 的改动运行受影响 T0-T3 门禁
	@python3 scripts/test/changed-gate.py --repository "$(CURDIR)" $(if $(BASE_REF),--base-ref "$(BASE_REF)",)

local-ci: ## 在专用 macOS checkout 运行辅助 T0-T3 巡检；无拉取全量用 LOCAL_CI_ARGS="--no-fetch --full"
	@bash scripts/test/local-macos-ci.sh $(LOCAL_CI_ARGS)

test-local-ci-runner: ## 运行辅助 macOS 巡检器的确定性测试
	@python3 scripts/test/local-macos-ci_test.py

test-node-dependencies: ## 校验开发生命周期不会改写已锁定的 Node 依赖
	@bash scripts/test/node-dependencies_test.sh

test-infra-postgresql-init: ## 校验本地 PostgreSQL 保留测试库及扩展的幂等初始化
	@python3 scripts/infra/init-postgresql_test.py

test-business-config: ## 校验 Business Compose 和服务管理脚本（不启动容器）
	@bash -n business/hdfs/start.sh scripts/test/common-hdfs-gate.sh
	@docker compose --env-file business/.env.example -f business/docker-compose.yml config --format json | python3 -c 'import json,sys; services=json.load(sys.stdin)["services"]; nn=services["hdfs-namenode"]; dn=services["hdfs-datanode"]; assert "@sha256:" in nn["image"] and nn["image"] == dn["image"]; assert nn["ports"][0]["host_ip"] == "127.0.0.1"; assert all(str(p["target"]) == p["published"] for p in dn["ports"]); assert nn["volumes"][1]["source"] != dn["volumes"][1]["source"]'
	@sh -n business/redis/start.sh business/redis/init.sh
	@bash -n scripts/test/business-redis-gate.sh scripts/test/common-redis-gate.sh scripts/test/redis-owned-fixture.sh scripts/test/common-redis-contract.sh
	@docker compose --env-file business/.env.example -f business/docker-compose.yml config --format json | python3 -c 'import json,sys; s=json.load(sys.stdin)["services"]["redis"]; assert s["ports"][0]["host_ip"] == "127.0.0.1"; assert "@sha256:" in s["image"]; assert "redis_data" == s["volumes"][0]["source"]'
	@bash -n business/scripts/start.sh business/scripts/stop.sh business/scripts/restart.sh business/scripts/ports.sh scripts/test/common-elasticsearch-gate.sh
	@docker compose --env-file business/.env.example -f business/docker-compose.yml config --services | grep -Fxq elasticsearch
	@docker compose --env-file business/.env.example -f business/docker-compose.yml config --format json | python3 -c 'import json,sys; s=json.load(sys.stdin)["services"]["elasticsearch"]; assert s["environment"]["xpack.security.enabled"] == "true"; assert s["ports"][0]["host_ip"] == "127.0.0.1"; assert "@sha256:" in s["image"]'
	@bash -n scripts/utils/register-business.sh scripts/infra/status.sh scripts/test/certify-infra-kafka.sh scripts/test/certify-infra-kafka-ha.sh
	@! rg -q '^BUSINESS_(PG|MINIO|ORACLE)_' .env.example
	@docker compose --env-file business/.env.example -f business/docker-compose.yml config --quiet
	@docker compose --env-file business/.env.example -f business/docker-compose.yml config --services | grep -Fxq oceanbase
	@docker compose --env-file business/.env.example -f business/docker-compose.yml config --images | grep -Fxq oceanbase/oceanbase-ce:4.4.2-lts
	@docker compose --env-file business/.env.example -f business/docker-compose.yml config --format json | python3 -c 'import json, sys; assert json.load(sys.stdin)["services"]["oceanbase"]["tmpfs"] == ["/root/ob/observer/run:mode=0755"]'
	@docker compose --env-file business/.env.example -f business/docker-compose.yml config --services | grep -Fxq tidb-pd
	@docker compose --env-file business/.env.example -f business/docker-compose.yml config --services | grep -Fxq tidb-tikv
	@docker compose --env-file business/.env.example -f business/docker-compose.yml config --services | grep -Fxq tidb
	@docker compose --env-file business/.env.example -f business/docker-compose.yml config --images | grep -Fxq pingcap/pd:v8.5.8@sha256:424e896800e42e1b7eb585b604c8daa3454110d0f0df5ab41f7c5f49164d3aef
	@docker compose --env-file business/.env.example -f business/docker-compose.yml config --images | grep -Fxq pingcap/tikv:v8.5.8@sha256:ab84580b6795868940231aa778a34b082d19e4417e6d66f755c609bebdbbfd69
	@docker compose --env-file business/.env.example -f business/docker-compose.yml config --format json | python3 -c 'import json, sys; nofile = json.load(sys.stdin)["services"]["tidb-tikv"]["ulimits"]["nofile"]; assert nofile == {"soft": 1000000, "hard": 1000000}'
	@docker compose --env-file business/.env.example -f business/docker-compose.yml config --images | grep -Fxq pingcap/tidb:v8.5.8@sha256:df168c764bf2dfdb166dc37a5c3b0e210d29d5f3ab2d33317fd0fdf7b32037f5
	@docker compose --env-file business/.env.example -f business/docker-compose.yml config --services | grep -Fxq opengauss
	@docker compose --env-file business/.env.example -f business/docker-compose.yml config --images | grep -Fxq opengauss:6.0.6
	@! docker compose --env-file /dev/null -f business/docker-compose.yml config --services | grep -Fxq kingbase
	@docker compose --env-file /dev/null -f business/docker-compose.yml --profile kingbase config --services | grep -Fxq kingbase
	@docker compose --env-file /dev/null -f business/docker-compose.yml --profile kingbase config --images | grep -Fxq addp/kingbase:v009r001c010b0004
	@docker compose --env-file /dev/null -f business/docker-compose.yml --profile kingbase config --format json | python3 -c 'import json, sys; service = json.load(sys.stdin)["services"]["kingbase"]; assert service["container_name"] == "business-kingbase"; assert service["profiles"] == ["kingbase"]; assert "platform" not in service; assert service["environment"] == {"DB_MODE": "pg", "DB_PASSWORD": "", "DB_USER": "system"}; assert service["ports"] == [{"mode": "ingress", "host_ip": "127.0.0.1", "target": 54321, "published": "5436", "protocol": "tcp"}]; assert "volumes" not in service; assert service["labels"] == {"com.addp.business-fixture": "kingbase"}'
	@! docker compose --env-file /dev/null -f business/docker-compose.yml config --services | grep -Fxq dameng
	@docker compose --env-file /dev/null -f business/docker-compose.yml --profile dameng config --services | grep -Fxq dameng
	@docker compose --env-file /dev/null -f business/docker-compose.yml --profile dameng config --images | grep -Fxq addp/dameng:dm8-20260708-arm64
	@docker compose --env-file /dev/null -f business/docker-compose.yml --profile dameng config --format json | python3 -c 'import json, sys; service = json.load(sys.stdin)["services"]["dameng"]; assert service["container_name"] == "business-dameng"; assert service["profiles"] == ["dameng"]; assert service["platform"] == "linux/arm64"; assert service["ports"] == [{"mode": "ingress", "host_ip": "127.0.0.1", "target": 5236, "published": "5236", "protocol": "tcp"}]; assert "volumes" not in service; assert service["labels"] == {"com.addp.business-fixture": "dameng"}'
	@docker compose --env-file business/.env.example -f business/docker-compose.yml config | grep -Fq '/root/boot/init.d/01-addp-business.sql'
	@test -f business/oceanbase/init.sql
	@test -f business/tidb/init.sql
	@test -f business/opengauss/init.sql
	@test -f business/kingbase/init.sql
	@test -f business/dameng/init.sql
	@grep -Fq "TABLE_NAME = 'ADDP_ENGINE_PROBE'" business/dameng/init.sql
	@grep -Fq 'TIMESTAMP '\''2026-09-13 08:00:00.000003'\''' business/dameng/init.sql
	@grep -Fq 'SET NAMES utf8mb4;' business/oceanbase/init.sql
	@grep -Fq 'CREATE TABLE IF NOT EXISTS addp_engine_probe' business/tidb/init.sql
	@grep -Fq 'docker run --rm -i --network business_business-network' business/scripts/start.sh
	@grep -Fq 'CREATE TABLE IF NOT EXISTS addp_engine_probe' business/opengauss/init.sql
	@grep -Fq 'CREATE TABLE IF NOT EXISTS addp_engine_probe' business/kingbase/init.sql
	@grep -Fq 'bash scripts/start.sh -kingbase' business/README.md
	@grep -Fq 'FROM pg_database' business/scripts/kingbase.sh
	@! grep -Fq 'sys_database' business/scripts/kingbase.sh
	@grep -Fq 'compose create "$$COMPOSE_SERVICE"' business/scripts/kingbase.sh
	@grep -Fq 'compose start "$$COMPOSE_SERVICE"' business/scripts/kingbase.sh
	@grep -Fq 'compose rm --stop --force "$$COMPOSE_SERVICE"' business/scripts/kingbase.sh
	@grep -Fq 'com.docker.compose.project' business/scripts/kingbase.sh
	@! grep -Eq '(^|[[:space:]])docker create([[:space:]]|$$)' business/scripts/kingbase.sh
	@grep -Fq 'exec bash "$$SCRIPT_DIR/kingbase.sh" start' business/scripts/start.sh
	@grep -Fq 'exec bash "$$SCRIPT_DIR/kingbase.sh" stop' business/scripts/stop.sh
	@grep -Fq 'exec bash "$$SCRIPT_DIR/dameng.sh" start' business/scripts/start.sh
	@grep -Fq 'exec bash "$$SCRIPT_DIR/dameng.sh" stop' business/scripts/stop.sh
	@grep -Fq 'd6871147cd4a04e1595d9dedf9d05245c55b17bff2ae738dded37fe568df9824' scripts/lib/dameng-official-media.sh
	@grep -Fq '2a8a4844527e901718a88b4c747d7fd44460bcb2a862956bba98146760b8423e' scripts/lib/dameng-official-media.sh
	@grep -Fq 'V009R001C010B0004' scripts/lib/kingbase-official-media.sh
	@grep -Fq '16a436608cc204349e510cb136b8fc1fcbdf6874aee7b204cdac20a3522282da' scripts/lib/kingbase-official-media.sh
	@grep -Fq '3d08f5a99f5659723c34315b71d49f783847cba19c7a0c61c2628bf9f131c8ee' scripts/lib/kingbase-official-media.sh
	@grep -Fq 'openGauss-Docker-6.0.6-x86_64.tar' scripts/lib/opengauss-official-media.sh
	@grep -Fq 'openGauss-Docker-6.0.6-aarch64.tar' scripts/lib/opengauss-official-media.sh
	@grep -Fq '00ad2206ac93cf28c7702cd624b7c59dc1a146f9dca1f81ed19eeac430416c0b' scripts/lib/opengauss-official-media.sh
	@grep -Fq 'opengauss_official_container_ready business-opengauss opengauss_gsql' business/scripts/start.sh
	@test "$$(grep -c -- '--default-character-set=utf8mb4' business/scripts/start.sh)" -ge 4
	@bash -n business/scripts/start.sh business/scripts/ports.sh business/scripts/stop.sh business/scripts/restart.sh business/scripts/kingbase.sh business/scripts/dameng.sh scripts/utils/register-business.sh scripts/lib/opengauss-official-media.sh scripts/lib/kingbase-official-media.sh scripts/lib/dameng-official-media.sh scripts/test/business-port-resolution.sh scripts/test/common-kingbase-gate.sh scripts/test/common-dameng-gate.sh scripts/test/kingbase-official-media-release-gate.sh scripts/test/dameng-official-media-release-gate.sh
	@bash scripts/test/business-port-resolution.sh
	@bash business/scripts/start.sh --help | grep -Fq -- '-oceanbase'
	@bash business/scripts/start.sh --help | grep -Fq -- '-tidb'
	@bash business/scripts/start.sh --help | grep -Fq -- '-opengauss'
	@bash business/scripts/start.sh --help | grep -Fq -- '-kingbase'
	@! bash business/scripts/start.sh -kingbase -postgres >/dev/null 2>&1
	@bash business/scripts/start.sh --help | grep -Fq -- '-dameng'
	@! bash business/scripts/start.sh -dameng -postgres >/dev/null 2>&1
	@bash business/scripts/stop.sh --help | grep -Fq -- '-oceanbase'
	@bash business/scripts/stop.sh --help | grep -Fq -- '-opengauss'
	@bash business/scripts/stop.sh --help | grep -Fq -- '-kingbase'
	@! bash business/scripts/stop.sh -kingbase -postgres >/dev/null 2>&1
	@bash business/scripts/stop.sh --help | grep -Fq -- '-dameng'
	@! bash business/scripts/stop.sh -dameng -postgres >/dev/null 2>&1
	@bash business/scripts/restart.sh --help | grep -Fq -- '-kingbase'
	@! bash business/scripts/restart.sh -kingbase -postgres >/dev/null 2>&1
	@bash business/scripts/restart.sh --help | grep -Fq -- '-dameng'
	@! bash business/scripts/restart.sh -dameng -postgres >/dev/null 2>&1
	@bash business/scripts/kingbase.sh --help | grep -Fq -- 'start|stop|status'
	@bash business/scripts/dameng.sh --help | grep -Fq -- 'start|stop|status'
	@python3 -m unittest scripts/test/dameng-official-media-release-gate_test.py
	@python3 -m unittest scripts/test/common-doris-decimal-gate_test.py

test-integration: ## 严格串行运行所有本地可执行的 disposable 基础设施集成门禁
	@$(MAKE) test-business-redis
	@$(MAKE) test-common-redis
	@$(MAKE) test-common-postgres
	@$(MAKE) test-monitor-postgres
	@$(MAKE) test-orchestrator-postgres
	@$(MAKE) test-common-mysql-data-protection
	@$(MAKE) test-model-mysql
	@$(MAKE) test-common-oceanbase
	@$(MAKE) test-common-tidb
	@$(MAKE) test-common-elasticsearch
	@$(MAKE) test-common-hdfs
	@$(MAKE) test-manager-postgres
	@$(MAKE) test-manager-mongodb-security
	@$(MAKE) test-system-iam-postgres
	@$(MAKE) test-system-runtime-log
	@$(MAKE) test-monitor-metrics
	@$(MAKE) test-asset-postgres
	@$(MAKE) test-meta-postgres
	@$(MAKE) test-catalog-postgres
	@$(MAKE) test-develop-postgres
	@$(MAKE) test-model-postgres
	@$(MAKE) test-quality-postgres
	@$(MAKE) test-security-postgres
	@$(MAKE) test-service-postgres
	@$(MAKE) test-standard-postgres
	@$(MAKE) test-ontology-postgres
	@$(MAKE) test-ontology-falkor
	@$(MAKE) test-transfer-postgres
	@$(MAKE) test-workbench-postgres

test-integration-hosted: test-integration ## 严格串行追加 hosted-only disposable 基础设施门禁
	@$(MAKE) test-common-opengauss
	@$(MAKE) test-common-oracle-decimal
	@$(MAKE) test-common-doris-decimal
	@$(MAKE) test-common-clickhouse-decimal

test-integration-owner-managed: ## 仅在具备合法凭据的 owner 受控 Linux 主机人工串行运行门禁
	@$(MAKE) test-common-kingbase

.PHONY: test-orchestrator-postgres
test-orchestrator-postgres: ## 使用隔离 PostgreSQL 验证 Orchestrator 领取、调度事务及失联收敛
	@bash scripts/test/orchestrator-postgres-gate.sh

.PHONY: test-monitor-postgres
test-monitor-postgres: ## 使用隔离 PostgreSQL 验证 Monitor 读取隔离、执行聚合及通知链路
	@bash scripts/test/monitor-postgres-gate.sh

test-common-postgres: ## 使用一次性 PostgreSQL 数据库运行 Common Engine Provider、execution store 与保护投影存储集成门禁
	@bash scripts/test/common-postgres-gate.sh

test-common-mysql-data-protection: ## 使用一次性 MySQL database 验证 Provider 与四个 Owner 的数据保护契约
	@bash scripts/test/common-mysql-data-protection-gate.sh

.PHONY: test-model-mysql
test-model-mysql: ## 使用一次性 MySQL database 验证数据库无关指标逻辑及 Provider 查询
	@bash scripts/test/model-mysql-gate.sh

test-common-oceanbase: ## 使用一次性 OceanBase database 验证 Engine Provider 契约
	@bash scripts/test/common-oceanbase-gate.sh

test-common-tidb: ## 使用一次性 TiDB database 验证 Provider、Model 指标及 Service 查询契约
	@bash scripts/test/common-tidb-gate.sh

test-common-opengauss: ## 在 Linux x86_64 hosted runner 使用一次性 openGauss database 验证 Provider 契约
	@bash scripts/test/common-opengauss-gate.sh

test-common-kingbase: ## 在 owner 受控 Linux x86_64 主机使用正规 License 人工验证 KingbaseES Provider 契约
	@bash scripts/test/common-kingbase-gate.sh

test-common-dameng: ## 在本机 Linux ARM64 Docker 使用 Business 试用实例验证 DM8 Provider 契约
	@bash scripts/test/common-dameng-gate.sh

test-common-oracle-decimal: ## 使用一次性 Oracle database 验证 Decimal Provider 契约
	@bash scripts/test/common-oracle-decimal-gate.sh

test-common-doris-decimal: ## 使用一次性 Doris database 验证 Decimal Provider 契约
	@bash scripts/test/common-doris-decimal-gate.sh

test-common-clickhouse-decimal: ## 使用一次性 ClickHouse database 验证 Decimal Provider 契约
	@bash scripts/test/common-clickhouse-decimal-gate.sh

test-manager-postgres: ## 使用测试 PostGIS 数据库运行 Manager 统一任务、几何声明与清理集成门禁
	@bash scripts/test/manager-postgres-gate.sh

test-manager-mongodb-security: ## 使用 MongoDB Outdoor/Persons 运行 Manager 数据保护集成门禁
	@bash scripts/test/manager-mongodb-security-gate.sh

test-system-iam-postgres: ## 使用一次性 PostgreSQL 数据库运行 System IAM 发布门禁
	@bash scripts/test/system-iam-postgres-gate.sh $(SYSTEM_IAM_POSTGRES_TEST_ARGS)

.PHONY: test-system-iam-runner
test-system-iam-runner: ## 验证 System IAM PostgreSQL 门禁跨进程互斥
	@python3 -m unittest scripts/test/system-iam-postgres-gate_test.py

test-asset-postgres: ## 使用一次性 PostgreSQL 数据库运行 Asset 授权履约迁移门禁
	@bash scripts/test/asset-postgres-gate.sh

test-meta-postgres: ## 使用测试 PostgreSQL 数据库运行 Meta 迁移、目录统计与字段血缘取证集成门禁
	@bash scripts/test/meta-postgres-gate.sh

test-catalog-postgres: ## 使用一次性 PostgreSQL 数据库运行 Catalog 约束集成门禁
	@bash scripts/test/catalog-postgres-gate.sh

test-develop-postgres: ## 使用测试 PostgreSQL 数据库运行 Develop schema 与目录变化集成门禁
	@bash scripts/test/develop-postgres-gate.sh

test-model-postgres: ## 使用一次性 PostgreSQL 数据库运行 Model 物化与事务集成门禁
	@bash scripts/test/model-postgres-gate.sh

.PHONY: test-quality-backend
test-quality-backend: ## 运行 Quality 后端单元与契约测试（不依赖平台 T0）
	@cd quality/backend && go test ./...
	@cd orchestrator/backend && go test ./internal/service -run 'TestQualityPlanReceivesResolvedUpstreamTargets' -count=1

test-quality-postgres: ## 使用测试 PostgreSQL 运行 Quality 及 Orchestrator 方案引用集成门禁
	@bash scripts/test/quality-postgres-gate.sh

test-security-postgres: ## 使用一次性 PostgreSQL 数据库运行 Security 集成门禁
	@bash scripts/test/security-postgres-gate.sh

test-service-postgres: ## 使用一次性 PostgreSQL 数据库运行 Service 数据保护与 Consumer Catalog 集成门禁
	@bash scripts/test/service-postgres-gate.sh

test-standard-postgres: ## 使用测试 PostgreSQL 与独立临时 MinIO 运行 Standard 集成门禁
	@bash scripts/test/standard-postgres-gate.sh

.PHONY: test-ontology-postgres
test-ontology-postgres: ## 验证 Ontology 修订、发布事务、并发与撤回
	@bash scripts/test/ontology-postgres-gate.sh

.PHONY: test-ontology-infra-config
test-ontology-infra-config: ## 验证 FalkorDB 部署、独立 Secret、持久化契约与门禁清理
	@bash -n scripts/infra/up.sh scripts/infra/down.sh scripts/infra/status.sh scripts/prod/setup-env.sh scripts/prod/wait-infra.sh scripts/test/ontology-falkor-gate.sh
	@python3 scripts/test/ontology-falkor-gate_test.py

.PHONY: test-ontology-falkor
test-ontology-falkor: test-ontology-infra-config ## 验证独占 FalkorDB 适配及 PG 投影执行/激活/清理
	@bash scripts/test/ontology-falkor-gate.sh

test-transfer-postgres: ## 使用普通 PostgreSQL 测试库运行 Transfer schema、受保护导出与非空间目标覆盖门禁（无需 PostGIS）
	@bash scripts/test/transfer-postgres-gate.sh

test-workbench-postgres: ## 使用一次性 PostgreSQL 数据库运行 Workbench Data Application 集成门禁
	@bash scripts/test/workbench-postgres-gate.sh

test-arcgis-open-formats: ## 使用真实 Access/PGeo 样本和 Oracle Spatial 运行集成门禁
	@GEOPYTHON_WORKFLOW_PYTHON="$(abspath $(GEOPYTHON_WORKFLOW_PYTHON))" bash scripts/test/arcgis-open-formats-integration-gate.sh $(ARCGIS_OPEN_FORMATS_ARGS)

test-execution-fixtures: ## 校验统一执行存储测试夹具
	@python3 scripts/test/schema-ownership-gates_test.py ExecutionFixtureGateTest
	@bash scripts/test/check-execution-test-fixtures.sh

test-projection-store-ownership: ## 校验保护投影存储 DDL 的 Common 唯一归属
	@python3 scripts/test/schema-ownership-gates_test.py ProjectionStoreGateTest
	@bash scripts/test/check-protection-projection-store-ownership.sh

test-online: ## 运行指定 Online suite（必须设置 ONLINE_SUITE 和 ADDP_ONLINE_TEST=1）
	@test -n "$(ONLINE_SUITE)" || (echo "ONLINE_SUITE is required" >&2; exit 2)
	@python3 scripts/test/online-gate.py --repository "$(CURDIR)" --suite "$(ONLINE_SUITE)"

.PHONY: test-orchestrator-online-runner
test-orchestrator-online-runner: ## 验证 Orchestrator Online 故障、权限和隔离生命周期
	@python3 -m unittest scripts/test/orchestrator-execution-online_test.py scripts/test/orchestrator-execution-faults_test.py scripts/test/online-hosted-orchestrator-gate_test.py

test-raster-online-runner: ## 验证栅格 T4 场景、物理夹具与隔离生命周期
	@python3 -m unittest scripts/test/raster-workflow-online_test.py scripts/test/online-raster-minio-fixture_test.py scripts/test/online-hosted-raster-gate_test.py

.PHONY: test-hdfs-online-runner
test-hdfs-online-runner: ## 验证 HDFS Spark T4 场景、物理夹具与隔离生命周期
	@python3 -m unittest scripts/test/hdfs-spark-consumer-flow-online_test.py scripts/test/online-hdfs-spark-fixture_test.py scripts/test/online-hosted-hdfs-gate_test.py scripts/test/online-engine-registration_test.py scripts/test/security-mysql-owner-protection-online_test.py
	@cd system/backend && GOWORK=off go test ./cmd/online-test-fixture

.PHONY: test-elasticsearch-online-runner
test-elasticsearch-online-runner: ## 验证 ES Spark T4 场景、物理夹具与隔离生命周期
	@python3 -m unittest scripts/test/elasticsearch-consumer-flow-online_test.py scripts/test/online-hosted-elasticsearch-gate_test.py scripts/test/online-elasticsearch-consumer-fixture_test.py

.PHONY: test-node-metrics-online-runner
test-node-metrics-online-runner: ## 验证节点指标 Online 身份边界、浏览器证据与 Hosted 清理
	@node --check console/frontend/e2e/online/platform-node-resources.spec.js
	@bash -n scripts/test/online-hosted-node-metrics-gate.sh
	@python3 -m unittest scripts/test/platform-node-metrics-online_test.py scripts/test/platform-node-metrics-fixture_test.py scripts/test/online-hosted-node-metrics-gate_test.py
	@cd system/backend && GOWORK=off go test ./cmd/online-test-fixture -run Metrics -count=1

test-online-runner: ## 运行 Online 分发器和预检器的确定性测试
	@$(MAKE) test-node-metrics-online-runner
	@$(MAKE) test-hdfs-online-runner
	@$(MAKE) test-raster-online-runner
	@python3 -m unittest scripts/test/redis-consumer-flow-online_test.py scripts/test/online-hosted-redis-gate_test.py scripts/test/online-redis-consumer-fixture_test.py
	@cd system/backend && GOWORK=off go test ./cmd/online-test-fixture
	@$(MAKE) test-elasticsearch-online-runner
	@$(MAKE) test-orchestrator-online-runner
	@$(MAKE) test-manager-online-runner
	@python3 -m unittest scripts/test/compose-public-origin-online_test.py scripts/test/online-hosted-public-origin-gate_test.py
	@python3 -m unittest scripts/test/ontology-revision-lifecycle-online_test.py scripts/test/online-hosted-ontology-gate_test.py
	@python3 -m unittest scripts/test/quality-dynamic-binding-online_test.py
	@python3 -m unittest scripts/test/online-gate_test.py scripts/test/online-preflight_test.py scripts/test/online-host-gate_test.py scripts/test/online-hosted-opengauss-gate_test.py scripts/test/online-hosted-metric-gate_test.py scripts/test/online-hosted-transfer-gate_test.py scripts/test/online-hosted-security-gate_test.py scripts/test/online-security-owner-fixture_test.py scripts/test/online-owner-managed-kingbase-gate_test.py scripts/test/online-engine-registration_test.py scripts/test/online-engine-fixture_test.py scripts/test/online-workbench-mysql-fixture_test.py scripts/test/online-oceanbase-consumer-fixture_test.py scripts/test/online-tidb-consumer-fixture_test.py scripts/test/online-opengauss-consumer-fixture_test.py scripts/test/online-kingbase-consumer-fixture_test.py scripts/test/online-transfer-insert-only-fixture_test.py scripts/test/online-transfer-relational-sql-etl-fixture_test.py scripts/test/online-security-transfer-fixture_test.py scripts/test/consumer-engine-recovery-online_test.py scripts/test/consumer-process-stability-online_test.py scripts/test/module-registry-recovery-online_test.py scripts/test/relational-consumer-flow-online_test.py scripts/test/transfer-insert-only-mysql-online_test.py scripts/test/transfer-relational-sql-etl-online_test.py scripts/test/metric-service-revision-lifecycle-online_test.py scripts/test/standard-model-reference-deletion-online_test.py scripts/test/enterprise-catalog-publishing-online_test.py scripts/test/workbench-service-consumption-online_test.py scripts/test/manager-hybrid-search-online_test.py scripts/test/security-transfer-protection-online_test.py scripts/test/security-plaintext-access-online_test.py scripts/test/security-mysql-owner-protection-online_test.py scripts/ci/check-online-ci-registration_test.py
	@python3 scripts/ci/check-online-ci-registration.py --repository "$(CURDIR)"

test-release: ## 运行指定 T5 发布套件；用法：make test-release RELEASE_SUITE=common-python-cli
	@python3 scripts/test/release-gate.py --repository "$(CURDIR)" --suite "$(RELEASE_SUITE)"

.PHONY: test-workflow-security
test-workflow-security: ## 使用固定版本 zizmor 审计发布工作流
	@python3 scripts/test/workflow-security-gate.py --repository "$(CURDIR)"

test-release-runner: ## 运行 T5 分发器和 CI 登记检查的确定性测试
	@python3 -m unittest scripts/test/release-gate_test.py scripts/test/opengauss-official-media-release-gate_test.py scripts/test/kingbase-official-media-release-gate_test.py scripts/test/dameng-official-media-release-gate_test.py scripts/test/workflow-security-gate_test.py scripts/ci/check-release-ci-registration_test.py
	@python3 scripts/ci/check-release-ci-registration.py --repository "$(CURDIR)"

.PHONY: test-dev-lifecycle
test-dev-lifecycle: ## 验证 Swagger 增量、增量重启、批量端口检查、构建指纹、Runtime 并发与安装锁
	@bash -n scripts/dev/model3d-workflow.sh
	@bash -n scripts/dev/document-workflow.sh
	@bash -n scripts/dev/pointcloud-workflow.sh
	@bash -n scripts/dev/spark-workflow.sh
	@bash -n scripts/dev/geopython-workflow.sh
	@bash -n scripts/dev/gdal-env.sh
	@bash -n scripts/dev/raster-mosaic-runtime.sh
	@for script in scripts/dev/restart.sh scripts/dev/start.sh scripts/dev/stop.sh scripts/dev/ports.sh scripts/dev/lifecycle-lock.sh scripts/dev/build-identity.sh scripts/dev/jupyter-env.sh scripts/infra/ports.sh scripts/infra/up.sh scripts/infra/down.sh scripts/infra/status.sh scripts/prod/setup-env.sh scripts/prod/start.sh scripts/prod/wait-infra.sh scripts/utils/observability-env.sh scripts/test/infra-port-resolution.sh scripts/swagger/gen-swagger.sh scripts/test/dev-lifecycle-and-build.sh scripts/test/system-runtime-log-gate.sh scripts/test/monitor-metrics-gate.sh; do bash -n "$$script" || exit 1; done
	@python3 -m unittest scripts/test/pointcloud-native-lifecycle_test.py scripts/test/document-native-lifecycle_test.py scripts/test/gdal-native-lifecycle_test.py scripts/test/infra-runtime-log-lifecycle_test.py scripts/test/metrics-deployment-config_test.py scripts/test/model3d-linux-images_test.py scripts/test/model3d-native-lifecycle_test.py
	@bash scripts/test/infra-port-resolution.sh
	@bash scripts/test/dev-lifecycle-and-build.sh
	@cd common && go test ./schema ./repository ./dataprotection/projectionstore
	@for module in security meta quality transfer; do (cd $$module/backend && go test -tags sqlite_load_extension ./cmd/... -run '^$$') || exit 1; done

test-infra-backup: ## 验证备份清单、隔离路径及恢复容器所有权防线
	@python3 -m unittest scripts/test/infra_backup_test.py

.PHONY: test-go-dependency-policy
test-go-dependency-policy: ## 验证 Go 依赖规约检查器并核对当前版本
	@python3 scripts/test/check-deps-version_test.py
	@bash scripts/utils/check-deps-version.sh

.PHONY: test-build-registration
test-build-registration: ## 验证产品镜像构建登记与影响范围选择
	@bash -n scripts/build/build-images.sh
	@python3 scripts/ci/check-build-registration_test.py
	@python3 scripts/ci/select-image-services_test.py
	@python3 scripts/ci/check-build-registration.py --repository "$(CURDIR)"

test-platform: ## 运行无外部服务依赖的平台一致性门禁
	@$(MAKE) test-platform-review
	@$(MAKE) test-dev-lifecycle
	@$(MAKE) test-infra-backup
	@$(MAKE) test-business-config
	@$(MAKE) test-common-frontend
	@$(MAKE) test-book
	@$(MAKE) test-local-ci-runner
	@$(MAKE) test-node-dependencies
	@$(MAKE) test-infra-postgresql-init
	@$(MAKE) test-system-iam-runner
	@python3 scripts/test/ontology-postgres-gate_test.py
	@$(MAKE) test-ontology-infra-config
	@$(MAKE) test-go-dependency-policy
	@$(MAKE) test-build-registration
	@$(MAKE) test-frontend-ci-registration
	@$(MAKE) test-python-ci-registration
	@python3 scripts/ci/check-t2-ci-registration_test.py
	@python3 scripts/ci/check-t2-ci-registration.py --repository "$(CURDIR)"
	@python3 scripts/ci/check-release-eligibility_test.py
	@$(MAKE) test-release-runner
	@$(MAKE) test-workflow-security
	@python3 scripts/ci/select-module-gate_test.py
	@python3 scripts/ci/update-cli-version_test.py
	@python3 scripts/test/module-gate_test.py
	@python3 scripts/test/changed-gate_test.py
	@$(MAKE) test-engine-plugin-registration
	@$(MAKE) test-engine-startup-isolation
	@$(MAKE) test-execution-fixtures
	@$(MAKE) test-projection-store-ownership
	@$(MAKE) test-online-runner
	@$(MAKE) test-authorization

.PHONY: test-platform-review
test-platform-review: ## 核验平台能力来源、覆盖与不可变发布快照，拒绝来源漂移
	@cd ontology/backend && GOWORK=off go test ./internal/platform -count=1

.PHONY: test-python-ci-registration
test-python-ci-registration: ## 校验 Python 模块与已登记 owner Runtime 的 Make / CI 一致性
	@python3 scripts/ci/check-python-ci-registration_test.py
	@python3 scripts/ci/check-python-ci-registration.py --repository "$(CURDIR)"

test-frontend-ci-registration: ## 校验前端 CI 登记和浏览器夹具隔离
	@python3 scripts/ci/check-frontend-ci-registration_test.py
	@python3 scripts/ci/check-frontend-ci-registration.py --repository "$(CURDIR)"

test-book: ## 校验《数据治理100问》源稿目录、编号和延伸阅读链接
	@python3 docs/books/数据治理100问/tools/validate.py
	@python3 docs/books/数据治理100问/tools/book_sources_test.py
	@python3 docs/books/数据治理100问/tools/release_tools_test.py

test-engine-plugin-registration: ## 校验内置 Engine Plugin 聚合登记的唯一性和完整性
	@python3 scripts/ci/check-engine-plugin-registration_test.py
	@python3 scripts/ci/check-engine-plugin-registration.py --repository "$(CURDIR)"

test-engine-startup-isolation: ## 校验模块启动不依赖 Engine Instance 或可选 Engine Runtime
	@python3 scripts/ci/check-engine-startup-isolation_test.py
	@python3 scripts/ci/check-engine-startup-isolation.py --repository "$(CURDIR)"
	@python3 -m py_compile common-python/addp_common/client/runtime_registration.py engines/geopython-workflow/api_server.py engines/spark-workflow/api_server.py engines/model3d-workflow/api_server.py engines/pointcloud-workflow/api_server.py engines/document-workflow/api_server.py
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

test-security-frontend: ## 运行 Security 前端确定性测试、浏览器回归与构建
	@cd security/frontend && npm test
	@cd security/frontend && npm run test:e2e
	@cd security/frontend && npm run build

test-agent-frontend: ## 运行 Agent 前端确定性测试、浏览器回归与构建门禁
	@cd agent/frontend && npm test
	@cd agent/frontend && npm run test:e2e
	@cd agent/frontend && npm run build

test-asset-frontend: ## 运行 Asset 前端确定性测试与构建
	@cd asset/frontend && npm test
	@cd asset/frontend && npm run build

test-catalog-frontend: ## 运行 Catalog 前端路由状态测试与构建
	@cd catalog/frontend && npm test
	@cd catalog/frontend && npm run build

test-console-frontend: ## 运行 Console 前端确定性测试、浏览器回归与构建
	@cd console/frontend && npm test
	@cd console/frontend && npm run test:e2e
	@cd console/frontend && npm run build

test-common-frontend: ## 运行共享前端组件、契约与唯一所有权门禁
	@npm --prefix common-frontend test

test-develop-frontend: ## 运行 Develop 前端确定性测试与构建
	@cd develop/frontend && npm run test:workflow
	@cd develop/frontend && npm run test:e2e
	@cd develop/frontend && npm run build

test-graph-frontend: ## 运行 Graph 前端确定性测试与构建
	@cd graph/frontend && npm test
	@cd graph/frontend && npm run build

test-inference-frontend: ## 运行 Inference 前端确定性测试与构建
	@cd inference/frontend && npm test
	@cd inference/frontend && npm run build

test-manager-frontend: ## 运行 Manager 前端确定性测试与构建
	@cd manager/frontend && npm test
	@cd manager/frontend && npm run test:e2e
	@cd manager/frontend && npm run build

test-meta-frontend: ## 运行 Meta 前端确定性测试与构建
	@cd meta/frontend && npm test
	@cd meta/frontend && npm run build

test-monitor-frontend: ## 运行 Monitor 前端确定性测试、诊断浏览器回归与构建
	@cd monitor/frontend && npm test
	@cd monitor/frontend && npm run test:e2e
	@cd monitor/frontend && npm run build

test-orchestrator-frontend: ## 运行 Orchestrator 前端确定性测试、编辑器与路由浏览器回归及构建
	@cd orchestrator/frontend && npm test
	@cd orchestrator/frontend && npm run test:e2e:routes
	@cd orchestrator/frontend && npm run build

test-portal-frontend: ## 运行 Portal 前端确定性测试、浏览器回归与构建
	@cd portal/frontend && npm test
	@cd portal/frontend && npm run test:e2e
	@cd portal/frontend && npm run build

test-service-frontend: ## 运行 Service 前端确定性测试与构建
	@cd service/frontend && npm test
	@cd service/frontend && npm run build

test-standard-frontend: ## 运行 Standard 前端确定性测试与构建
	@cd standard/frontend && npm test
	@cd standard/frontend && npm run test:e2e
	@cd standard/frontend && npm run build

test-system-frontend: ## 运行 System 前端确定性测试与构建
	@cd system/frontend && npm test
	@cd system/frontend && npm run test:e2e
	@cd system/frontend && npm run build

test-transfer-frontend: ## 运行 Transfer 前端确定性测试与构建
	@cd transfer/frontend && npm test
	@cd transfer/frontend && npm run build

.PHONY: test-ontology-frontend
test-ontology-frontend: ## 运行 Ontology 前端状态契约、浏览器交互与构建
	@cd ontology/frontend && npm test
	@cd ontology/frontend && npm run test:e2e
	@cd ontology/frontend && npm run build

test-workbench-frontend: ## 运行 Workbench 前端确定性测试与构建
	@cd workbench/frontend && npm test
	@cd workbench/frontend && npm run test:e2e
	@cd workbench/frontend && npm run build

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
		if [ "$$module" = common ]; then \
			if ! (cd "$$module" && CGO_ENABLED=0 GOWORK="$$workspace_file" go test ./processmetrics); then \
				write_summary failure "$$module" process-metrics-without-cgo; \
				exit 1; \
			fi; \
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
	@$(MAKE) test-swagger

.PHONY: test-swagger
test-swagger: ## 校验 Swagger 检查脚本与全模块路由覆盖
	@python3 scripts/test/swagger-route-coverage_test.py
	@bash scripts/swagger/check-route-coverage.sh all

test: test-platform test-go test-common-python test-agent-eval test-copilot \
	test-document-workflow test-pointcloud-workflow test-geopython-workflow test-raster-mosaic-runtime test-model3d-workflow test-supermap-workflow test-spark-workflow \
	test-agent-frontend test-asset-frontend test-catalog-frontend test-console-frontend test-develop-frontend \
	test-graph-frontend test-inference-frontend test-manager-frontend test-meta-frontend \
	test-model-frontend test-monitor-frontend test-orchestrator-frontend test-portal-frontend \
	test-quality-frontend test-security-frontend test-service-frontend test-standard-frontend test-system-frontend \
	test-transfer-frontend test-workbench-frontend test-ontology-frontend ## 运行全部无外部服务的确定性测试与构建门禁
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

prod-stop: ## 停止平台与 Runtime 容器，保留基础设施和数据卷
	@bash scripts/prod/stop.sh

prod-health: ## 检查生产环境服务健康状态
	@bash scripts/prod/health-check.sh

.PHONY: test-system-runtime-log
test-system-runtime-log: ## 隔离验证模块运行日志采集、授权、持久化和实例隔离
	@bash scripts/test/system-runtime-log-gate.sh

.PHONY: test-common-hdfs-unit test-common-hdfs
test-common-hdfs-unit: ## HDFS 管理根边界、WebHDFS 协议和只读能力确定性验证
	@cd common && GOWORK=off go test ./resourcetree ./engine/plugins/hdfs ./engine/plugins ./engine/plugin -count=1
	@cd develop/backend && GOWORK=off go test ./internal/service -count=1

test-common-hdfs: ## 独占 HDFS/PostGIS/MinIO 及真实 Spark Worker 读取和成果保存门禁
	@bash scripts/test/common-hdfs-gate.sh

test-common-elasticsearch-unit: ## ES 插件、通用文档预览和单层目录扫描确定性测试
	@cd common && GOWORK=off go test ./engine/plugins/elasticsearch ./resourcetree ./query -count=1
	@cd manager/backend && GOWORK=off go test ./internal/preview -count=1
	@cd meta/backend && GOWORK=off go test ./internal/scanruntime -count=1
	@cd develop/backend && GOWORK=off go test ./internal/api ./internal/service -count=1

test-common-elasticsearch: ## 独占 ES 与 Spark Worker 验证插件、预览、扫描和索引分布式读取
	@bash scripts/test/common-elasticsearch-gate.sh

.PHONY: test-business-redis
test-business-redis: ## 独占 Redis 验证 Business 认证、原生样例、幂等与重启持久化
	@bash scripts/test/business-redis-gate.sh

.PHONY: test-common-redis test-common-redis-unit
test-common-redis-unit: ## Redis 连接、键值数据集与内容读取预算与 System 加密登记确定性测试
	@cd common && GOWORK=off go test ./datatype ./engine/plugin ./resourcetree -count=1
	@cd common && GOWORK=off go test ./engine/plugins/redis -run '^Test(ConnectionValidation|AuthenticatedConnection|TLS|ConnectionRejects|Redis)' -count=1
	@cd system/backend && GOWORK=off go test ./internal/service -run '^TestRedisRegistration' -count=1
	@cd system/backend && GOWORK=off go test ./internal/api -run '^TestRedisCatalogRequests' -count=1
	@cd manager/backend && GOWORK=off go test ./internal/preview -count=1
	@cd manager/backend && GOWORK=off go test ./internal/api -run '^TestPreview(Protection|Catalog)' -count=1
	@cd meta/backend && GOWORK=off go test ./internal/scanruntime -count=1

test-common-redis: ## 独占 Redis 验证 Common、System、Meta、Manager 键值数据集消费链路
	@bash scripts/test/common-redis-gate.sh

.PHONY: test-monitor-metrics
test-monitor-metrics: ## 隔离验证 Prometheus mTLS、真实采样、限额、故障恢复和持久化
	@bash scripts/test/monitor-metrics-gate.sh

.PHONY: test-manager-online-runner
test-manager-online-runner: ## 验证 Manager 私有成果 Online 场景、物理夹具与 Hosted 生命周期
	@node --check console/frontend/e2e/online/manager-internal-artifact-lineage.spec.js
	@bash -n scripts/test/online-hosted-manager-gate.sh
	@python3 -m unittest scripts/test/manager-internal-artifact-lineage-online_test.py scripts/test/online-manager-minio-fixture_test.py scripts/test/online-hosted-manager-gate_test.py
