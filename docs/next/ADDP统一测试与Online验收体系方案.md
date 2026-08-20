# ADDP 统一测试与 Online 验收体系方案

> 状态：待实施方案。本文定义测试分层、统一入口和实施顺序，不作为已经落地的命令说明；当前可用入口仍以根目录 `Makefile`、`scripts/README.md` 和各模块文档为准。

## 一、目标

ADDP 已经存在 Go 全量测试、模块前端测试、PostgreSQL 专项门禁、跨模块 Online 验收和发布认证，但命名、依赖、数据隔离与触发方式尚未形成统一体系。

本方案的目标是：

1. 让开发者能明确回答“这次改动应该运行哪一层测试”。
2. 统一测试入口、环境开关、数据隔离、安全检查和失败报告。
3. 保持模块 owner 边界：平台统一协议，各模块维护自己的业务夹具和断言。
4. 区分“服务已经启动”和“业务已经验收”，避免把测试隐式塞入开发环境重启流程。
5. 将 Standard ↔ Model 删除屏障作为首个跨模块 Online 验收样例，再逐步推广到其他模块。

## 二、核心决策

### 2.1 测试能力平台通用，业务场景归 owner 模块

平台统一以下内容：

- 测试分层和命名。
- 根 `Makefile` 的标准入口。
- `scripts/test/` 的门禁协议与安全检查。
- disposable 数据库、专用测试 Tenant、唯一运行标识和清理要求。
- 超时、跳过、退出码、日志和报告格式。
- CI 触发层级与制品归档方式。

各模块继续负责：

- 本模块资源的创建、修改、删除夹具。
- 本模块 API、Repository、Service 和 UI 的业务断言。
- 跨模块链路中由本模块拥有的测试场景。
- 测试数据的回收与残留核验。

不建设一个了解所有业务表、代替所有 owner 清理数据的“万能测试脚本”。共享层只编排，不拥有业务事实。

### 2.2 `restart.sh` 不执行测试

`scripts/dev/restart.sh` 的职责保持为：

- 构建目标服务。
- 执行服务启动所需的迁移。
- 启动进程并等待健康检查。
- 生成 Swagger 并执行路由覆盖检查。
- 暴露可核验的构建身份，避免旧进程或旧二进制冒充本次代码。

它不自动运行单元测试、集成测试、浏览器 E2E 或 Online 验收。原因不是测试不重要，而是重启和测试具有不同的依赖、耗时、数据风险与失败语义。开发者在重启成功后，应按变更范围显式运行对应门禁。

### 2.3 只保留单一入口，不保留旁路

稳定后的测试入口由根 `Makefile` 提供；复杂准备和安全检查放入 `scripts/test/`。模块目录可以保存测试代码和配置，但不再新增与根入口平行的长期脚本。

引入新入口时，应同步删除被替代的旧入口和旧环境变量，不保留兼容别名或双轨执行。

## 三、测试分层

| 层级 | 名称 | 典型内容 | 外部依赖 | 默认触发 |
| --- | --- | --- | --- | --- |
| T0 | 静态与契约检查 | 格式、生成物一致性、授权清单、Swagger 路由覆盖、测试夹具约束 | 无运行中服务 | 每次改动、PR |
| T1 | 单元与组件测试 | Go package、纯函数、Repository mock、Vue 组件与状态逻辑 | 无，或进程内临时资源 | 每次改动、PR |
| T2 | 模块集成测试 | 真实 PostgreSQL、Redis、对象存储或模块内部多层协作 | disposable 测试基础设施 | 相关模块改动、PR |
| T3 | 前端 Smoke / E2E | 路由、权限提示、状态恢复、关键表单与浏览器交互 | 独立测试端口或专用测试环境 | 相关前端改动、PR |
| T4 | 跨模块 Online 验收 | 真实 System 认证、Gateway、两个及以上 owner 服务、补偿与最终一致性 | 已启动且构建身份匹配的 ADDP 服务 | 显式执行、夜间或手工 CI |
| T5 | 发布认证 | 真实 OS 凭据后端、安装包生命周期、HA、故障切换、在线厂商证据 | 专用认证环境 | 发布前或 Tag |

### 3.1 T0-T1：快速确定性门禁

T0-T1 必须满足：

