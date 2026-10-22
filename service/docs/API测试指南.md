# Service 模块 API 测试指南

本指南覆盖查询/要素协议、二维瓦片、注册服务与代理的测试方法。当前 API 以[Service Router](../backend/internal/api/router.go)和对应 Handler 为据；图查询等其他领域入口沿[模块说明](../CLAUDE.md)，不从本指南推断已完成全部模块验收。

## 专题导航

先完成[公共准备](#公共准备)，再选择专题；各项结论仍受[能力与验收边界](#能力与验收边界)约束。

| 阅读目的 | 文档 |
| --- | --- |
| REST 请求、格式、游标、Features 发现与失败分支 | [查询与要素协议](testing/查询与要素协议.md) |
| 发布血缘的实际同步范围、失败重试与完整链路验收 | [发布血缘同步边界](architecture/查询发布与执行.md#发布血缘同步边界) |
| WMTS 元数据、XYZ / OGC Tiles、MVT 和空瓦片 | [瓦片协议](testing/瓦片协议.md) |
| 瓦片直连、根 Nginx 与公开 HTTPS 入口及元数据链接 | [入口边界](architecture/瓦片发布与缓存.md#元数据链接与公开入口)、[公开入口验证](testing/瓦片协议.md#元数据链接与公开入口核对) |
| 注册字段、创建、详情与可清理测试配置 | [注册与元数据](testing/注册服务与代理.md#注册与元数据) |
| 注册图层 CRS、BBox、TileMatrixSet 与实际地图能力 | [注册图层能力核对](testing/注册服务与代理.md#注册图层能力核对) |
| 异步、手动、定时刷新，非原子写入与部分失败 | [元数据刷新核对](testing/注册服务与代理.md#元数据刷新核对) |
| 上游健康结果、持久状态与状态/时间回写 | [健康结果与状态回写核对](testing/注册服务与代理.md#健康结果与状态回写核对) |
| Basic、Bearer、API Key 装配与凭据保护 | [上游认证矩阵](testing/注册服务与代理.md#上游认证矩阵) |
| Service 直连、Gateway 可达性、上游响应及代理错误 | [代理读取与错误](testing/注册服务与代理.md#代理读取与错误) |
| 代理应用消息、正式审计与调用统计的区别 | [代理日志核对](testing/注册服务与代理.md#代理日志核对) |
| 标准门禁、GIS/Web 互通、性能证据与故障排查 | [验收方法与排查](testing/验收方法与排查.md) |

## 公共准备

先确认本次验收部署、当前 Gateway / Service Backend 的实际地址和已发布的测试资源。端口按标准启动输出与 `.dev-state/ports.env` 核对，Infra 状态使用 `bash scripts/infra/status.sh`；不因文档曾写固定端口就向该地址运行请求。测试或构建不要求重启；服务确需启动/重启时按[部署指南](../../docs/guide/addp部署和开发步骤.md)由用户操作。

下列 shell 示例在同一终端使用这些变量，origin 不带末尾斜杠；服务名和图层名均使用当前配置的真实程序标识，输出位于操作系统临时目录：

```bash
: "${SERVICE_API_BASE:?设置本次验收入口的完整 origin}"
: "${SERVICE_USER_TOKEN:?使用有权访问测试资源的当前 Tenant User Access Token}"
SERVICE_TEST_DIR=$(mktemp -d)
```

瓦片样例直接在 `SERVICE_API_BASE` 后拼接协议根路径；该变量只填本轮实际入口的 origin，不含 `/api/v1/service` 等管理 API 前缀。不能仅凭 Console 可访问就选用根 Nginx origin；当前协议转发缺口与直连、公开入口的证据边界沿[入口专题](architecture/瓦片发布与缓存.md#元数据链接与公开入口)及上述公开入口验证核对。

User Access Token 是 System 签发的 opaque Token，浏览器沿共享 AuthSession 在内存使用，不能采用 JWT 样例、URL Token 或 localStorage 令牌方案。管理面还须对应 `service.definition.*` 或 `service.external_registration.*` Permission；公开、私有、跨 Tenant 和 API Consumer 是不同用例，公开服务的匿名测试应省略 Authorization Header。身份规则见[统一认证规范](../../docs/spec/addp登录认证的统一要求.md)。

HTTP 示例使用 curl；读取 JSON 中真实 ID/游标的步骤使用 jq。选择有代表性的表/SQL/指标来源及空间/非空间、动态/静态瓦片和受控上游，参数、范围和格式来自发布契约。示例只给出请求方法与应核对内容，不记录现场测试通过结论。

## 能力与验收边界

按当前实现验证已挂载的协议；WFS 发布、WMS/栅格/高级样式，以及尚未接通的代理授权和统计分别作为待实现或待验收范围，沿[查询架构](architecture/查询发布与执行.md)、[瓦片架构](architecture/瓦片发布与缓存.md)、[外部服务架构](外部服务架构设计.md)核对。

查询成功与血缘同步分别验收。血缘专题所述发布器当前要求源 item 身份和依赖摘要，正常覆盖表来源；SQL、联邦 SQL、analytical 的执行依赖不能视为已同步的 `serve` 边。按[标准入口与证据](testing/验收方法与排查.md#标准入口与证据)分别核对 Service 投递与失败重放、Meta 当前投影与历史观察、Tenant/权限及实际页面查询；模拟 Meta 的测试、HTTP 成功、重放运行或空图均不证明完整 Service → Meta → 页面链路通过，未接通与未验证项分别列明。
