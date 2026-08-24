# ADDP IAM 三员初始化操作指南

更新日期：2026-07-30

## 一、适用范围

本指南用于在已经完成当前 migration、但尚未建立任何 User Principal 的 ADDP IAM 数据库中，通过离线 CLI 一次性创建：

- 平台系统管理员；
- 平台安全管理员；
- 平台审计管理员。

初始化只允许执行一次。成功后 Bootstrap 永久关闭，不能通过默认密码、网络注册接口或开发模式绕过。

当前 migration 会预置平台内置 Service Principal、对应 OAuth Client 和机器 Role Assignment。这些机器身份是全新数据库的正常基线，不使用 User 密码、MFA 或平台三员 Role，也不阻断三员 Bootstrap。这里的“尚未初始化”只表示不存在任何 User Principal 且不存在 Bootstrap 状态，不表示 `system.principals` 或 `system.role_assignments` 物理空表。

## 二、先区分三种内容

| 名称 | 外观 | 用途 | 应该输入到哪里 |
| --- | --- | --- | --- |
| Bootstrap Secret | 以 `addp_bs_` 开头 | 授权本次一次性初始化 | CLI 的 `Bootstrap Secret:` 提示 |
| TOTP Secret | 较长的 Base32 字母数字串 | 在不支持扫码时手动建立 TOTP 账号 | 认证器 App 明确标注的“手动输入设置密钥/TOTP”页面，不能填入短信/邮件激活入口 |
| TOTP 验证码 | 6 位数字，每 30 秒变化 | 证明当前持有认证器 | CLI 的“TOTP 验证码”提示 |

最容易发生的错误是把 TOTP Secret 粘贴到“TOTP 验证码”提示。这里必须输入认证器当前显示的 **6 位数字**，不能输入 Secret。

Bootstrap Secret、TOTP Secret、密码和验证码都不得：

- 写进 Manifest；
- 放进命令行参数；
- 发送到聊天、工单或日志；
- 截图或录屏；
- 保存到仓库。

一旦 Bootstrap Secret 或任一 TOTP Secret 被截图、发送或泄露，应立即停止操作并按第八节处理。

## 三、准备三员资料

Manifest 只包含非 Secret 资料。开发环境可以使用仓库文档中的示例账号；生产环境必须替换为三个不同自然人的实名资料。

```json
{
  "administrators": [
    {
      "role_key": "platform.system_administrator",
      "username": "system-admin",
      "display_name": "系统管理员",
      "primary_email": "system-admin@example.com",
      "locale": "zh-cn"
    },
    {
      "role_key": "platform.security_administrator",
      "username": "security-admin",
      "display_name": "安全管理员",
      "primary_email": "security-admin@example.com",
      "locale": "zh-cn"
    },
    {
      "role_key": "platform.audit_administrator",
      "username": "audit-admin",
      "display_name": "审计管理员",
      "primary_email": "audit-admin@example.com",
      "locale": "zh-cn"
    }
  ]
}
```

本地开发使用的示例文件路径为：

```text
/tmp/addp-iam-bootstrap.json
```

三个账号必须使用不同密码，且每个密码至少 14 个字符。

## 四、准备认证器

手机或桌面端需要安装支持标准 TOTP 的认证器，例如：

- 2FAS；
- Google Authenticator；
- Microsoft Authenticator；
- 1Password；
- Bitwarden Authenticator。

确保设备时间使用网络自动同步。设备时间偏差会导致正确的 6 位验证码仍被判定无效。

## 五、执行初始化

### 5.1 构建 CLI

在 ADDP 仓库根目录执行：

```bash
cd /Users/pampa/code/addp
make build-iam-bootstrap
```

### 5.2 生成一次性 Bootstrap Secret

```bash
dist/release-$(go env GOOS)-$(go env GOARCH)/addp-iam-bootstrap prepare
```

终端会显示一次 Bootstrap Secret，有效期最多一小时。保持该终端打开，不要截图。

### 5.3 进入交互式初始化

