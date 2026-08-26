# ADDP 模块启动、就绪与注册治理待办

> 状态：启动与就绪契约、公共生命周期、全模块切换和专用 Runner 进程乱序编排已于 2026-08-26 完成，剩余首次真实 T4 验收

## 一、保留边界

本文件不复制稳定规范，只临时跟踪尚未完成的公共实现、全模块迁移和外部运行证据。模块和引擎都由 System 管理，但必须保持为两个独立聚合：

- 模块采用“持久模块定义 + 临时运行实例租约 + Gateway 动态发现”；稳定契约见 [模块架构图](../concepts/addp模块架构图.md)、[Gateway 架构说明](../../gateway/docs/gateway架构说明.md) 和 [System 数据库架构](../../system/docs/数据库架构.md)。
- 引擎采用“永久 Engine Instance 身份 + 生命周期管理意图 + 连通性观测”；稳定契约见 [引擎体系图](../concepts/addp引擎体系图.md)。
- 稳定术语见 [术语表](../concepts/addp术语表.md)，测试分层与证据要求见 [ADDP 测试与验收规范](../spec/addp测试与验收规范.md)。
- 专用 Runner 的唯一环境清单和执行步骤见 [Online 专用 Runner 首次验收待办](ADDP统一测试与Online验收体系方案.md)，本文件不复制环境配置。

不得把模块运行实例和 Engine Instance 合并为同一注册实体，也不得为二者建立第二套注册、发现或状态事实源。

## 二、已完成基线

- 模块定义、Backend/Worker/Scheduler 运行实例、进程级 `instance_id`、租约心跳、按实例注销和 Gateway revision 长轮询已经形成单一主路径。
- Engine Instance 永久 ID、身份幂等、软删除墓碑、生命周期与连通性分离已经形成单一主路径。
- 模块管理列表使用有界当前运行投影，全部实例历史通过模块下的只读分页入口查询；实例历史不再随每次管理页刷新全量加载。
- Go 与 Python 公共注册客户端已统一进程生命周期和结构化错误诊断；System 不再存在第二套 Runtime Engine 注册服务。
- Develop、Manager、Service 在实际使用时消费当前状态；离线引擎保留展示但禁选，恢复在线后不要求重启消费模块或整页刷新。
- 2026-08-23 人工验收已覆盖全量冷启动、System/Gateway 晚启动、租约到期、同模块多实例、优雅注销和页面动态恢复。人工结果只作为基线，不替代自动化 T4 证据。

2026-08-25 新确认的稳定原则是：System 是所有业务模块进入 Ready 的唯一控制面强依赖，但不是进程保持 Alive 的强依赖。其他业务模块与可选 Engine Instance 不得进入当前模块 readiness。该原则已回写术语表、模块架构图、API 规范、新模块开发指南和测试验收规范。

## 三、实现待办

- [x] 在 `common/` 建立唯一模块生命周期能力，统一 `starting|registered|recovering|failed|stopped` 状态、快照、完成信号、Ready 门禁和健康 DTO。
- [x] 在 `common-python` 实现等价契约，Agent/Copilot 不保留私有状态机或不同的重试分类。
- [x] 将全部 HTTP Backend 一次性切换为 `/health/live` 与 `/health/ready`，删除 `/health`，并在健康路由之后、业务路由之前安装 Ready 门禁。
- [x] Backend 先绑定 Listener 再注册；更新全部 `health_check_url`、Monitor 探测、Gateway 健康语义、Compose/镜像探针、开发启停脚本和 T4 预检。
- [x] Worker/Scheduler 未 `registered` 时停止领取新工作；已执行工作仍按 owner execution lease 和授权契约收敛。
- [x] 公共客户端只重试连接失败、超时、`429`、`5xx` 和心跳实例缺失；确定性注册拒绝进入 `failed` 并终止进程。
- [x] 补齐 T1 状态机、Ready 路由、未就绪业务门禁和 Worker/Scheduler 停止领取测试，并让 `make test-changed` 按共享依赖扩散覆盖全部消费方。
- [x] 专用 Runner 使用受 `ADDP_ONLINE_HOST=1` 保护的正式进程入口，依次验证 Manager 先 Alive/Not Ready、System 后启动、Gateway 后启动、System 中断与同 `instance_id` 自动恢复；各阶段向仓库外证据目录写入独立 JSON。