- 不依赖开发环境中已经启动的服务。
- 不连接 `addp` 开发业务库。
- 可并行、可重复，失败后不留下外部状态。
- 默认 `go test ./...` 或模块 `npm test` 可以运行；需要 Online 环境的测试必须默认跳过。

根目录现有 `make test-go`、`make test-authorization`、`make test-execution-fixtures` 和模块前端测试属于这一层或这一层的组合入口。

### 3.2 T2：模块集成门禁

T2 用真实基础设施验证 Migration、Repository、事务、锁、SQL 方言和模块内部 Service 协作。数据库门禁必须满足：

- 数据库名包含独立的 `test` 或 `disposable` 段。
- 禁止对 `addp` 开发业务库执行 Schema 重建、TRUNCATE、批量清理或其他破坏性动作。
- 每个 CI Job 使用独占数据库；本地共享 `addp_test` 时，各场景只能清理自己拥有的 Schema 或唯一测试事实。
- 门禁脚本必须拒绝意外 Skip，避免“命令成功但测试没有运行”。

现有 `make test-system-iam-postgres` 和 `make test-quality-postgres` 是该层的参考实现。

### 3.3 T3：前端 Smoke / E2E

T3 分为两类：

- 独立端口 E2E：测试自行启动前端或 mock 后端，适合 PR 门禁。
- Online UI 验收：使用真实登录态和后端服务，只覆盖必须由浏览器验证的关键路径，归入 T4 执行。

浏览器测试不应重复验证后端已经覆盖的全部字段规则。它重点验证路由、权限反馈、状态恢复、关键交互、主题和响应式布局。

### 3.4 T4：跨模块 Online 验收

T4 验证进程内测试无法证明的真实链路，例如：

- System 签发的真实 User 或 Service Access Token。
- Gateway 与 owner 服务的真实路由和授权。
- 跨模块引用、冻结、删除、补偿和最终一致性。
- Worker、重试、超时和故障注入后的收敛。
- 前后端在真实 API 契约下的完整工作流。

T4 不是默认 `go test ./...` 的一部分，也不由 `restart.sh` 自动触发。测试代码可以默认 Skip，但对应 Online 门禁必须显式开启并拒绝 Skip。

### 3.5 T5：发布认证

T5 面向难以在普通 PR 环境稳定执行的产品级条件，包括：

- CLI wheel、pipx、Keychain 等安装与真实 OS 生命周期。
- Kafka / Connect HA、故障切换和资源预算。
- 在线 AI 厂商或外部 IdP 的新鲜证据。
- 生产镜像、部署包和发布制品的一致性。

T5 不并入日常 `make test`，由独立发布门禁或手工触发工作流执行。

## 四、目标入口

以下为目标态，尚未全部实现：

```bash
# T0-T1：仓库默认确定性门禁
make test

# 指定模块的 T0-T3 门禁
make test-module MODULE=standard

# 所有已登记的 disposable 基础设施集成门禁
make test-integration

# 指定跨模块 Online 场景；必须显式选择，不默认跑全平台
make test-online ONLINE_SUITE=standard-model-reference-deletion

# 指定发布认证场景
make test-release RELEASE_SUITE=system-iam
```

不建议提供无条件执行所有层级的 `test-all`：T4-T5 需要不同凭据、运行时、数据库和安全边界，“全部测试”没有一个可靠的统一前置条件。需要完整认证时，应由 CI workflow 显式编排多个标准入口并分别展示结果。

现有 `test-go`、`test-model-frontend`、`test-system-iam-postgres`、`test-quality-postgres` 等目标在迁移期继续作为事实入口；实施新体系时应一次性调整到目标结构并删除被替代路径，不能长期保留两套分类。

## 五、Online 验收协议

### 5.1 命名

建议统一为：

- Go 文件：`*_online_test.go` 或 `*_online_integration_test.go`。
- Go 测试：`TestOnline<场景>`。
- 门禁脚本：`scripts/test/<suite>-online-gate.sh`。
- Make 入口：`make test-online ONLINE_SUITE=<suite>`。
- 运行资源前缀：`addp_online_<suite>_<run_id>`。

### 5.2 开关与配置

统一实现后只保留以下平台级开关：

- `ADDP_ONLINE_TEST=1`：确认允许执行 Online 测试。
- `ADDP_ONLINE_TEST_TENANT_ID`：专用测试 Tenant，必须显式提供。
- `ADDP_ONLINE_TEST_RUN_ID`：可选；未提供时由门禁生成并传给全部子测试。

