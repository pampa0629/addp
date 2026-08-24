# ADDP 模块与引擎注册 T4 验收待办

> 状态：稳定治理机制已收口，人工验收已通过，待专用部署完成 T4 首跑与进程乱序自动化验收（2026-08-23）

## 一、保留边界

本文件不是新的治理规范，只临时跟踪尚未取得的外部运行证据。模块和引擎都由 System 管理，但必须保持为两个独立聚合：

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

## 三、剩余 T4 验收

- [ ] 在 `addp-online` 专用 Runner 真实执行 `make test-online ONLINE_SUITE=module-registry-recovery`，保存 workflow、suite 报告和清理结果。
- [ ] 由部署编排自动验证“业务模块先启动、System 后启动”，确认模块自动重注册且 Gateway 自动恢复路由。
- [ ] 由部署编排自动验证“System 与业务模块先启动、Gateway 后启动”，确认 Gateway 首个完整快照即可建立路由。
- [ ] 由部署编排自动验证全量重启后的首次页面访问，Configuration、Manager 和 Service 不需要人工刷新或二次点击。
- [ ] 自动验证模块租约失效与重新注册、Engine Instance 离线与恢复均不要求重启消费方。

进程乱序场景必须由专用部署真实控制进程生命周期；不得以本地人工操作、Mock 或仅调用注册 API 冒充部署恢复证据。首次 Online 通过后再开放夜间触发。

## 四、最小门禁

| 层级 | 入口 | 责任 |
| --- | --- | --- |
| T1 | `make test-common-python` | Python 注册、心跳、注销、实例身份和错误诊断契约 |
| T1/T2 | `make test-module MODULE=system` | System 模块与引擎注册、Repository、Handler 和迁移规则 |
| 平台一致性 | `make test-platform` | 单一发现路径、共享代码边界、可取消生命周期和注销等待规则 |
| T4 | `make test-online ONLINE_SUITE=module-registry-recovery` | 真实租约、幂等、多实例、恢复、注销与残留清理 |

新增注册字段、角色、模块、Worker、路由或公共客户端行为时，必须在同一次变更中同步正式契约、相应测试和 CI 自动发现规则。

## 五、删除条件

以下条件全部满足后直接删除本文件，不归档完成历史：

1. `module-registry-recovery` 在专用 Runner 首次真实通过，并保留可追溯报告。
2. System 晚启动、Gateway 晚启动和全量重启首次访问均取得自动化进程级证据。
3. 模块与引擎恢复不重启消费方的行为已由 T4 持续覆盖。
4. 新发现的稳定规则已经回写上述正式概念、架构或测试规范，而不是继续沉淀在本文件。
