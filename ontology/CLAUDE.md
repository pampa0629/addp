# Ontology 模块

Ontology 管理租户领域本体的形式化语义，不接管 Standard、Model、Catalog 的事实所有权，也不读取业务数据库。总体边界见 [最小设计契约](../docs/next/ADDP%20Ontology最小设计契约.md)。

## 当前交付范围

`backend/internal/semantic` 是正式 Go 语义内核；`internal/service`、`internal/repository` 管理原生定义的 PG 修订生命周期。`cmd/server` 已装配统一 Lifecycle、管理 HTTP API 和投影 Supervisor；FalkorDB 是私有 Infra。尚无页面或 Agent Tool，不影响 Graph 的当前功能。本轮没有重启个人开发服务，不把 T1/T2 当作已完成真实身份的 T4 上线验收。

`internal/falkor` 提供唯一的 go-redis 薄适配及确定性图投影构建/全量校验；投影执行器连接首次发布/失败重建准入、Common 租约、构建前/激活前 System 授权消费和 PG 激活指针。Backend 绑定 HTTP 后异步注册，Ready 同时验证 PG、FalkorDB 非零查询预算及 System 注册，只有 Ready 才领取；退出等待在途执行与注销。

- 自有类、属性、继承和关系声明的严格校验；拒绝环、悬空引用、继承属性歧义及不一致逆关系。
- 确定修订的不可变内存快照；规范化 JSON 和 SHA-256 摘要包含租户、本体、修订、内核契约与编译器版本。它不是数据库发布成功的凭证。
- 受限 CEL 分类：布尔/字符串、等值、逻辑运算、字符串字面量有限集合成员判断；禁止宏、算术、动态类型、时间、正则和任意函数。
- 有限枚举同时校验输入值和直接比较/集合条件中的字面量，拼错的码值不能发布为永远不匹配的规则。未知状态应传 `unknown`，不将不认识的已知码值自动视为未知或 false。
- 已知、已确认缺失、未知、无效四种输入；输出 matched、not_matched、unknown、error。只有输入声明明确允许的布尔属性，才将已确认缺失映射为 false；未读取不适用该策略。
- 解释保留规则、输入状态与缺口，但仅为 `hypothetical` 试算，不认证业务事实。原始事实值不回显。内部错误不是 HTTP 错误格式，未来 API owner 须翻译及授权过滤。

关系声明目前只校验，不执行传递/逆关系的实例推理。仅支持原生定义；未来来源引用须遵守 owner 快照契约，不能加可编辑来源副本。

## PG 修订与发布