服务地址继续使用平台既有配置，如 `SYSTEM_URL`、`MODEL_URL`，不在测试体系中复制第二套端口表。场景需要的 Client Secret 或 User 凭据使用 owner 已定义的环境变量。

当前 Standard ↔ Model 测试使用的 `ADDP_STANDARD_MODEL_ONLINE_TEST` 和默认 Tenant 1 仅是首个实现，不作为长期通用契约。迁移到统一门禁时应删除旧变量和默认 Tenant 路径。

### 5.3 运行前检查

Online 门禁必须先检查：

1. 必需服务 `/health` 可用。
2. 服务 Build ID、Git commit 或源码指纹与当前工作区匹配；不允许对旧进程给出通过结论。
3. Tenant 明确标记为测试用途，禁止默认使用 Tenant 1。
4. 数据库和外部系统不属于生产环境。
5. 所需 User / Service Principal 权限精确存在，不通过扩大运行时身份权限来迁就测试。
6. 同一 suite 没有使用相同 Run ID 的活动任务，避免并发互相清理。

### 5.4 身份边界

- Repository / Migration 测试可直接访问 disposable 数据库，但不得借此代替 API 授权测试。
- 用户侧创建、审批、删除等操作使用专用测试 User 身份。
- Service Principal 只验证跨服务调用，并保持最小 Scope；不能为测试赋予用户侧高风险写权限。
- 浏览器 Online 验收使用专用测试 User 会话，不复用开发者个人账号作为自动化事实。
- 测试不得同时尝试 User Token、Service Token 和直接 SQL 三条兼容路径；每个断言只验证规范定义的唯一链路。

### 5.5 数据隔离与清理

Online 测试按以下优先级创建夹具：

1. 通过 owner 正式 API 在专用测试 Tenant 创建资源。
2. 仅当待测能力本身无法通过 API 建立前置状态时，使用 owner 提供的测试夹具 helper。
3. 直接 SQL 仅限 disposable 数据库中的 Repository / Migration 集成测试，不作为跨模块 Online 测试的常规夹具方式。

每个场景必须：

- 使用唯一 Run ID，所有资源名称可追踪到该运行。
- 在成功、失败、超时和中断路径执行清理。
- 检查清理错误，不能用忽略错误的 Cleanup 冒充成功。
- 在结束时查询并断言残留为零；清理失败时即使业务断言通过，门禁仍失败。
- 不在仓库中保存测试报告、Token、临时 SQL 或夹具快照。

### 5.6 超时、故障注入与重试

- HTTP Client 必须设置超时，不能依赖无界默认客户端。
- 场景必须有总超时；worker 补偿使用有截止时间的轮询，不使用固定长时间 Sleep。
- 故障注入应位于测试 Transport、测试 Provider 或明确的测试 hook，不在生产代码中保留调试分支。
- 409、权限失败和业务冲突不自动重试；仅对规范允许的临时传输失败和异步收敛执行有界重试。

### 5.7 输出与报告

门禁至少输出：

- suite、scenario、Run ID。
- 源码 Git commit 和工作区是否干净。
- 参与服务的 Build ID / 源码指纹。
- Tenant、数据库类别和服务地址的脱敏摘要。
- 各场景耗时、失败阶段、稳定错误码。
- 已创建资源数量、清理结果和残留检查。

本地日志写入操作系统临时目录。CI 可将 JUnit 或 JSON 报告作为 workflow artifact 归档，但不把运行报告提交到仓库。

## 六、Standard ↔ Model 参考场景

现有 Standard ↔ Model Online 测试已经证明以下行为：

1. Model 存在引用时，Standard 删除被阻止。
2. 引用解除后，Standard 进入删除流程。
3. 第一次 `deleted` 通知失败时，协调记录保留。
4. 后台补偿最终将 Model guard 收敛到 `deleted`。
5. 测试结束后清理临时 Domain、Entity、Guard 和协调记录。

它适合作为 T4 的首个业务场景，但统一化前还需完成两项收口：

- 从默认 Tenant 1 迁移到显式专用测试 Tenant。
- 将直接写入 `model.entities` 和直接数据库清理改为 owner API 或受控测试夹具；若必须保留直接 SQL，则该场景应迁入 disposable 数据库集成门禁，而不能继续称为通用 Online 模板。

