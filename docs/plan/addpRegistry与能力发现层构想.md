# ADDP 跨 Registry 能力发现构想

状态：跨 Registry 的统一汇总视图尚待确认。现有 FormatDescriptor、接口实现状态及冲突诊断已落地，以[类型与格式能力规范](../spec/addp数据类型与格式能力规范.md#能力诊断快照)为准；不将其写成尚待建设，也不将诊断快照作为可执行能力或业务事实源。

## 候选范围

engine、dataitem、format 和 preview registry 各自维护唯一职责，扩展 Transfer 继续走 planner/executor 与 Common Provider，不恢复旧 ConnectorRegistry。仅在确有跨层诊断需求时，讨论一个只读汇总视图；它不替代或合并原 registry，也不单独声明执行授权。

候选输出尚未定稿：

```json
{
  "plugin_id": "builtin-shapefile",
  "detects": [{"layout": "multi", "data_type": "table", "format": "shapefile"}],
  "parses": ["type_info.table", "format_info.shapefile", "capabilities.spatial"],
  "previews": ["table", "table.spatial"],
  "transfers": ["read", "export"]
}
```

上述成员不证明当前进程加载了对应 reader/writer，也不证明某个 Engine Instance 可用；执行仍查询具体 Provider。

## 实施前决策

1. 是否需要超出现有格式诊断的汇总、管理 UI 或持久化；不先建中心能力数据库。
2. 加载顺序、冲突与来源版本如何追溯；每个事实继续引用其原 registry。
3. 一个扩展同时注册 detector/format/preview 时是否需要原子启停及失败回滚。
4. Meta 是否需要冻结“扫描使用的能力版本”，其来源、存储、成本及查询用途是什么。

相关插件声明见[第三方扩展构想](addp第三方插件扩展声明构想.md)。决定进入实现后先更新概念与规范，并同步实际 owner 的门禁及 CI；不提前登记空框架。