- `CreateDraft` 只接受同一本体的下一修订号；同一时刻最多一个 draft / in_review。
- `SaveDraft` 和 `Transition` 必须携带精确 version，状态沿 draft → in_review → published → withdrawn，允许审核退回草稿。错误版本不自动重试。
- `semantic.Restore` 严格核对规范化内容、摘要、内核契约和编译器版本；读取异常快照失败关闭，不隐式升级历史定义。
- 发布在同一事务内冻结修订、写审计、创建 `common.task_executions` 的 pending 构建意图，绑定修订、摘要和 generation；审计失败时全部回滚。数据库触发器保护审核/发布内容和追加式审计。
- 发布同时保存 pending 投影及激活基线。构建通过全量校验后，ready、active revision/generation、激活审计与 execution success 同事务提交；并发完成只允许基线匹配者激活。每次切换或清空指针都递增 activation_version，阻止空指针 ABA。
- 撤回与 pending 构建意图取消、当前激活指针清空同事务；不伪装取消已运行工作，也不回退旧版本。执行器在激活前再次核实撤回、授权、当前 attempt/token/租约及基线，不把 published 当作可运行。
- 单槽 Supervisor 仅在显式 Ready 时领取，使用 Common 队列、30 秒租约、5 秒续租和 25 秒执行预算。过期租约收敛失败，不重放结果不确定的图写入；失败保留原激活版本，不删除部分图。历史图清理尚未实现。
- `RebuildProjection` 必须显式指定仍为 published 的修订 version、failed generation 和当前 activation_version；旧 execution 必须终结且无租约。新 generation、新 execution、前驱关联及请求审计原子创建，同一失败批次至多一个后继，同一修订至多一个 pending/building 投影。重复/过期请求拒绝，不回放旧写入、不改原定义、不复用旧授权。ready 投影的主动替换不在当前范围。
- 修订的 generation/build_execution_id 是首次发布的不可变历史身份，不是当前投影指针。首次构建和重建共用 owner 投影记录、准入及执行路径；撤回会取消该修订尚未领取的后继投影，并阻断所有在途投影激活。
- Actor 只承载调用方已核实的租户/主体/成员/授权版本。创建投影先保存无授权的 pending 意图，唯一准入方法 `AdmitProjection` 显式接收 generation，用当前请求的 User Token 调用 System，核对该 execution 的请求主体及全部范围后原子附加授权；领取必须要求授权。重建可由当前已获发布权限的用户请求，不借用原发布者身份。签发或附加失败只关闭目标 generation 仍未授权的 pending 意图，撤回优先于迟到授权。不保存或交给 Worker 用户 Token，不得把内部服务直接暴露为未鉴权 API。
- `repository.Migrate` 使用 `common/schema.Migrate` 协调不可变的 `001_revisions.sql`、`002_projection_runtime.sql` 与向前的 `003_projection_rebuild.sql`，重复同版本不执行。旧版本发布记录没有激活基线，不猜测补写投影，也不由新执行器领取。生产 Ontology 只用 `schema.Require` 检查 common，不创建共享执行表。

## 代码与测试

### FalkorDB 适配边界

- `scripts/infra/falkordb.yml` 是 Infra Compose 与 T2 共用的数据库定义。常驻服务使用独立密码与卷，仅开放回环 `16479`；T2 使用随机密码/端口及退出必删的独占卷，并覆盖图快照在容器重建后的恢复。数据库启动就绪不等于本体投影 ready。

- 每条命令使用独占连接、无自动重试、RESP2 标量结果；取消关闭本请求连接并拒绝迟到结果，不承诺服务端立即硬中止。最多 4 条本地在途命令，满额立即拒绝。
- 查询必须携带 1–2000 ms 服务端预算，且不超过调用方剩余 deadline。服务端必须同时启用非零 `TIMEOUT_MAX/TIMEOUT_DEFAULT`；写超时回滚可能额外耗时。取消/网络错误后的写入结果不确定，禁止原地自动重放。
- `Plan` 只从已校验的不可变快照和 owner generation 派生物理 graph key。图内只保存类型、属性、关系、规则的成员身份/名称及结构引用；CEL、规则依据与业务事实不复制。传递/逆关系实例推理未实现。
- `Build` 原子保留空 generation，拒绝覆盖已有图；分阶段写入后由 `Verify` 比较摘要、完整成员/边集合及总数。失败可能留下部分图，不能据此激活；清理必须由后续 PG owner 核实引用与 lease 后协调，不在取消路径直接删除仍可能写入的图。
- 适配不是授权器。执行器在构建前核实授权和租约，在激活前再次核实取消、撤回、授权、lease/fencing 和激活基线。网络授权调用不能置于 PG 行锁事务中；owner 统一按本体头、修订、投影、execution 加锁，最后终态写入再次核实数据库时间。当前不提供任意 Cypher API，也没有跳过准入直接图写入的旁路。

System 的 `ontology` 内部执行授权固定绑定 semantic_projection、修订、摘要与 generation；`addp-ontology` 的 Tenant Runtime 只有 `system.execution_authorization.execute`，Platform Runtime 只有自身模块注册权限。消费接口验证当前 running attempt/token 和 User 授权版本，不能消费引擎连接。System 启动需要独立 `ONTOLOGY_SERVICE_CLIENT_SECRET`。

### 管理 API 与部署

