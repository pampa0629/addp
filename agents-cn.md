# 仓库指南

## 项目结构与模块组织
核心通用资产位于 `common/` 目录，领域服务分别位于 `system/`、`manager/`、`meta/`、`transfer/`、`gateway/` 和 `portal/`。后端 Go 项目遵循 `cmd/`、`internal/`、`pkg/` 的约定；测试应与其目标代码文件放在相邻位置。各个后端模块在上述目录下通常也会有相应的前端代码目录，例如 `system/frontend`、`portal/frontend` 等。Vue 前端位于 `system/frontend` 和 `portal/frontend`，种子数据位于 `system/data`。自动化脚本放在 `scripts/`，参考资料放在 `docs/`，而诸如 `Makefile` 和 `docker-compose.yml` 这样的编排文件应放在仓库根目录。新增模块必须遵循这一结构保持一致性，并在需要时为对应的后端服务建立匹配的前端模块。

## 构建、测试与开发命令
克隆仓库后先运行 `make init` 来生成 `.env` 文件并准备数据目录。使用 `make dev-system` 或 `cd system/backend && go run cmd/server/main.go` 启动主 API；使用 `cd system/frontend && npm install && npm run dev` 启动 SPA。通过 `make up`（或 `make up-full`）拉起完整 Docker 运行栈，用 `make status` 查看容器状态，使用 `make build` 生成位于 `dist/` 下的构建产物（或 `make build-debug`），并使用 `make docker-build-all` 构建镜像。本地开发工作流优先使用 `scripts/` 中的辅助脚本。

## 代码风格与命名约定
Go 包名保持小写，文件名使用 snake_case；在提交前运行 `make fmt` 或 `go fmt ./...`。导出标识符采用 PascalCase 命名，并尽量复用 `common/` 中的工具和帮助方法。对于 Vue 项目，组件文件命名为 `ComponentName.vue`，composable 使用小驼峰命名的 `.ts` 文件，并保持样式局部化与封装。避免在单个子项目中重复实现整个仓库中已经存在的通用逻辑。

## 测试规范
推荐使用表驱动的 Go 测试，并将 `_test.go` 文件与被测代码文件放在相邻位置。在推送前执行 `make test` 或 `go test ./...`，确保所有服务测试通过。目前前端自动化测试尚未接入；完成 UI 流程的本地验证后，请在 PR 描述中记录手动测试场景。

## Commit 与 Pull Request 规范
遵循 Conventional Commits 规范，例如 `feat(meta): add scanner config` 或 `fix(system): handle nil payload`。PR 描述中应说明变更范围、列出受影响的服务、关联相关 issue，并在 UI 更新时附上截图。请汇总验证步骤——如执行了 `make fmt`、`make lint`、`make test`、Docker 运行或手动走查——以便评审者信任该变更。合并前请 squash 掉中间的 WIP commit，以保持提交历史清晰。

## 安全与配置提示
复制 `.env.example` 为 `.env`，但不要提交包含真实密钥的配置文件。定期轮换本地凭据，并在仓库外备份重要配置。在 Docker 运行的情况下，优先通过 `make db-migrate`、`make db-shell`、`make redis-cli` 和 `make minio-setup` 等命令进行基础设施相关操作。对任何可能包含敏感信息的数据集进行脱敏后再对外分享，以保护数据安全。