```bash
dist/release-$(go env GOOS)-$(go env GOARCH)/addp-iam-bootstrap apply \
  --manifest /tmp/addp-iam-bootstrap.json
```

在 `Bootstrap Secret:` 提示处输入上一步生成的 `addp_bs_...`。输入不会回显，按 Enter 继续。

### 5.4 配置每个管理员

CLI 会依次处理系统管理员、安全管理员和审计管理员。每个管理员执行相同步骤：

1. 输入该账号的独立密码；
2. 再次输入相同密码；
3. CLI 在终端直接显示 TOTP 二维码，并同时显示手动设置密钥备用值；
4. 打开认证器，选择“添加账号”或“扫描二维码”；
5. 首选直接扫描终端二维码；
6. 只有认证器明确提供“手动输入设置密钥/TOTP”时，才填写 CLI 显示的 TOTP Secret，并选择“基于时间”或“TOTP”；
7. 不要把 TOTP Secret 填入短信/邮件激活入口，该入口需要认证器厂商发送的激活码，不是标准 TOTP 登记；
8. 保存后，认证器会显示 6 位数字验证码；
9. 在 CLI 的“输入当前 TOTP 验证码”处输入该 6 位数字；
10. 等待认证器数字变化后，立即输入新的 6 位数字。

第二次必须是紧邻的下一个 30 秒窗口。不要跳过多个验证码，也不要重复输入同一个验证码。

完成一个管理员后，CLI 会自动进入下一个管理员。

## 六、成功结果

三个管理员全部完成后，CLI 才会在单个数据库事务中写入身份、MFA、角色和审计事实。成功输出包含：

```text
IAM Bootstrap 已永久完成于 ...
platform.audit_administrator -> Principal ...
platform.security_administrator -> Principal ...
platform.system_administrator -> Principal ...
```

验证数据库状态：

```bash
docker exec addp-postgres psql -U addp -d addp -Atc "
select status, completed_at, secret_hash is null
from system.iam_bootstrap_state;

select count(*) from system.users;
select count(*) from system.mfa_credentials where status = 'active';
select count(*)
from system.role_assignments assignment
join system.roles role on role.id = assignment.role_id
join system.principals principal on principal.id = assignment.principal_id
where assignment.status = 'active'
  and assignment.scope_type = 'platform'
  and principal.principal_type = 'user'
  and role.role_key in (
    'platform.system_administrator',
    'platform.security_administrator',
    'platform.audit_administrator'
  );
"
```

预期结果：

- Bootstrap 状态为 `completed`；
- `secret_hash is null` 为 `true`；
- User 和有效 MFA Credential 均为 3；
- 上述按 User、Platform Scope 和三员 Role 精确过滤的有效 Role Assignment 为 3；
- `system.principals` 和全部 `system.role_assignments` 还包含 migration 预置的机器身份事实，总数不应按 3 验收。

## 七、初始化后的首次登录

启动 System 和 Console 后：

1. 打开 `http://localhost:5170/login`；
2. 输入其中一个管理员的用户名和密码；
3. 输入该账号认证器当前显示的 6 位验证码；
4. 选择 Platform Context；
5. 登录后确认 AuthContext 为 `context.type=platform` 且 `authentication.assurance_level=aal2`；
6. 从 Console 进入“系统管理 -> 身份与访问管理”，对应地址为 `/system/iam`。

三员权限互斥，登录成功不代表三个账号可以执行相同管理操作。

### 7.1 三类管理员的工作台验收

工作台只根据 AuthContext 中的 Permission 显示标签和操作，不根据账号名或 Role Key 硬编码页面。依次退出并使用三个账号登录，预期如下：

| 登录身份 | 应显示 | 不应显示 |
| --- | --- | --- |
| 平台系统管理员 | 租户生命周期、身份变更审批；可复核安全管理员发起的身份变更 | 用户生命周期、平台审计 |
| 平台安全管理员 | 用户生命周期、身份变更审批；可创建身份暂停或恢复申请 | 租户生命周期、平台审计、批准自己发起的申请 |
| 平台审计管理员 | 平台审计、身份变更审批只读监督；可导出审计事件 | 租户和用户写操作、批准或拒绝身份变更 |