- 唯一前缀 `/api/v1/ontology`，Backend 端口 `8195`，Swagger `/swagger/index.html`。路由和 DTO 详见 `backend/docs/swagger.json` 及设计契约 10.1。
- `ontology.revision.read` 读取本体头、确定修订、确定投影；`ontology.revision.update` 创建/保存/提交/退回；`ontology.revision.publish` 发布/撤回，发布与重建另需 `system.execution_authorization.create`。全部由 Tenant 自定义角色显式分配，不自动扩张既有角色。
- 只接受当前 Tenant 的 User Token。拒绝 Service、Delegated、资源票据以及非 Tenant 作用域的 Permission 候选；身份来自 AuthContext，请求体不能提交 scope、tenant_id 或 Actor。
- 发布/重建成功为 202，仅代表已准入。准入失败为 502 `projection_admission_failed`，响应 `intent` 保留已提交 revision/version/generation/execution_id，不能盲重发发布。重新读取确认后显式重建 failed generation；进程中断留下的未准入 pending 不自动补签，可撤回修订。
- `revision.initial_generation/initial_execution_id` 是首次发布身份；当前激活事实只从本体头读取，具体投影从 generation 读取。不回显执行授权、租约 token、图 key 或底层错误。
- 修订下 `GET /projection` 沿重建前驱链读取最新尝试，响应丢失时可找回身份；它不表示 active，没有投影返回 404，不依赖墙钟排序。
- `INFRA_FALKORDB_ADDRESS` 宿主默认 `127.0.0.1:16479`，容器为 `falkordb:6379`；只读取独立 `INFRA_FALKORDB_PASSWORD`。构建、Compose、`start.sh -ontology`、`restart.sh -ontology` 已登记，无前端占位入口。
- 若本机已有忽略提交的 `go.work`，使用 `go work use ./ontology/backend` 纳入模块后再生成 Swagger/开发构建；T1 同时校验生成文档中的原生定义字段，拒绝只保留路由但请求类型变成空对象的假覆盖。
- System 向前迁移 `000153` 登记 read/update 与 Platform Runtime，既有角色绑定触发器推进授权版本；不修改已发布迁移。Ontology owner 仍使用 schema v3。

### 标准门禁

后端模块：`github.com/addp/ontology`，Go 1.24.2。规则上限是版本化内核契约的一部分，不通过用户选项关闭；修改上限或表达式语义须升级契约版本。

v1 边界：64 类、256 属性、128 关系、64 规则；单类 8 个直接父类、继承深度 32；单规则 16 个输入、4096 字节表达式、256 个 AST 节点、深度 32、运行成本 256；字符串值最多 4096 字节，枚举/字面量集合最多 64 项，规范化前定义 JSON 最多 1 MiB。空属性枚举表示不限定字符串取值；字面量成员集合不允许空集合或重复值。属性不得覆盖继承链上的同名 key，菱形继承同一个属性不算冲突。v1 仅接受两端为同一类的传递关系声明；逆关系须双向显式引用，端点和传递性一致。

```bash
ONTOLOGY_POSTGRES_TEST_DSN='postgres://addp:addp_password@127.0.0.1:15432/addp_test?sslmode=disable' \
  make test-module MODULE=ontology
# 并发安全与覆盖率仍走同一标准入口：
ONTOLOGY_POSTGRES_TEST_DSN='postgres://addp:addp_password@127.0.0.1:15432/addp_test?sslmode=disable' \
  GOFLAGS='-race -count=1 -cover' make test-module MODULE=ontology
# 图适配及 PG/FalkorDB 联动门禁；无需传入个人图数据库地址或密码：
ONTOLOGY_POSTGRES_TEST_DSN='postgres://addp:addp_password@127.0.0.1:15432/addp_test?sslmode=disable' \
  make test-ontology-falkor
# System 独立 IAM 测试库，只验证本切片的两步向前迁移：
ADDP_SYSTEM_POSTGRES_TEST_DSN='postgres://addp:addp_password@127.0.0.1:15432/addp_iam_test?sslmode=disable' \
  bash scripts/test/system-iam-postgres-gate.sh --package migration --test ontology-backend
# 共享严格 JSON 绑定器与授权覆盖变更需扩散到 Go 消费方：
make test-go
```

