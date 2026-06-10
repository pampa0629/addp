# 资源树 locator 祖先链定位 API 专题设计

> 状态：讨论稿。本文记录资源树按 ResourceLocator 精确定位任意深度 node / item 所需的祖先链查询能力。当前只记录边界和推荐方向，不立即改代码。

## 背景

Manager、数据检索、向量化结果、MVT / Quick View 等页面都需要从一个 `locator` 回到资源树并高亮目标资源。

当前前端已经统一消费 ResourceLocator，不再自行拼接 `engine_id / bucket / path` 等旧路径。但资源树是懒加载的：如果目标资源很深，前端只有目标 locator 时，无法稳定知道从根节点到目标节点之间每一级祖先是否已经加载、应该展开哪个节点、每一级展示名和节点类型是什么。

因此需要一个面向资源树定位的后端 API：输入目标 locator，返回从 catalog root 到目标资源的祖先链。

## 问题

### 1. locator 能定位目标，但不能直接展开树

示例：

```text
addp://engine/1/path/bucket/a/b/c/report.pdf?type=object&item_id=123
```

这个 locator 能表达目标 item，但资源树要展开到目标，需要知道：

```text
engine root -> bucket -> a -> b -> c -> report.pdf
```

前端如果自己按 `/` 拆 path，会把 engine catalog 模型、Meta node / item 身份、NFS / MinIO 差异和展示名规则混在一起。

### 2. 当前定位只适合已加载或浅层节点

前端可以在已经加载的节点中查找目标，也可以先展开根节点再找一层或两层。但对任意深度目录、prefix、bucket 下对象、NFS 文件路径，缺少稳定的“祖先链事实源”。

### 3. locator 不应作为用户理解对象暴露

locator 是内部定位契约，适合 API、跳转和树定位。界面上应展示引擎名、资源路径、节点名和操作按钮，而不是要求用户理解 locator URI。

## 目标

1. 支持根据标准 ResourceLocator 精确返回任意深度资源的祖先链。
2. 支持 node 和 item 两类目标。
3. 支持 MinIO / S3 / NFS / NAS 等统一 engine + format 分层后的资源树。
4. 前端可基于祖先链逐级展开树并高亮目标。
5. API 不触发扫描、不写 Meta attributes、不创建派生产物。

## 非目标

1. 不替代资源树 children 查询 API。
2. 不把 locator 展示为用户操作字段。
3. 不在前端重新实现 engine catalog path 解析。
4. 不为未扫描、未落库的资源隐式创建 Meta node / item。
5. 不解决 cleanup 或 artifact lifecycle。

## 推荐 API 形态

建议放在 Manager 资源树 API 下：

```http
GET /api/v1/manager/tree/{engine_id}/ancestors?locator={locator}
```

也可以命名为 `resolve-path` 或 `locate`，但语义应明确是“为资源树展开返回祖先链”，不是读取内容或刷新扫描。

### 请求约束

| 参数 | 说明 |
| --- | --- |
| `engine_id` | 路径参数，用于租户权限和 engine 边界校验。 |
| `locator` | 标准 ResourceLocator URI，必须与 `engine_id` 匹配。 |

### 响应草案

```json
{
  "engine_id": 1,
  "target_locator": "addp://engine/1/path/bucket/a/b/c/report.pdf?type=object&item_id=123",
  "target_kind": "item",
  "ancestors": [
    {
      "locator": "addp://engine/1/path/?type=service",
      "type": "service",
      "label": "对象存储",
      "node_id": 10,
      "item_id": null,
      "has_children": true
    },
    {
      "locator": "addp://engine/1/path/bucket?type=bucket&node_id=11",
      "type": "bucket",
      "label": "bucket",
      "node_id": 11,
      "item_id": null,
      "has_children": true
    },
    {
      "locator": "addp://engine/1/path/bucket/a/b/c/report.pdf?type=object&item_id=123",
      "type": "object",
      "label": "report.pdf",
      "node_id": null,
      "item_id": 123,
      "has_children": false
    }
  ]
}
```

约束：

- `ancestors` 必须按根到目标排序。
- 最后一项必须对应 `target_locator`。
- node 目标最后一项带 `node_id`，item 目标最后一项带 `item_id`。
- 每项 locator 必须是标准 ResourceLocator，供前端作为 tree node key。
- 展示字段可包含 `label/type/has_children`，但不应返回 engine 凭据或 provider 私有连接信息。

## 后端解析原则

1. 先解析 locator，校验 engine、type、`node_id` / `item_id`。
2. 如果 locator 已携带 `item_id`，优先回查 Meta item，并基于当前 `full_name` 和 item_type 生成目标 locator。
3. 如果 locator 已携带 `node_id`，优先回查 Meta node，并基于当前 node path 生成目标 locator。
4. 对祖先链，优先使用 Meta 已落库 node 关系，不触发扫描。
5. 如果某一级祖先未落库，应返回明确错误或部分链状态，不隐式创建节点。
6. engine catalog 模型差异由后端统一处理，前端只消费返回链。

## 前端消费方式

前端定位流程：

1. 从检索结果、向量化结果或任务详情拿到 locator。
2. 调用 ancestors API。
3. 按 `ancestors[].locator` 逐级展开资源树。
4. 若某一级 children 未加载，则调用现有 children API 加载该级。
5. 展开完成后设置 current node key 为目标 locator。

界面只展示“定位”按钮、资源路径和资源名，不展示 locator URI。

## 与现有专题关系

- 向量化结果必须返回 locator；本 API 让结果页“定位”可以覆盖任意深度资源。
- 数据检索命中以 data item 为对象；本 API 让检索结果稳定回到资源树。
- cleanup / artifact lifecycle 不依赖本 API。
- 本 API 属于 Manager Explorer / ResourceTree 通用能力，不应做成 embedding 私有接口。

## 待确认问题

1. API 命名采用 `ancestors`、`locate` 还是 `resolve-path`。
2. 部分祖先缺失时，是返回 404，还是返回 `resolved=false` 与缺失层级。
3. 是否需要同时返回 sibling children，减少前端逐级 children 查询次数。
4. 是否将该能力下沉为 common resource tree helper，供其他模块复用 DTO。
5. 是否需要在 Swagger 中把 `locator` 参数和响应结构沉淀为 Manager 资源树标准契约。

## 后续实施建议

1. 先补 Manager Explorer API 与 Swagger。
2. 增加 MetaClient 查询 node / item 祖先链的正式方法，避免 Manager 直接拼 Meta SQL。
3. 前端 `ResourceTree` 增加 `reveal(locator)` 或等价组合能力。
4. 数据检索、向量化结果、MVT / Quick View 统一使用该定位能力。
5. 增加 MinIO 深层 prefix、NFS 深层目录、item locator、node locator 四类最小验证用例。
