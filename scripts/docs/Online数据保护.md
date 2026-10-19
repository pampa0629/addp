# ADDP 脚本：Online数据保护

[返回脚本导航](../README.md)

测试分层、身份和清理规则以[测试与验收规范](../../docs/spec/addp测试与验收规范.md)为准；历史结果仅适用于所列源码和运行尝试，不代表当前 HEAD 已重跑。

## Transfer 保护与原值访问

`security-transfer-protection` 要求全量 System、Gateway、Meta、Security、Transfer 和 Manager，并使用 `business/scripts/online-security-transfer-fixture.sh` 管理固定 MongoDB 源与 PostgreSQL 目标。专用环境长期预置分别指向只读 MongoDB 账号和 PostgreSQL Fixture 的 Engine Instance，专用 Tenant 预置平台推荐的手机号敏感类型、识别方式和默认保护规则。suite 经 Gateway 扫描两端资源；首次运行允许为固定 MongoDB DataItem 建立一个永久 Security 纳管，后续必须复用同一纳管并确认 Transfer 已安装 `export → mask` 投影。每轮创建两条带 Run ID 的 bounded Transfer 任务：不输出手机号的可证明 MQL 必须正常写入，输出手机号的可证明 MQL 必须写入遮盖值且明文计数为零。目标表属于固定物理 Fixture；临时 Transfer 任务在 `finally` 按捕获 ID 删除并逐条确认 404，不能撤销投影、管理员绕过或为 MongoDB 增加白名单。

`security-plaintext-access` 复用同一个复合 Fixture，在固定 PostgreSQL `addp_online_security.exemption_source` 上建立永久 Enrollment 和手机号正式 Assessment。suite 使用两名不同的专用 User：申请人在 Manager 预览确认手机号已遮盖并从该出口提交原值访问申请，审批人在 Security 待审批队列批准；随后只允许申请人的 Manager 预览临时返回原值，审批人仍看到遮盖值，到期后不访问 Security 即验证 Manager 依据本地投影恢复遮盖。固定治理聚合、申请和授权修订作为专用 Tenant 审计事实保留。环境文件必须额外提供 `ADDP_ONLINE_TEST_APPROVER_ACCESS_TOKEN`，且审批用户不得与申请用户相同。

## MySQL 与空间字段保护

`security-mysql-owner-protection` 使用唯一 GitHub Hosted Linux x86_64 disposable profile，由 `scripts/test/online-hosted-security-gate.sh` 复用共享 Hosted 生命周期。每轮从零启动 Infra、PostGIS PostgreSQL 与 MySQL，System owner helper 创建一次性 Tenant、最小权限非管理员 User、独立引擎登记用户和独立治理初始化用户；Business 只管理物理数据及 owner-only Engine descriptor，引擎登记和分类、等级、email/phone 敏感类型及默认保护初始化分别通过 System、Security 正式 API 完成。初始化令牌在业务验收前移除，不复用个人环境、永久身份、GitHub Environment 或仓库 Secret。suite 先验证默认保护同事务创建、非法参数整体回滚和探针清理，再经 Meta 扫描和 Security 正式 API 建立 Enrollment/Assessment。MySQL `customers.email` 在 Manager、Develop、Service、Transfer 四个出口均移除，但保留 5 条非敏感记录；临时任务及服务按捕获 ID 清理。成功、失败退出都停止应用并销毁当次业务容器及 Infra，核对零残留；凭据只保存在 `runner.temp` 的 owner-only Secret 目录并删除。旧 macOS self-hosted 部署入口与其 Security 专属物理夹具已移除。

同一 Hosted suite 的 Console 浏览器阶段使用上述普通 User 正式登录，在 Manager 从已纳管的 MySQL `customers` 点击导出表格式能力声明支持的 CSV，通过 Python 标准 CSV 解析器核对完整五行、敏感 email 字段移除及非敏感字段保留，再以同一身份查询并打开 Monitor 中对应的 Transfer 一次性执行。执行必须成功、来源为 Manager、无任务定义引用，且 `triggered_by` 与已验证 User 一致；不允许浏览器自报身份或用 API 创建代替点击导出。测试身份只额外持有数据探查页面必需的 `manager.content.read`、`manager.derived_artifact.create/read` 与 `monitor.execution.read`；原有 `manager.data_item.read` 同时参与页面入口校验，准备身份仍不进入业务阶段。Playwright 失败、缺失或不匹配的浏览器报告使整个 suite 失败。全新匿名浏览器初始化时唯一一次 `/system/refresh` 的 401 及对应 Chromium 网络告警单独计数并验证；正式登录成功后，任何 401、业务失败响应或 warning/error 均使验收失败，JavaScript 异常全程检查。浏览器报告只接受 v2，包含上述匿名刷新计数、安全执行 UUID、业务计数与断言结果，截图归档，原始下载文件删除。Monitor 加入同一版本健康预检，应用与文件仍由既有 Hosted 生命周期清理；此扩展通过真实 T4 前不计为已验收。

同一 suite 还使用 PostgreSQL `addp_online_security.spatial_algorithm_source` 与 `spatial_algorithm_transfer` 验收空间表的普通属性脱敏。三个字符串字段通过 Meta 当前组件清单形成正式 Assessment，绑定当次初始化的 `phone` 类型及其初始等级；空间列名为 `location_point`。依次将该组合的默认保护切换为前后掩码、固定值替换、普通 SM3，等待四个 Owner 确认最新投影后核对五行输出、Unicode、null、空串异常效果和点坐标。掩码阶段还单独为 Manager 三个字段配置全掩码、固定值替换、SM3，验证字段配置只作用于 Manager preview，其他出口仍执行默认保护。测试成功、失败及中断退出时撤销本轮字段策略、删除临时 Service/Transfer 任务，并恢复运行前的完整 phone 默认规则；审计历史与当次 Enrollment/Assessment 随 disposable Infra 一同销毁。若发现前次遗留的活动字段策略，预检失败，不能替本轮静默接管。专用 User 还需 `security.assessment.create`、`security.policy.read/create/update/delete` 和 `security.protection_baseline.update`，当次 Tenant 与业务引擎仅供本 suite 使用。确定性协议、失败清理与 Fixture 回归由现有 `make test-online-runner` 和平台 T0 覆盖；真实执行仍使用 `make test-online ONLINE_SUITE=security-mysql-owner-protection`，未真实运行不得标记为 T4 通过。