标准模块门禁自动发现 `backend/go.mod`、PostgreSQL T2 和门禁自管的 disposable Compose T2，先预检连接条件，再执行 T0、T1、T2；T1 明确排除数据库连接变量与图集成开关。两个 T2 均进入根 `test-integration` 串行聚合、CI 独立 Job 和本地巡检的同一发现路径。

FalkorDB T2 固定 4.20.6 多架构镜像 digest，每轮独立 Compose Project、随机密码、动态回环端口、资源上限与非零服务端超时；不读取根 `.env`，不使用个人 Redis 或图实例。正常退出、失败和可捕获中断都删除自有容器/卷/网络并核验零残留。T2 覆盖投影往返/防覆盖/篡改检测/最大定义、参数转义与大整数、服务端读写超时/回滚、重复取消后的客户端关闭和服务端回收、并发结果隔离；同一门禁再连接 PG 验证发布、真实图构建、激活、失败保留旧版本及新 generation 重建恢复，使用可控授权夹具，不宣称正式 HTTP/IAM 端到端验收。PG owner 状态门禁另覆盖并发激活、ABA、授权失效、撤回、租约恢复、重建去重/祖先分叉拒绝、新主体授权、旧授权拒绝、审计回滚与 v1/v2 向前迁移。清理核对本轮修订及全部投影的 execution，不遗漏重建任务。服务端 TCP FIN 回收允许有界异步收敛，不以一次 CLIENT LIST 快照冒充泄漏判断。

本地只允许回环地址的 `addp_test`，CI 只允许 Job 独占的 `addp_ontology_test`。门禁拒绝清空已存在的 ontology schema；当次新建 schema 带随机 Run ID 所有权标记，正常退出、失败及可捕获中断都通过同一 owner 清理函数删除本轮 execution 与 schema 并检查残留，不清空 common。强制 SIGKILL 无法运行退出钩子，若残留需核对原 Run ID 后由 owner 门禁处理，不能直接删除未知 schema。

新增 CEL 依赖同时由 T0 的 `make test-go-dependency-policy` 检查；该入口覆盖单行/块状 `require`、错误版本拒绝和注释/排除声明不误识别，不能靠固定 go.mod 排版才能识别依赖。

Outdoor 示例仅在测试夹具中；北京是示例背景，不从活动名称猜测行政区。核心代码不得硬编码业务状态、集合名、字段路径或地名。

### 正式 Online 验收入口

`ontology-revision-lifecycle` 已登记为仅手工触发的 GitHub Hosted T4 suite；入口为隔离部署中的 `make test-online ONLINE_SUITE=ontology-revision-lifecycle`，生命周期由 `scripts/test/online-hosted-ontology-gate.sh` 复用统一 Hosted owner 管理。当前仍未取得真实 T4 通过证据，脚本单测和本地开发服务 Ready 不能替代它。

场景复用 `internal/semantic/testdata/beijing_outdoor_online.json` 的北京 Outdoor 合成本体；语义内核 T1 同时验证此定义可编译，并区分明确北京、明确其他与未知地点。Hosted helper 只给 User 分配本体 read/update/publish 与执行授权派生四项权限，不创建 Engine Provisioner 或业务 Engine。所有本体操作通过 Gateway，核对两次发布的不可变快照、generation/execution、ready 与 active 指针、旧版本冲突、最新尝试发现，以及撤回第二修订不自动回退第一修订。

没有业务实例查询或分类 API 的验收。修订、审计和图历史在场景中保留，退出时停止当次应用、销毁整个独占 Infra 及数据卷和临时凭据；生命周期另查容器/网络/卷零残留。发现旧 Infra 容器或卷时必须在启动前拒绝，不能误用或删除既有环境。确定性脚本验证由 `make test-online-runner` 纳入 T0，实际执行由 `.github/workflows/online-t4-gates.yml` 的 `ontology-hosted-t4` Job 完成。