2026-08-26 本地验证已通过 `make test-platform`、包含全部 22 个受影响模块和 PostgreSQL T2 的 `make test-changed`，以及包含正式进程编排、安全拒绝和失败清理的 `make test-online-runner`。System IAM 中原先按 Orchestrator 角色全部权限统计的脆弱断言已收窄为五个精确角色权限绑定，不再与后续 Model TaskProvider 权限迁移耦合。本地确定性证据不能替代下方专用 Runner T4。

## 四、剩余 T4 验收

- [ ] 在 `addp-online` 专用 Runner 真实执行 `make test-online ONLINE_SUITE=module-registry-recovery`，保存 workflow、suite 报告和清理结果。
- [ ] 由部署编排自动验证“业务模块先启动、System 后启动”，确认模块先 Alive/Not Ready，业务路由不可用；System 恢复后以同 ID 自动 Ready 且 Gateway 自动恢复路由。
- [ ] 由部署编排自动验证“System 与业务模块先启动、Gateway 后启动”，确认 Gateway 首个完整快照即可建立路由。
- [ ] 由部署编排自动验证全量重启后的首次页面访问，Configuration、Manager 和 Service 不需要人工刷新或二次点击。
- [ ] 自动验证模块租约失效与重新注册、Engine Instance 离线与恢复均不要求重启消费方。

进程乱序场景必须由专用部署真实控制进程生命周期；不得以本地人工操作、Mock 或仅调用注册 API 冒充部署恢复证据。首次 Online 通过后再开放夜间触发。

`module-registry-recovery` 的 Host Gate 已实现唯一进程顺序：Manager 正式 Backend → System → Gateway → 停止 System → 恢复 System。观测器验证 Manager 的 Liveness、Readiness、业务门禁和 `instance_id`，并验证 Gateway 路由随租约出现、消失和恢复；随后优雅停止正式 Manager，再运行原有幂等、双实例、发布元数据、注销和零残留套件。前三项中的模块进程乱序与中断恢复已经具备编排代码，只缺 `addp-online` 专用 Runner 的首次真实报告；全量页面首次访问和 Engine Instance 离线恢复仍是独立的后续 T4 场景，不能由本套件代替。

## 五、最小门禁

| 层级 | 入口 | 责任 |
| --- | --- | --- |
| T1 | `make test-common-python` | Python 注册、心跳、注销、实例身份和错误诊断契约 |
| T1/T2 | `make test-module MODULE=system` | System 模块与引擎注册、Repository、Handler 和迁移规则 |
| 平台一致性 | `make test-platform` | 单一发现路径、共享代码边界、可取消生命周期和注销等待规则 |
| T4 | `make test-online ONLINE_SUITE=module-registry-recovery` | 真实租约、幂等、多实例、恢复、注销与残留清理 |

新增注册字段、角色、模块、Worker、路由或公共客户端行为时，必须在同一次变更中同步正式契约、相应测试和 CI 自动发现规则。

## 六、删除条件

以下条件全部满足后直接删除本文件，不归档完成历史：

1. `module-registry-recovery` 在专用 Runner 首次真实通过，并保留可追溯报告。
2. 公共生命周期、全模块健康端点、Ready 门禁、Worker/Scheduler 领取门禁和全部调用方切换完成，旧 `/health` 已删除。
3. System 晚启动、Gateway 晚启动、System 运行中断与全量重启首次访问均取得自动化进程级证据。
4. 模块与引擎恢复不重启消费方的行为已由 T4 持续覆盖。
5. 新发现的稳定规则已经回写上述正式概念、架构或测试规范，而不是继续沉淀在本文件。
