# ADDP Manager 内容预览插件扩展构想

状态：第三方预览插件的声明式扩展尚待确认；现有内置预览已落地，不能按本构想再建另一条路由。

现行职责与接口见[Manager 预览实现规范](../../manager/docs/数据预览与资源树实现规范.md)、[数据预览语义协议](../../manager/docs/数据预览语义协议.md)、[类型与格式能力规范](../spec/addp数据类型与格式能力规范.md)及[元数据扫描规范](../spec/addp元数据扫描机制规范.md)。表格 single/multi/whole、refs、deep 补齐、当前 kind/material/renderer、刷新与错误语义由这些正式文档维护，不在此复制 DTO 或已完成阶段。

## 未确认的扩展范围

目标是在有第三方扩展需求时，声明如何将已识别 data item 组合为 Manager 预览材料。它不替代 Meta 识别、Common Format Provider、ContentIO 或现有 PreviewRegistry。

以下只是候选描述，当前未实现，不是可直接安装的 manifest：

```json
{
  "id": "vendor-table-preview",
  "version": "0.1.0",
  "preview": {
    "match": {"data_type": ["table"], "layout": ["single", "multi", "whole"]},
    "priority": 100,
    "input": {"requires_resource_reader": true},
    "output": {"mode": "table", "stream": false, "paging": true}
  }
}
```

## 实施前决策

1. 是否确有第三方动态加载需求；代码注册已能满足时不新增 manifest 或插件管理实体。
2. 声明如何映射现有预览材料/renderer 和 command 输入 Schema，不引入平行 kind/分页协议。
3. 同一标准匹配结果的冲突、优先级、启停和失败诊断由谁裁决；不能按扩展名或优先级覆盖 Meta 事实。
4. 大文件 stream/paging/tile 的声明如何与实际内容读取能力、预算及授权校验一致。
5. 与[第三方属性扩展](addp第三方插件扩展声明构想.md)及[能力发现](addpRegistry与能力发现层构想.md)的关联边界。

确认后先修订正式预览与扩展规范，再沿现有 Manager/Common owner 门禁验证；未落地前不注册占位插件。完成内容迁入规范并从本构想移除。
