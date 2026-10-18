# 内置格式：CAD

[返回内置格式规范](../addp内置数据类型与文件格式规范.md) · [通用写入与消费约束](../addp内置数据类型与文件格式规范.md#通用写入与消费约束)

DWG / DXF 的图纸身份、头部扫描、浏览器预览与转换边界。

阅读导航：[DWG / DXF](#dwg--dxf)。

## DWG / DXF

### 识别与组织

| 维度 | 取值 |
|---|---|
| `layout` | `single` |
| `data_type` | `cad` |
| `format` | `dwg` 或 `dxf` |
| 主资源 | `meta_item.full_name` 指向 `.dwg` 或 `.dxf` 文件或对象 |

Basic scan 按已注册扩展名或 MIME 保存基础格式候选；deep scan 再读取文件头确认，且两者都不依赖 SuperMap。DWG 通过六字节 `AC10xx` 版本头确认；DXF 通过 ASCII `0/SECTION` 结构头或 `AutoCAD Binary DXF` 签名确认。扩展名存在但 header 不合法时，deep scan 必须降级为 `unknown`，但不得丢弃 DataItem。

### attributes 写入

| 分区 | 写入内容 |
|---|---|
| `item` | `layout=single`、`data_type=cad`、`format=dwg\|dxf` |
| `type_info.cad` | 当前写入 `drawing_kind=2d` 与文件大小；只有后续独立、已规范化的结构解析能力才能增加单位、entity/layer/layout/block/xref 数量、model/paper space 或范围 |
| `format_info.dwg` | 文件头中的 `format_version=AC10xx` |
| `format_info.dxf` | ASCII DXF header 中可读取时写入 `$ACADVER`；Binary DXF 无可确认版本时省略 |
| `capabilities.spatial` | 仅在 DWG 已有明确 CRS / 空间定位事实时写入；本地 CAD 坐标不得伪造 EPSG |

### 消费要求

Meta deep scan 通过存储引擎的受控 content reader 读取最多 8 KiB 文件头，确认格式并写入版本和最小 CAD 类型事实。扫描不得调用渲染引擎、工作流算子或要求 SuperMap 在线；结构摘要不完整也不得导致源文件从 DataItem 目录消失。

Manager 基础预览通过受控 `storage-stream` 返回完整 DWG/DXF 源文件，前端按需加载 LibreDWG Worker/WASM 并通过 WebGL 绘制。不得生成 Manager CAD 栅格瓦片、manifest 或预览任务，也不得把 entity 转为 WKB/GeoJSON 作为第二预览路线。Transfer 只允许 encoded raw copy。显式 `cad.import` 的 GIS 输出登记为新的 table item，不修改源 CAD item。

### 格式约束

- 当前只承诺二维 DWG、DXF。
- Xref 只允许同目录或同 object prefix 的相对引用；拒绝绝对路径、网络路径和越权引用。
- SHX/TTF 只从平台受控目录加载。
- entity-as-row 只是读取投影，不得把源 DWG、DXF 归为 `table`。
- 预览交互状态和 CAD→GIS 转换报告不得写入 `type_info.cad` 或 `format_info.<cad-format>`。
