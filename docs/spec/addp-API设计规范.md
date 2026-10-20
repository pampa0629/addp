# ADDP API 设计规范

适用范围：ADDP 全平台所有模块的 HTTP API。

---

## 一、总则

本规范旨在统一 ADDP 全域数据平台各模块的 API 设计风格，提升接口的一致性、可维护性和易用性。

### 1.1 设计原则

1. **统一性** - 所有模块遵循统一的响应格式、命名规范和错误处理
2. **简洁性** - 优先采用简单直观的设计，避免过度工程化
3. **RESTful** - 遵循 REST 架构风格，使用标准 HTTP 方法和语义
4. **版本化演进** - 通过统一版本前缀管理接口契约，避免隐式旧路径
5. **开发友好** - 清晰的文档、统一的错误码、便于调试

### 1.2 适用场景

- ✅ 所有面向前端的 HTTP API（Console、各模块前端）
- ✅ 模块间的 HTTP API 调用
- ✅ 对外开放的 API（如数据服务 API）
- ⚠️  内部 gRPC/消息队列等非 HTTP 通信可参考但不强制

---

## 专题导航

| 阅读目的 | 文档 |
| --- | --- |
| 成功响应、错误码、精确数值与 HTTP 状态码 | [响应与错误](api/响应与错误.md) |
| REST 资源、动作、版本前缀与命名 | [资源路由与命名](api/资源路由与命名.md) |
| 管理分页、业务 cursor、过滤、排序与搜索 | [分页与查询](api/分页与查询.md) |
| version / revision、批量写入与冲突交互 | [资源版本与并发更新](api/资源版本与并发更新.md) |
| 用户、服务、消费凭据、浏览器 Ticket 与 Agent 委托 | [认证授权](api/认证授权.md) |
| 健康端点、节点台账、告警与日志专用接口 | [健康与平台控制面](api/健康与平台控制面.md) |
| Swagger、后端分层、共享响应与实施要求 | [文档与实施](api/文档与实施.md) |
| 共享前端与 Python HTTP Client | [客户端集成](api/客户端集成.md) |
| x-ai-hint、HTTP 错误重试提示 | [AI Agent 友好性](api/AI-Agent友好性.md) |

规则正文按专题维护，方法、响应和命名速查表归入对应专题。新增接口先核对响应、路由、身份和资源版本，再按[实施要求](api/文档与实施.md#新接口开发)同步调用方与 Swagger。

## 参考资料

- [Google API Design Guide](https://cloud.google.com/apis/design)
- [Microsoft REST API Guidelines](https://github.com/microsoft/api-guidelines)
- [RESTful API Best Practices](https://restfulapi.net/)
- [HTTP 状态码 RFC 9110](https://www.rfc-editor.org/rfc/rfc9110.html)

---
