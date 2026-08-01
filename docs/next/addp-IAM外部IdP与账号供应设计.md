# ADDP IAM 外部 IdP 与账号供应设计

更新日期：2026-08-01

状态：待设计和实现。当前数据库具有 Identity Provider、Tenant Connection 和 External Identity 的目标表基础，但尚无外部 IdP 登录、账号供应和单点退出运行时。

## 一、稳定边界

- System 始终是 ADDP User、Tenant Membership、Role、Permission 和会话的唯一 IAM 权威；
- 外部 IdP 只证明外部主体身份，不直接成为 ADDP Tenant 或授权事实源；
- 外部身份稳定键为 `issuer + subject`，邮箱不能作为永久身份主键；
- 外部 IdP Token 不进入业务模块，成功联合后由 System 建立 ADDP opaque Token Family；
- Platform Context 与 Tenant Context 继续互斥。

## 二、待确认范围

1. Identity Provider 配置、密钥和元数据更新边界；
2. Tenant 与 IdP Connection 的启停、域限制和认证策略；
3. 管理员预配与受控即时供应的唯一选择规则；
4. External Identity 绑定、冲突、换绑和恢复流程；
5. 姓名、邮箱、部门等属性的权威来源和映射版本；
6. 首次登录时 User、Membership 和默认 Role 的事务边界；
7. 离职、禁用、属性变化和 Membership 回收；
8. MFA、AAL、step-up 与外部认证强度映射；
9. 单点登录、当前 ADDP Client 退出、ADDP 全局退出和外部 IdP 单点退出；
10. 审计、重放防护、错误语义和管理控制台。

## 三、数据模型基础

`system.identity_providers`、`system.tenant_idp_connections` 和 `system.external_identities` 已由 IAM migration 建立。现有表只表示目标事实边界，不代表运行时能力已启用。

实施前必须逐字段复核当前 migration 是否满足最终协议和属性映射设计。需要变化时增加新的向前 migration，不修改既有 migration，也不增加旧字段兼容读取。

## 四、事务要求

首次联合或绑定至少需要原子处理：

- 锁定外部身份稳定键；
- 解析或创建唯一 ADDP User；
- 校验 Tenant Connection 和供应策略；
- 创建或更新 Tenant Membership；
- 推进授权版本并处理既有 Token Family；
- 写入安全审计。

外部网络调用不得持有数据库事务锁。必须先完成协议验证，再在短事务中提交 ADDP 权威事实；并发首次登录只能创建一个绑定。

## 五、禁止事项

- 直接把外部 `sub`、邮箱或 Token 当作 ADDP Principal；
- 让外部组名自动成为任意 ADDP Role；
- 同时保留管理员预配和无约束即时供应两条竞争路径；
- 通过邮箱自动合并两个既有 User；
- 在业务模块中校验外部 IdP Token；
- 用兼容字段或双写保留旧账号体系。

## 六、验收

必须覆盖 issuer/subject 唯一性、并发首次登录、预配与即时供应策略、属性更新、停用撤权、Membership 回收、MFA/AAL 映射、上下文选择、退出语义、审计脱敏和外部 IdP 不可用场景。

完成实现后，稳定概念并入 `docs/concepts/addp账号与权限体系图.md`，认证要求并入 `docs/spec/addp登录认证的统一要求.md`，数据和运行时细节并入 `system/docs/IAM数据模型与迁移规范.md`，本文删除。