若某账号看到不属于其职责的写操作，不得把它当作前端显示问题跳过，应检查该会话 AuthContext 的 `authorization.role_assignments[].permissions` 和数据库 Role Assignment。若页面标签正确但 API 返回 `403`，应检查前端 AuthContext 是否陈旧、Token Family 是否已因授权变更撤销，以及当前是否确实为 Platform Context。

## 八、Secret 暴露或操作中断

### 8.1 尚未完成 Bootstrap

如果 Bootstrap Secret 或 TOTP Secret 已经截图、发送或泄露：

1. 按 `Ctrl+C` 终止 `apply`；
2. 不再使用已经显示的 Secret；
3. 开发环境在确认 IAM 没有需要保留的数据后，重建 `system` schema；
4. 重新执行 migration、`prepare` 和 `apply`，生成全新 Secret。

不要通过修改数据库 Hash、复用旧 Secret或跳过 TOTP 验证来恢复。

生产环境不得自行执行 `DROP SCHEMA`。应停止上线并按受控的 IAM 初始化恢复流程重新建立空 IAM 数据库。

### 8.2 已完成 Bootstrap

已完成后忘记密码、丢失 TOTP 或发现 TOTP Secret 泄露，不得重新运行 Bootstrap，也不要修改 `system.iam_bootstrap_state` 或数据库中的 Password Hash。三名管理员仍至少有一人可以登录时，应优先走平台身份治理和独立复核；三员凭据均不可用时，使用离线整体恢复：

```bash
make build-iam-recovery
dist/release-$(go env GOOS)-$(go env GOARCH)/addp-iam-recovery prepare
dist/release-$(go env GOOS)-$(go env GOARCH)/addp-iam-recovery apply
```

`prepare` 只显示一次以 `addp_ir_` 开头的 Recovery Secret。保持终端打开，不要截图、复制到聊天、写入文件或命令行历史。随后直接执行 `apply`，在隐藏提示中输入该 Secret。

`apply` 会从数据库确认三种平台角色的唯一当前持有人，并依次要求：

1. 输入并确认三个互不相同、至少 14 字符的新密码；
2. 使用认证器的“扫描二维码”入口扫描 CLI 直接显示的二维码；仅当认证器明确支持“手动输入设置密钥/TOTP”时才输入 TOTP Secret，不使用短信/邮件激活入口；
3. 输入认证器当前显示的 6 位验证码；
4. 等验证码变化后输入紧邻的下一个 6 位验证码。

三个账号全部验证成功后才会在一个事务中替换凭据、撤销旧会话并完成恢复。任一步失败都不会留下部分恢复结果。恢复完成后应全量重启 ADDP，再分别验证三员 Browser `platform + AAL2` 登录。仓库已由 `common-python` wheel 发布正式 `addp` 命令入口；版本发布前还必须运行 `make test-release RELEASE_SUITE=common-python-cli`，验证 OAuth 登录、Context 绑定、Keychain、刷新轮换和撤销。三员 Platform Context 的在线人工验收仍要求 AAL2，不能用 CLI 的通用 Tenant E2E 替代。

若 `prepare` 报告三员角色持有人缺失、重复、已暂停或账号已禁用，不得使用 SQL 临时修补后继续。该状态已经超出凭据恢复边界，需要先按身份与授权治理流程处理。

## 九、常见问题

### 验证码为什么一直无效？

确认输入的是认证器显示的 6 位数字，不是 TOTP Secret；然后确认设备时间已开启自动同步。

### 为什么需要连续两个验证码？

用于确认认证器已经正确保存 Secret，并验证连续时间窗口而非偶然获得一个验证码。

### 为什么不能只创建一个超级管理员？

ADDP 采用系统管理员、安全管理员和审计管理员三权分立，不保留拥有全部最高权限的 SuperAdmin。

### 为什么三个管理员最后才一起写入？

Bootstrap 使用单事务提交。任意一个账号的密码或 TOTP 验证失败时，不会留下半套管理员和不完整授权事实。
