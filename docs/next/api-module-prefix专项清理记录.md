# Service 发布服务访问路径待决事项

状态：未确认的路径清理提案。管理 API 的正式规则见[API 设计规范](../spec/addp-API设计规范.md)与[Swagger 集成指南](../spec/addp-Swagger集成指南.md)，发布服务的实际路径以 Service 当前路由、Swagger 及 Gateway 契约为准。本文不宣布新路径已生效。

## 已确认边界

模块管理 API 使用 `/api/v1/{module}/...`，根健康检查使用 `/health/live` 与 `/health/ready`；明确的兼容别名删除，不保留双轨。Service 发布的数据访问 URL 和 OGC/WMTS/XYZ 协议入口需按访问语义单独决策，不能机械替换为管理 API。

## 实施前必须确认

1. 发布查询、图查询和注册服务代理是否统一进入一个 Service 公开命名空间；候选 `/api/v1/service/public/...` 尚未确认，不作为规范或占位路由。
2. 已发布服务访问 URL 的切换范围与外部调用影响，公开/私有访问的身份和授权保持由 Service 裁决。
3. OGC/WMTS/XYZ 的协议入口边界；不因字符串前缀相似认定为旧兼容路由。

历史盘点中的 client baseURL 和 endpoint 不一致仅是调查线索，不能当作当前已确认缺陷。实施前重新核对 Service router、endpoint 生成、前端测试/复制链接和 Gateway 特殊转发，再决定具体改动。

## 确认后的唯一实施范围

先修订 API 规范、Gateway 架构说明和 Service 文档，再同批修改 Backend、Gateway、endpoint 生成、Frontend 与调用文档，删除被替换的旧路径。公开测试与协议路径的 client 必须使用匹配的 baseURL，不拼接管理前缀。

按实际影响执行 Service Swagger 生成和覆盖校验、Service/Gateway owner 门禁及前端验证：

```bash
bash scripts/swagger/gen-swagger.sh service
bash scripts/swagger/check-route-coverage.sh service
make test-module MODULE=service
make test-module MODULE=gateway
```

完成确认、唯一主路径切换及对应门禁后删除本专题；不保留历史处理清单或兼容路由。
