# ADDP Online 验收后续事项

测试分层、标准入口、身份隔离、报告与各固定场景断言统一维护于[测试与验收规范](../spec/addp测试与验收规范.md)。本文只跟踪专用 self-hosted 部署的准备和尚缺真实证据；已迁入 Hosted 的场景使用各自 disposable profile，不列入专用 Mac 首跑清单。

## 一、部署与证据边界

- Runner 分发唯一依据为[Online workflow](../../.github/workflows/online-t4-gates.yml)，suite 与生命周期以 [scripts 说明](../../scripts/README.md)、[Online registry](../../scripts/test/online-gate.py)及 [Host Gate](../../scripts/test/online-host-gate.sh)为准。已具备实现和确定性测试，不等于已取得实机 T4。
- 历史排队运行已经不能续用；本文没有当前 Runner 在线状态或外部 Settings 的实时审计结论。准备完成后重新手工触发并核对实际报告，不把旧 queued 记录当作待执行作业。
- 专用部署要求独立账号、干净 checkout、仓库外 owner-only 环境文件和证据目录；个人开发账号、根 .env、生产凭据或脏工作区不得接单。工具链版本遵循仓库当前锁定文件，不保存过时安装清单。

## 二、专用部署准备

1. 准备具有 self-hosted/macOS/addp-online 标签的专用 Runner；GitHub Environment 与外部环境文件路径按 Host Gate 配置，真实秘密不写进仓库或报告。
2. 只经正式 UI/API 建立每个 suite 所需的最小权限 User、Tenant 与永久业务 Fixture；Engine Instance 不按 Run ID 创建再删除。平台和业务数据库、端口、凭据彼此隔离。
3. 先运行 `bash scripts/test/online-host-gate.sh --check-only`，检查工具链、checkout、回环边界、环境权限及证据目录；不放宽准入绕过资源不足。
4. 通过 workflow 按 suite 运行，核对构建身份、AuthContext、业务断言、addp.online-gate/v1 报告及成功/失败/中断清理，保存外部 Artifact。

## 三、待取得专用环境报告的范围

以下按当前 workflow 的默认 self-hosted 分发列出，不是历史固定数量；新增或迁出 profile 时直接更新清单：

- [ ] `consumer-engine-recovery`。
- [ ] `enterprise-catalog-publishing`。
- [ ] `manager-hybrid-search`。
- [ ] `module-registry-recovery`。
- [ ] `oceanbase-consumer-flow`。
- [ ] `security-transfer-protection`。
- [ ] `standard-model-reference-deletion`。
- [ ] `tidb-consumer-flow`。
- [ ] `transfer-insert-only-mysql`。
- [ ] `workbench-service-consumption`。

优先验证 module-registry-recovery 的进程乱序恢复，再验证 consumer-engine-recovery 的首次页面访问、同 iframe 离线/恢复及消费方 PID 不变。其他场景按实际业务需求安排；没有新报告的项目不勾选，不据此否定独立 Hosted 场景的已有证据。

## 四、关闭条件

范围内 suite 取得真实报告并核对清理后从清单移除。若确认迁到 Hosted，须同次完成唯一 profile、隔离生命周期和 CI 登记，不能仅改 Runner 标签。剩余部署事项关闭后删除本文；稳定规则继续由正式规范维护，不归档逐轮日志。
