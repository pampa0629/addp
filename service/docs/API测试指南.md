# Service 模块 API 测试指南

本指南覆盖查询/要素协议、二维瓦片、注册服务与代理的测试方法。当前 API 以[Service Router](../backend/internal/api/router.go)和对应 Handler 为据；图查询等其他领域入口沿[模块说明](../CLAUDE.md)，不从本指南推断已完成全部模块验收。

## 专题导航

| 阅读目的 | 文档 |
| --- | --- |
| REST 请求、格式、游标、Features 发现与失败分支 | [查询与要素协议](testing/查询与要素协议.md) |
| WMTS 元数据、XYZ / OGC Tiles、MVT 和空瓦片 | [瓦片协议](testing/瓦片协议.md) |
| 注册字段、上游认证、代理链路与访问边界 | [注册服务与代理](testing/注册服务与代理.md) |
| 标准门禁、GIS/Web 互通、性能证据与故障排查 | [验收方法与排查](testing/验收方法与排查.md) |

## 公共准备

先确认本次验收部署、当前 Gateway / Service Backend 的实际地址和已发布的测试资源。端口按标准启动输出与 `.dev-state/ports.env` 核对，Infra 状态使用 `bash scripts/infra/status.sh`；不因文档曾写固定端口就向该地址运行请求。测试或构建不要求重启；服务确需启动/重启时按[部署指南](../../docs/guide/addp部署和开发步骤.md)由用户操作。

下列 shell 示例在同一终端使用这些变量，origin 不带末尾斜杠；服务名和图层名均使用当前配置的真实程序标识，输出位于操作系统临时目录：

```bash
: "${SERVICE_API_BASE:?设置本次验收入口的完整 origin}"
: "${SERVICE_USER_TOKEN:?使用有权访问测试资源的当前 Tenant User Access Token}"
SERVICE_TEST_DIR=$(mktemp -d)
```

User Access Token 是 System 签发的 opaque Token，浏览器沿共享 AuthSession 在内存使用，不能采用 JWT 样例、URL Token 或 localStorage 令牌方案。管理面还须对应 `service.definition.*` 或 `service.external_registration.*` Permission；公开、私有、跨 Tenant 和 API Consumer 是不同用例，公开服务的匿名测试应省略 Authorization Header。身份规则见[统一认证规范](../../docs/spec/addp登录认证的统一要求.md)。

HTTP 示例使用 curl；读取 JSON 中真实 ID/游标的步骤使用 jq。选择有代表性的表/SQL/指标来源及空间/非空间、动态/静态瓦片和受控上游，参数、范围和格式来自发布契约。示例只给出请求方法与应核对内容，不记录现场测试通过结论。

## 能力与验收边界

按当前实现验证已挂载的协议；WFS 发布、WMS/栅格/高级样式，以及尚未接通的代理授权和统计分别作为待实现或待验收范围，沿[查询架构](architecture/查询发布与执行.md)、[瓦片架构](architecture/瓦片发布与缓存.md)、[外部服务架构](外部服务架构设计.md)核对。
