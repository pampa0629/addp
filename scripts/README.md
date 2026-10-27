# ADDP Scripts 使用指南

本目录包含 ADDP 项目的所有脚本工具，涵盖基础设施管理、开发调试、编译构建、本地部署和生产部署等全流程。

## 目录入口

| 工作范围 | 目录指南 |
| --- | --- |
| 基础设施、端口、备份及观测 | [Infra 指南](infra/README.md) |
| 本地源码开发 | [Dev 指南](dev/README.md) |
| 编译、镜像与部署包 | [Build 指南](build/README.md) |
| 本地容器部署 | [Local 指南](local/README.md) |
| 生产部署与健康检查 | [Prod 指南](prod/README.md) |
| Registry 与通用工具 | [Registry 指南](registry/README.md)、[Utils 指南](utils/README.md) |

## 专题导航

| 阅读内容 | 专题 |
| --- | --- |
| 目录职责、辅助工具、最佳实践与贡献要求 | [脚本组织与维护](docs/脚本组织与维护.md) |
| 启动、重启、停止与源码开发排障速查 | [启动脚本使用指南](docs/启动脚本使用指南.md) |
| 模块参数、依赖并集与 Runtime 联调范围 | [模块选择启动指南](docs/模块选择启动指南.md) |
| 开发脚本、容器部署与 Swarm 脚本现状 | [开发与容器部署](docs/开发与容器部署.md) |
| 本地容器启动、状态展示与失败范围 | [Local 启动阶段](local/README.md#1-startsh---启动服务)、[状态范围](local/README.md#3-statussh---查看状态)、[超时排查](local/README.md#5-健康检查超时) |
| 生产容器状态、回环 HTTP 与验收范围 | [Prod 健康检查](prod/README.md#health-checksh)、[失败与覆盖排查](prod/README.md#问题-3-健康检查失败) |
| 按现象定位跨专题故障 | [故障排查总入口](../docs/guide/addp常见故障排查.md#按现象查找) |
| 统一构建事实、编译、镜像、推送和部署包 | [构建打包与推送](docs/构建打包与推送.md) |
| 前端镜像上下文、锁文件与共享依赖布局 | [前端镜像构建规范](docs/前端镜像构建规范.md) |
| 场景选择、日常开发、容器测试、发布与常见问题 | [工作流与常见问题](docs/工作流与常见问题.md) |
| 测试脚本目录、T0–T3、CI 登记、Agent 评测和 CLI 发布 | [测试与发布入口](docs/测试与发布入口.md) |
| Online 分发、预检、Runner 隔离、报告、日志和节点指标 | [Online编排与观测](docs/Online编排与观测.md) |
| 标准/模型引用、企业目录 ECV 矩阵与质量动态绑定 | [Online目录与治理](docs/Online目录与治理.md) |
| 模块/消费方恢复、SQL ETL、指标、Workbench 与关系引擎 | [Online引擎与消费](docs/Online引擎与消费.md) |
| Manager 内部产物、内容边界、矢量与栅格工作流、容量 | [Online成果与栅格](docs/Online成果与栅格.md) |
| Transfer 保护、原值访问、MySQL 与空间字段保护 | [Online数据保护](docs/Online数据保护.md) |

## 验证入口

- Local 状态展示、Prod 回环 HTTP 探测与公开 HTTPS、认证和业务结果分别核对，实际覆盖和失败边界按上表正文执行；未执行、失败或未覆盖项不能计为通过。
- 变更验证使用 `make test-changed`，明确单个 Owner 时使用 `make test-module MODULE=<模块>`。
- Online 与发布认证分别使用 `make test-online ONLINE_SUITE=<suite>`、`make test-release RELEASE_SUITE=<suite>`，身份、环境与覆盖边界见[统一测试规范](../docs/spec/addp测试与验收规范.md)。
- PDAL、IFC、LibreOffice 必需工具故障与恢复使用 `workflow-runtime-health-recovery` suite，复用 GitHub Hosted Ubuntu 一次性部署；本地确定性入口为 `make test-workflow-runtime-health-online-runner`，准备、断言与清理见[Runtime 健康恢复规范](../docs/spec/testing/scenarios/部署与执行恢复.md#workflow-runtime-health-recovery)。
- Infra 启停、初始化与排障直接查[Infra 专题](infra/README.md)，总入口不重复维护操作步骤。

## 相关文档

- [CLAUDE.md](../CLAUDE.md) - 项目总体架构文档
- [Makefile](../Makefile) - Make 命令封装
- [docker-compose.infra.yml](../docker-compose.infra.yml) - 基础设施配置
- [docker-compose.yml](../docker-compose.yml) - 应用服务配置
- [docs/guide/addp部署和开发步骤.md](../docs/guide/addp部署和开发步骤.md) - 部署与开发启动指南

**Version**: 0.1.14

分类蚕食容量使用独立手工入口 `make test-online ONLINE_SUITE=raster-nibble-capacity`；仅允许专属 GitHub Hosted Linux 部署，经 `online-hosted-raster-nibble-capacity-gate.sh` 完成准入、同源产品 Runtime、NFS、资源限制和零残留清理。本地确定性入口为 `make test-raster-nibble-capacity-online-runner` 与 `make test-geopython-workflow`；完整场景、资源观测和证据边界见[专项规范](../docs/spec/testing/scenarios/Manager与空间成果.md#分类蚕食容量raster-nibble-capacity)。

D8河网转矢量线独立手工入口为 `make test-online ONLINE_SUITE=raster-stream-to-vector-d8-persistence`；Hosted由专门CI会话协调运行。本地准备门禁为 `make test-raster-stream-to-vector-d8-online-runner` 和标准 `make test-geopython-workflow` 中的真实GDAL夹具，前者由 `make test-raster-online-runner` 聚合。范围及证据边界见[Manager与空间成果](../docs/spec/testing/scenarios/Manager与空间成果.md#d8-河网转矢量线独立-hosted-验收)。
