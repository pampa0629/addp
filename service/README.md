# Service 数据服务模块

Service 负责查询、图查询和瓦片服务发布，以及第三方服务注册与代理。服务定义和最终业务授权归 Service；Engine、身份与注册表等共享职责见[模块契约](CLAUDE.md)。

## 📖 文档说明

本页提供启动、验证和专题导航，具体契约在对应文档维护：

| 阅读目的 | 入口 |
| --- | --- |
| 对象职责、查询来源、指标修订与瓦片架构 | [核心概念与架构](docs/service核心概念和架构设计.md) |
| 当前 API、Permission 与开发约束 | [模块契约](CLAUDE.md#核心-api) |
| Console 页面、注册表单与模块协作 | [管理界面专题](docs/architecture/管理界面与模块协作.md) |
| 第三方注册、代理、直连与未闭合能力 | [外部服务架构](docs/外部服务架构设计.md) |
| 查询/要素、瓦片、注册代理的验证方法 | [API 测试指南](docs/API测试指南.md) |
| 浏览器直连：原始端点、上游认证、CORS 与实际客户端验收 | [原始端点与浏览器直连核对](docs/testing/注册服务与代理.md#原始端点与浏览器直连核对) |
| 注册代理：Backend/Gateway 入口、消费授权与上游响应验证 | [代理读取与错误](docs/testing/注册服务与代理.md#代理读取与错误) |
| 标准门禁、运行证据与排障 | [验收方法与排查](docs/testing/验收方法与排查.md) |

## 🎯 核心功能

- 查询服务支持表、SQL、联邦及确定指标修订等来源；协议、输出契约和授权范围见[查询发布与执行](docs/architecture/查询发布与执行.md)。
- 瓦片服务维护动态/静态图层与发布配置，使用方式见[瓦片指南](docs/瓦片服务使用指南.md)。
- 第三方注册保存上游端点、认证配置及能力解析值。创建、刷新、健康结果和代理消费分别成立，不从保存成功推导图层完整或消费授权已接通，详见[注册服务流程](docs/数据库架构.md#写入与读取流程)。

## 🚀 快速开始

### 方式 1: 开发模式（推荐）

先按[部署与开发指南](../docs/guide/addp部署和开发步骤.md)准备根配置和依赖，再从项目根目录执行：

```bash
# 确认实际 Infra 状态与端口，再启动所需设施
bash scripts/infra/status.sh
bash scripts/infra/up.sh

# 启动 Service 及脚本选择的依赖
bash scripts/dev/start.sh -service
```

启动范围沿[模块选择指南](../scripts/docs/模块选择启动指南.md)，Engine Runtime 不随 `-service` 隐式启动；业务需要的 Runtime 须显式选择并满足对应引擎要求。页面从 Console 进入，实际访问地址以标准脚本输出和 `.dev-state/ports.env` 为准，端口规则见[端口规范](../docs/spec/addp端口分配.md)。

需要应用代码变更时，由用户在自己的终端执行 `./scripts/dev/restart.sh -service`；仅测试或构建无需重启，预检、失败与进程归属边界沿[开发生命周期说明](../scripts/dev/README.md)。

### 方式 2: Docker 部署

容器部署使用根应用栈和已构建镜像；从项目根目录按场景选择标准入口，不在模块目录单独启动 Compose：

| 场景 | 入口与说明 |
| --- | --- |
| 本地容器部署 | `bash scripts/local/start.sh`，准备与生命周期见[Local 指南](../scripts/local/README.md) |
| 生产部署 | `bash scripts/prod/start.sh`，初始化、公开地址与发布流程见[Prod 指南](../scripts/prod/README.md) |

镜像平台、Registry 与版本按[Build 指南](../scripts/build/README.md#build-imagessh)准备；依赖、启动范围和健康检查沿上述入口，不从容器启动成功推导业务链路已验收。

## 📋 主要 API 端点

管理 API 前缀为 `/api/v1/service`，具体方法、路径与 Permission 以[模块契约](CLAUDE.md#核心-api)及 [Service Router](backend/internal/api/router.go)为准。查询/要素、瓦片与注册服务请求统一沿[API 测试指南](docs/API测试指南.md)，不在本页复制请求模板。

注册公开代理单独挂载 `ANY /api/service/registered/proxy/:id/*path`；当前 Gateway 未转发该路径，DTO 中的代理地址不证明统一入口可达，也不继承管理 API 的 Tenant 和 Permission 保证。Backend 直连代理与统一入口的验证见[代理读取与错误](docs/testing/注册服务与代理.md#代理读取与错误)。

## 🏗️ 数据库设计

Service 使用 PostgreSQL 的 `service` schema。完整模型、表清单和迁移归属见[数据库架构](docs/数据库架构.md#模块与表清单)；第三方注册对应：

| 表 | 说明 |
| --- | --- |
| `service.registered_services` | [第三方端点、认证配置、健康与持久状态](docs/tables/registered_services表.md) |
| `service.registered_service_layers` | [上游解析的图层、CRS、BBox 与 TileMatrixSet 边界](docs/tables/registered_service_layers表.md) |

## 🔐 安全特性

- **管理认证**：注册管理使用 Bearer Tenant AuthContext 与精确 Permission；公开代理是独立边界，不能推导所有入口已登录认证或按租户隔离，见[访问边界](docs/数据库架构.md#管理面与公开代理)。
- **上游凭据**：Basic、Bearer、API Key 用于向第三方装配认证，不授予调用者消费权；参数与更新语义见[上游认证配置](docs/数据库架构.md#上游认证配置)。
- **凭据保护**：DTO 不返回 auth_config 正文，但 JSONB 存储尚无字段加密；has_auth_config 只表示配置非空。TLS、重定向及 URL/响应/日志保护须按[认证矩阵](docs/testing/注册服务与代理.md#上游认证矩阵)分别验证。

## 🐛 常见问题

- 注册第三方 OGC 服务、刷新能力和验证上游认证：沿[注册服务与代理](docs/testing/注册服务与代理.md)，使用可清理配置及受控上游，不复制旧字段或固定端口样例。
- 手动健康检查：先读[健康检查与定时刷新](docs/数据库架构.md#健康检查与定时刷新)，再按[健康结果与状态回写核对](docs/testing/注册服务与代理.md#健康结果与状态回写核对)区分上游结果、数据库回写和公开代理消费。

## 验证

从项目根目录运行 Service 标准门禁：

```bash
make test-module MODULE=service
```

该入口选择已登记的 T0–T3；快速 Go T1、PostgreSQL 集成、前端及跨模块运行的范围见[验收方法与排查](docs/testing/验收方法与排查.md#标准入口与证据)，层级与 CI 要求沿[测试与验收规范](../docs/spec/addp测试与验收规范.md)。健康就绪、HTTP 200、字段回读或模拟上游成功不代替真实授权、地图与完整业务链路验收；未运行、失败或跳过项分别记录，不计为通过。

## 📚 相关文档

- [开发原则](../docs/spec/addp开发原则.md)：开发与唯一实现约束。
- [API 设计规范](../docs/spec/addp-API设计规范.md)、[Swagger 指南](../docs/spec/addp-Swagger集成指南.md)：API 与接口文档维护。
- [配置规范](../docs/spec/addp配置介绍.md)、[技术栈规约](../docs/spec/addp技术栈规约.md)：配置归属与依赖版本。
- [统一认证规范](../docs/spec/addp登录认证的统一要求.md)：身份、凭据与日志保护。
