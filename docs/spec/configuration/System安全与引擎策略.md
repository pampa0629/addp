# ADDP 配置：System安全与引擎策略

[返回配置规范](../addp配置介绍.md)

### System IAM 安全策略

System IAM 安全策略是 `platform_only` 的 System-owned 平台安全配置，由 Platform Security Administrator 使用 `iam.security_policy.read/update` 维护。Platform System Administrator 不继承该职责，Tenant Context 不得读取或修改。

当前策略只包含以下普通运行字段：

| 字段 | 单位 | 约束 |
| --- | --- | --- |
| Access Token TTL | 分钟 | `1-60` |
| Delegated Access Token TTL | 分钟 | `1-2` |
| Browser Resource Access Ticket TTL | 分钟 | `1-60`，且不得大于 Access Token TTL |
| Refresh Token Family TTL | 天 | `1-365` |
| OAuth Authorization Code TTL | 分钟 | `1-5` |
| OAuth Device Code TTL | 分钟 | `5-30` |
| OAuth Device Poll Interval | 秒 | `5-60` |
| Tenant Invitation TTL | 小时 | `1-720` |
| OAuth public rate limit | 次/分钟 | `1-10000` |
| OAuth authenticated-user rate limit | 次/分钟 | `1-10000` |

策略保存到 `system.iam_security_policy` 单例表，以 `version` 做乐观并发控制，并记录 `applied_version`。IAM Runtime 在启动时只读取一次当前持久化版本并把它标记为已应用；更新 API 保存新版本后必须返回 `pending_restart=true`，直到 System 受控重启后才生效。该策略不热更新，不从根 `.env`、模块私有 env 或代码外部参数回退。

策略更新必须与 `iam.security_policy.updated` 审计事件在同一事务提交。审计只记录版本和普通数值字段差异，不记录 Token、Pepper、MFA Secret、Service Client Secret 或任何其他密钥材料。

模块通过 System 获取 AuthContext、Engine Instance 和其他 System-owned 业务事实，不通过 System 获取本模块进程数据库密码、加密密钥或普通运行配置。业务数据源连接信息继续以 System `engines` 强类型资源为事实源；这不等于 System 是所有配置的事实源。

## IAM 首次初始化

目标 IAM 不创建默认 `SuperAdmin`、默认租户或弱密码账号，也不接受通过环境变量开启公开注册。平台系统管理员、安全管理员和审计管理员只能通过一次性离线 Bootstrap 建立；Bootstrap 完成后永久关闭，后续平台高权限身份变更统一走双人审批。

Bootstrap 使用离线 `iam-bootstrap prepare/apply` 两阶段命令；已完成 Bootstrap 后，管理员凭据恢复先按目标岗位矩阵由另外两类管理员申请、复核；无法形成合格治理组合时，使用离线 `iam-recovery prepare/apply` 恢复凭据，不修改角色和暂停状态。Secret 和三员密码只通过 TTY 输入，具体步骤见 `docs/guide/addp IAM三员初始化操作指南.md`。


## API 端点摘要

**公开认证与邀请入会**:

- `POST /api/v1/system/login` - 本地账号登录
- `POST /api/v1/system/auth/mfa-verifications` - MFA 验证
- `POST /api/v1/system/tenant/invitations/registrations` - 持有有效邀请的新用户注册

System 不提供公开 `/register`。平台 IAM 管理使用 `/platform/*`，Tenant IAM 管理使用 `/tenant/*`，当前用户自服务使用 `/users/me`，OAuth 2.0 使用 `/oauth/*`。完整端点与权限元数据以 System Swagger 为准。

**另请参阅**: [配置规范总入口](../addp配置介绍.md)。
## 栅格引擎资源策略

System 的引擎实例管理拥有每个 GeoPython Runtime 的强类型栅格资源策略和 Tenant 使用额度，Console 通过“配置管理 → 系统管理 → 引擎配置 → 选择引擎 → 栅格引擎资源策略”（`/system/configuration`）呈现。引擎配置先选择已登记实例，再显示该实例的栅格资源策略。当前列表沿用原配置 API，仅管理 active、共享、内置的 GeoPython 引擎实例，不借用引擎目录权限扩大配置范围。它是 System-owned 的引擎资源治理，不代存 Manager 或 Develop 的业务策略，Runtime 不新增数据库、权限管理或控制面。平台策略默认总运行 2 项、总等待 2 项、GDAL 块缓存 256 MiB；平台管理员可修改。平台同时管理 Tenant 默认运行/等待额度（均默认 2），Tenant 管理员只能在平台总上限内修改本租户额度或恢复继承，不得修改全局容量、缓存或其他 Tenant。执行同时受进程共享上限与当前 Tenant 额度约束，不承诺独享资源。

策略使用独立正整数版本和精确配置 Permission，保存与审计同事务。平台 PUT 必须完整提交版本和五个预算字段；Tenant PUT 必须提交版本、运行及等待额度，后两项为 null 时恢复继承。缺失字段、额外字段和平台预算的 null 均拒绝。新注册实例或首次租户读取会在 System 事务内物化定义默认记录；Runtime 不另存默认值。配置值只由 System 的持久化事实与已声明定义默认值解析，不从环境变量或任务参数回退。并发/等待策略由 Runtime 周期读取后热更新，缩容保留已有工作；运行额度小于当前活动数时暂停领取，等待数超过新上限时拒绝新入队。缓存仅在新 Runtime 进程第一次应用策略时固定，后续变更展示待重启；已保存版本、实际应用的准入版本和实际缓存预算分开展示。

资源建议根据 Runtime 上报的有效 CPU 和内存约束生成，包含容器 cgroup 与 CPU affinity，未知字段明确为空。建议基于有效 CPU（并发取向下取整、至少 1、至多 2）及内存约束（至少 512 MiB 才生成建议，缓存为限额的 1/32、至多 256 MiB），属于明确展示的启发式。建议由管理员显式采用，不自动修改配置，不依据瞬时空闲内存保证安全并发，不把 GDAL 块缓存当作总 RSS 上限。当前范围仅为 GeoPython 栅格调用，不扩展其他引擎资源治理。
