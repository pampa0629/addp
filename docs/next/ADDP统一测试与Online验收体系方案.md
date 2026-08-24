# ADDP 统一测试与 Online 验收体系方案

> 状态：稳定规则已迁入正式规范；仅保留专用 T4 Runner 首次真实验收与关闭条件（2026-08-23）。

## 一、专题边界

测试分层、标准入口、数据与身份隔离、CI 编排以及 Online / Release 报告的稳定规则，统一以 [ADDP 测试与验收规范](../spec/addp测试与验收规范.md) 为准。

当前实现事实由以下入口拥有：

- 根目录 `Makefile`：公共测试命令。
- `scripts/README.md`：可用目标、参数和运行说明。
- `scripts/test/`：自动发现、suite 登记、安全预检、分发和报告。
- `.github/workflows/`：CI 触发、Runner、环境和证据归档。

本文不再复制上述规范和实现，只跟踪尚未具备硬件条件的 T4 实机事项。待完成关闭条件后删除本文，不归档第二份历史规范。

## 二、已完成基线

- [x] T0-T5 分层、根 `Makefile` 单一入口和 owner 边界已形成正式规范。
- [x] `make test-changed`、`make test-module` 与 PR T0-T3 使用同一 owner 影响计算。
- [x] `make test-integration` 严格串行编排全部已登记的 disposable 基础设施门禁。
- [x] `make test-online ONLINE_SUITE=<suite>` 统一 Run ID、预检、超时、进程锁和 `addp.online-gate/v1` 报告。
- [x] `standard-model-reference-deletion` 与 `module-registry-recovery` 已登记为真实 T4 suite，不存在占位 suite。
- [x] 专用宿主机门禁会在生命周期操作前检查 macOS、`ADDP_ONLINE_HOST=1`、仓库外环境文件与证据目录、干净 checkout、`addp_online` 数据库和 suite profile。
- [x] 手工 `.github/workflows/online-t4-gates.yml` 已绑定 `addp-online` Environment，并使用 `self-hosted`、`macOS`、`addp-online` Runner 标签。
- [x] 宿主机编排复用正式 Infra / Dev 生命周期与 `make test-online`，退出时停止应用并输出清理证据。
- [x] `make test-release RELEASE_SUITE=<suite>` 与 `addp.release-gate/v1` 已统一现有 T5 产品门禁调用和报告。

## 三、唯一剩余事项：专用 T4 Runner 首跑

### 3.1 机器与环境准备

- [ ] 准备独立 macOS Runner 账号和独立 checkout，并注册 `self-hosted`、`macOS`、`addp-online` 标签。
- [ ] 创建 `addp-online` GitHub Environment，将仓库外环境文件的绝对路径配置为 `ADDP_ONLINE_ENV_FILE` 变量。
- [ ] 环境文件基于当前部署配置准备专用测试 Tenant、最小权限 User / Service Principal、`POSTGRES_DB=addp_online` 及两个 suite 所需服务地址与凭据。
- [ ] 确认 Runner 不存在仓库根 `.env`，不连接个人开发服务、`addp`、`addp_test` 或 `addp_iam_test`。
- [ ] 确认证据目录、Docker、Go、Node、npm、Python、curl 和可用磁盘满足宿主机门禁。

### 3.2 首次真实执行

通过手工 workflow 分别运行：

1. `module-registry-recovery`。
2. `standard-model-reference-deletion`。

每次运行必须核验：

- [ ] 宿主机只读准入在任何服务停止或启动前通过。
- [ ] 被测服务健康且构建身份等于 workflow checkout。
- [ ] 测试 Tenant、AuthContext、Principal 与 Permission 满足 suite 的精确约束。
- [ ] `online-report.json` 使用 `addp.online-gate/v1`，包含阶段、构建身份、清理和零残留证据，且不含秘密。
- [ ] workflow Artifact 同时保留宿主机 readiness、gate summary、业务报告和必要日志。
- [ ] 成功、失败或中断后应用进程均停止，下一次执行不受上次残留影响。

### 3.3 进程乱序恢复证据

`module-registry-recovery` 已验证注册、租约、同 ID 恢复、多实例与 Gateway 路由收敛；仍需由专用部署编排补充真实进程启动次序证据：

- [ ] 模块先启动、System 后启动时，公共注册客户端自动恢复。
- [ ] System 与模块先启动、Gateway 后启动时，路由发现自动收敛。
- [ ] 全部服务重启后，Manager、Service 等消费方首次打开不依赖人工刷新。
- [ ] 引擎或模块恢复在线后，消费方无需重启即可动态恢复使用。

该编排必须复用正式进程入口和专用 T4 环境，不得在个人开发环境人工操作后宣称自动化通过。

## 四、关闭条件

满足以下条件后删除本文：

1. 两个已登记 T4 suite 在专用 Runner 至少各真实通过一次。
2. 构建身份、专用 Tenant、数据库隔离、失败清理和零残留证据均已核验。
3. 进程乱序恢复形成可重复的部署编排证据。
4. 根据首跑耗时与稳定性决定是否增加夜间 schedule；不需要定时执行时也必须明确记录决定。
5. `scripts/README.md`、workflow 与相关模块专题已更新为最终运行事实。

首跑日志和报告保留在 GitHub Actions Artifact 或外部证据存储中，不复制进长期规范。专题删除后，稳定规则继续由 [ADDP 测试与验收规范](../spec/addp测试与验收规范.md) 唯一维护。
