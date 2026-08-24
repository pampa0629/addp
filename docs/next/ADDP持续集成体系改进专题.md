# ADDP 持续集成体系改进专题

> 状态：仓库内 CI 主线已经完成；本文只保留 GitHub 外部治理与专用 T4 Runner 待办（2026-08-23）。

## 一、专题边界

稳定规则和当前实现不再由本文维护：

- 测试分层、CI 编排、数据隔离和交付门禁以 [ADDP 测试与验收规范](../spec/addp测试与验收规范.md) 与 [ADDP 开发原则](../spec/addp开发原则.md) 为准。
- 当前 Make 目标、自动发现、suite 登记、报告和 workflow 调用以根 `Makefile`、`scripts/README.md`、`scripts/test/`、`scripts/ci/` 与 `.github/workflows/` 为准。
- 专用 Runner 的准备、两个 T4 suite 首跑和进程乱序证据由 [Online 专用 Runner 首次验收待办](ADDP统一测试与Online验收体系方案.md) 唯一跟踪。

本文只记录无法通过仓库代码闭环的 GitHub 仓库设置、Runner 和运营决策。完成第四节的关闭条件后删除本文，不归档第二份 CI 规范或实施历史。

## 二、仓库内已完成基线

- [x] T0-T3、T5 和手工 T4 均只通过根 `Makefile` 标准入口编排，workflow 不复制业务测试。
- [x] owner 影响计算由本地 `make test-changed` 与 CI 共用；前端、Python、PostgreSQL、Online、Release 和产品构建登记均由 `make test-platform` 自动检查。
- [x] GitHub Hosted Runner 承担确定性与 disposable 基础设施门禁；专用 macOS 自托管 Runner 只承担隔离 T4 部署。
- [x] Action 与 PostgreSQL Service 引用固定到不可变摘要；普通门禁最小只读权限，只有发布 Job 取得所需写权限。
- [x] 旧专项 workflow、兼容入口、占位 suite 和在 YAML 中重复的 owner 路径表已经删除。
- [x] Renovate 只管理 GitHub Actions 与 PostgreSQL 15 Service 摘要，要求发布沉淀期、Dependency Dashboard 审批且不自动合并。

## 三、2026-08-23 外部状态快照

本节是公开 GitHub API 的只读快照，不是长期契约。

### 3.1 已核实

| 项目 | 当前事实 |
| --- | --- |
| 仓库 | `pampa0629/addp` 为公开仓库，默认分支为 `main`。 |
| 远端 workflow | 远端 `main` 当前有三个 active workflow：Platform CI、Quality frontend smoke、Release and T2 gates。 |
| 本地待推送 workflow | 当前工作区另有 `.github/workflows/online-t4-gates.yml`；它尚未进入远端，因此不能宣称远端已经具备 T4 workflow。 |
| Ruleset | `main release gates` 处于 active，禁止删除与非快进更新，要求 PR、解决 review thread，并严格要求 `CLI product gate (macOS Keychain)` 与 `System IAM gate (PostgreSQL 15)`。 |
| 最近一次共同提交 | 三个远端 workflow 在提交 `88fbce1af4e9417fc0bc6976f6994d6ff8b75235` 上均成功；Platform CI 约 8 分 20 秒，Quality 约 36 秒，Release/T2 约 2 分 8 秒。耗时只用于当前成本观察。 |
| Environment | 公开 API 返回 0 个 Environment，`addp-online` 尚未建立。 |
| Renovate | [Dependency Dashboard #5](https://github.com/pampa0629/addp/issues/5) 由 `renovate[bot]` 维护，2026-08-23 仍有更新，App 与 Dashboard 已确认有效。 |

### 3.2 当前权限不足，不能核实

本地 `gh auth status` 显示未登录；公开 API 无法读取以下管理员状态：

- Actions 默认 workflow 权限、fork PR 审批策略和允许的 Action 来源。
- Ruleset bypass actor 的完整配置。
- Artifact 与日志默认保留期、Actions 用量与账单。
- 自托管 Runner 清单、Runner Group、并发与标签实际状态。
- Environment 保护规则、变量和 Secret 元数据。
- 仓库通知接收人及失败通知策略。

不得根据旧快照或 workflow YAML 推断这些外部设置已经正确。

## 四、剩余待办与关闭条件

### P0：专用 T4 Runner

- [ ] 新机器到位后，按 [Online 专用 Runner 首次验收待办](ADDP统一测试与Online验收体系方案.md) 建立 `addp-online` Environment、注册 Runner、推送手工 workflow 并完成首次真实执行。

本文不复制该清单的机器、身份、数据库和 suite 细节。

### P1：管理员登录态审计

- [ ] 在具备仓库管理员只读权限的环境中重新读取 3.2 的六类状态。
- [ ] 核对 Ruleset required check 名称与远端 workflow Job 名称完全一致，并确认 bypass 仅保留用户明确需要的主体。
- [ ] 确认 fork PR、不受信任代码、自托管 Runner 和 Secret 不会进入同一执行边界。
- [ ] 确认 Artifact / 日志保留期和 Actions 用量满足调试与成本需要；没有明确价值时不新增外部通知或历史统计系统。

### 关闭条件

满足以下条件后删除本文：

1. 专用 T4 Runner 首次验收完成，远端能看到 T4 workflow 与 `addp-online` Environment。
2. 使用管理员只读权限完成 3.2 的仓库设置审计，发现的偏差已经修正或形成用户明确接受的决定。
3. 稳定契约已回写正式规范，当前运行事实已回写 `scripts/README.md`；本文不再拥有独有待办。

外部状态的日期快照只服务于当前决策，关闭时直接删除，不迁入长期规范。
