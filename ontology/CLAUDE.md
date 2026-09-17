# Ontology 模块

Ontology 管理租户领域本体的形式化语义，不接管 Standard、Model、Catalog 的事实所有权，也不读取业务数据库。总体边界见 [最小设计契约](../docs/next/ADDP%20Ontology最小设计契约.md)。

## 当前交付范围

`backend/internal/semantic` 是正式 Go 语义内核；`internal/service`、`internal/repository` 已增加原生定义的 PG 修订生命周期。尚无 HTTP 服务、页面、FalkorDB 服务或 Agent Tool，不登记占位服务，不影响 Graph 的当前功能。

`internal/falkor` 已增加唯一的 go-redis 薄适配及确定性图投影构建/全量校验；当前只由独占 T2 驱动，尚未接入发布执行器、IAM 准入或 PG 激活指针，不能把适配测试通过当作生产运行时已上线。

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
- 撤回与 pending 构建意图取消同事务；不伪装取消已运行工作。未实现激活/ready、图重建或图执行器，也不把 published 当作可运行。
- Actor 只承载调用方已核实的租户/主体/成员/授权版本。当前无真实 IAM 准入，pending execution 没有 Execution Authorization，未来领取必须要求授权。不得把内部服务直接暴露为未鉴权 API。
- `repository.Migrate` 使用 `common/schema.Migrate` 协调 `migrations/001_revisions.sql`，重复同版本不执行；新迁移只能向前增加，不改已发布 SQL。生产 Ontology 只用 `schema.Require` 检查 common，不创建共享执行表。

## 代码与测试

### FalkorDB 适配边界

- 每条命令使用独占连接、无自动重试、RESP2 标量结果；取消关闭本请求连接并拒绝迟到结果，不承诺服务端立即硬中止。最多 4 条本地在途命令，满额立即拒绝。
- 查询必须携带 1–2000 ms 服务端预算，且不超过调用方剩余 deadline。服务端必须同时启用非零 `TIMEOUT_MAX/TIMEOUT_DEFAULT`；写超时回滚可能额外耗时。取消/网络错误后的写入结果不确定，禁止原地自动重放。
- `Plan` 只从已校验的不可变快照和 owner generation 派生物理 graph key。图内只保存类型、属性、关系、规则的成员身份/名称及结构引用；CEL、规则依据与业务事实不复制。传递/逆关系实例推理未实现。
- `Build` 原子保留空 generation，拒绝覆盖已有图；分阶段写入后由 `Verify` 比较摘要、完整成员/边集合及总数。失败可能留下部分图，不能据此激活；清理必须由后续 PG owner 核实引用与 lease 后协调，不在取消路径直接删除仍可能写入的图。
- 适配不是授权器。未来执行器必须在构建前核实授权和租约，在输出/激活前再次核实取消、撤回、授权、lease/fencing 和激活基线。当前不提供任意 Cypher API，也没有从 PG 发布自动触发图写入的旁路。

### 标准门禁

后端模块：`github.com/addp/ontology`，Go 1.24.2。规则上限是版本化内核契约的一部分，不通过用户选项关闭；修改上限或表达式语义须升级契约版本。

v1 边界：64 类、256 属性、128 关系、64 规则；单类 8 个直接父类、继承深度 32；单规则 16 个输入、4096 字节表达式、256 个 AST 节点、深度 32、运行成本 256；字符串值最多 4096 字节，枚举/字面量集合最多 64 项，规范化前定义 JSON 最多 1 MiB。空属性枚举表示不限定字符串取值；字面量成员集合不允许空集合或重复值。属性不得覆盖继承链上的同名 key，菱形继承同一个属性不算冲突。v1 仅接受两端为同一类的传递关系声明；逆关系须双向显式引用，端点和传递性一致。

```bash
ONTOLOGY_POSTGRES_TEST_DSN='postgres://addp:addp_password@127.0.0.1:15432/addp_test?sslmode=disable' \
  make test-module MODULE=ontology
# 并发安全与覆盖率仍走同一标准入口：
ONTOLOGY_POSTGRES_TEST_DSN='postgres://addp:addp_password@127.0.0.1:15432/addp_test?sslmode=disable' \
  GOFLAGS='-race -count=1 -cover' make test-module MODULE=ontology
# 独立图适配门禁，无需传入个人数据库地址或密码：
make test-ontology-falkor
```

标准模块门禁自动发现 `backend/go.mod`、PostgreSQL T2 和门禁自管的 disposable Compose T2，先预检连接条件，再执行 T0、T1、T2；T1 明确排除数据库连接变量与图集成开关。两个 T2 均进入根 `test-integration` 串行聚合、CI 独立 Job 和本地巡检的同一发现路径。

FalkorDB T2 固定 4.20.6 多架构镜像 digest，每轮独立 Compose Project、随机密码、动态回环端口、资源上限与非零服务端超时；不读取根 `.env`，不使用个人 Redis 或图实例。正常退出、失败和可捕获中断都删除自有容器/卷/网络并核验零残留。T2 覆盖投影往返/防覆盖/篡改检测/最大定义、参数转义与大整数、服务端读写超时/回滚、重复取消后的客户端关闭和服务端回收、并发结果隔离。服务端 TCP FIN 回收允许有界异步收敛，不以一次 CLIENT LIST 快照冒充泄漏判断。

本地只允许回环地址的 `addp_test`，CI 只允许 Job 独占的 `addp_ontology_test`。门禁拒绝清空已存在的 ontology schema；当次新建 schema 带随机 Run ID 所有权标记，正常退出、失败及可捕获中断都通过同一 owner 清理函数删除本轮 execution 与 schema 并检查残留，不清空 common。强制 SIGKILL 无法运行退出钩子，若残留需核对原 Run ID 后由 owner 门禁处理，不能直接删除未知 schema。

新增 CEL 依赖同时由 T0 的 `make test-go-dependency-policy` 检查；该入口覆盖单行/块状 `require`、错误版本拒绝和注释/排除声明不误识别，不能靠固定 go.mod 排版才能识别依赖。

Outdoor 示例仅在测试夹具中；北京是示例背景，不从活动名称猜测行政区。核心代码不得硬编码业务状态、集合名、字段路径或地名。