因此，应复用它的行为断言、故障注入和最终一致性验证方式，不应复制它当前的数据环境假设。

## 七、CI 分层

### 7.1 Pull Request

PR 默认运行：

- T0-T1 全仓确定性门禁。
- 按变更路径选择相关模块的 T2。
- 相关前端模块的 T3 独立端口 Smoke / E2E。

每个 PostgreSQL Job 使用独占 PostgreSQL 15 实例和 disposable database，不连接开发环境 Infra。

### 7.2 夜间或手工 Online

T4 使用专用 ADDP 测试部署执行，可由定时或 `workflow_dispatch` 触发。失败报告必须区分：

- 环境未就绪。
- 构建身份不匹配。
- 测试数据准备失败。
- 业务断言失败。
- 补偿超时。
- 清理失败。

环境问题不能被统计成业务通过，也不能用自动 Skip 隐藏。

### 7.3 发布门禁

T5 按产品或运行时独立编排，例如 System IAM、CLI、Infra Kafka HA、Agent 在线证据。发布 workflow 只调用标准 Make 入口，不在 YAML 内重写业务测试流程。

## 八、实施顺序

### 阶段 0：文档确认

- 确认本文的分层、入口、Tenant 和身份边界。
- 明确 `make test` 的最终范围，以及现有目标的归类和删除清单。

### 阶段 1：统一 Online runner

- 在 `scripts/test/` 建立唯一 Online 门禁分发入口和 suite 登记。
- 在根 `Makefile` 增加 `test-online`，要求显式 `ONLINE_SUITE`。
- 实现健康、构建身份、Tenant、安全数据库、超时和报告检查。
- 将 Standard ↔ Model 场景迁入统一协议，删除旧环境变量和默认 Tenant 路径。
- 为该场景补充清理失败与残留检测测试。

### 阶段 2：统一模块和集成入口

- 盘点所有 Go、Python、前端和 PostgreSQL 测试入口。
- 建立 `test-module` 与 `test-integration` 的单一路线。
- 将 System IAM、Quality PostgreSQL、Model 前端等现有门禁登记到统一分类。
- 更新 `scripts/README.md` 和各 owner 模块验证说明，删除重复命令。

### 阶段 3：CI 矩阵

- 建立 PR 的 T0-T3 变更路径矩阵。
- 建立手工或夜间 T4 workflow。
- 保留产品独立的 T5 workflow，但统一调用和报告约定。
- 归档测试报告、构建身份和清理证明。

### 阶段 4：规范化

- 至少三个 owner 模块采用统一协议后，复盘稳定部分。
- 将稳定规则迁入 `docs/spec/`，本文只保留实施历史和未完成事项。
- 删除迁移期入口、变量和说明，确保只有一套测试路线。

## 九、验收标准

统一体系完成至少需要满足：

1. 任一测试都能归入 T0-T5，且 owner 明确。
2. `restart.sh` 不执行测试，测试入口也不隐式接管开发服务生命周期。
3. 破坏性数据库门禁无法连接 `addp` 开发业务库。
4. Online 测试无法使用默认 Tenant，无法对构建身份不匹配的服务给出通过结论。
5. Online 门禁能报告业务失败、环境失败和清理失败，且拒绝意外 Skip。
6. Standard ↔ Model 场景通过统一入口执行，旧变量和旧入口已删除。
7. 根 Makefile、`scripts/README.md`、CI workflow 和模块文档一致。
8. PR、Online、Release 三类 workflow 不重复实现业务测试逻辑。

## 十、当前建议

当前优先实施持续集成专题的首期平台门禁：开发者继续直接推送 `main`，GitHub Actions 在推送后、每日定时或手工触发时运行无外部服务的平台一致性检查和全仓 Go 测试。该流程不调用 `scripts/dev/restart.sh`，不接管本地开发服务，也不连接 ADDP 开发业务库。

首期使用 GitHub Hosted Runner，不要求独立测试环境。待确定性 CI 获得真实耗时和失败数据后，再推进本文“阶段 1：统一 Online runner”；届时 Online 验收必须使用专用测试部署、显式测试 Tenant 和构建身份，不能复用本地开发环境。

不建议一次性实施全部测试层级。全仓测试入口和 CI 矩阵涉及多个 owner，应该按稳定入口逐步迁移，避免用一轮大改制造新的双轨体系。
