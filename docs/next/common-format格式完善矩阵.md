# common/format 后续事项与验收边界

格式身份、组织、attributes 和各格式已开放读取/转换能力唯一维护于[内置格式规范](../spec/addp内置数据类型与文件格式规范.md)、[类型与格式能力规范](../spec/addp数据类型与格式能力规范.md)及实际 Owner 文档。本文只跟踪缺口，不保存页面快照、依赖调研往返、逐轮 CI、重启或样例哈希流水。

## 当前优先缺口

1. SGM 的静态几何、基础 RGB/alpha 贴图、同目录/Textures 精确解析已进入正式规范，并有本地真实样例和隔离技术验收；仍缺合法 SDK/许可环境中的正式 Linux Runner T4。私有厂商样例不上传公开 Hosted，不能用普通格式 suite 或单样例成功覆盖全部材质/版本。
2. 正常彩色 FBX 基础预览已有本地真实页面证据；固定盒子源声明 IndexToDirect 却缺颜色索引，仍不作为基础预览成功。已有 GLB 快显不等于基础解析缺陷已修复。Three.js 升级仍需 CAD/Giro3D 等消费者共同满足版本约束，不能强行 override、私有 FBXLoader 或第二份 Three.js。

2026-10-24 用户要求 MrSID 与 ECW 暂缓，不联系厂商。非商业 SDK 路线优先按以下缺口推进；识别、读取、转换快显和目标格式导出必须分别验收：

| 优先范围 | 当前缺口与推进边界 |
| --- | --- |
| 通用表格读取 | Avro 已从 descriptor 补为单文件 OCF 读取，复杂 schema、writer 与正式运行验收仍缺；ORC 仍只有 descriptor，后续补实际表格接口 |
| 清单 Excel 206 / B205 二维矢量导出 | KML 已补单文件二维简单要素 writer 和标准导出能力发现，复用固定 CRS 约束及既有空间转换；本地 writer／Transfer／Manager 与 PostGIS、LIBKML 独立验证见内置规范。尚无该出口的正式页面／Hosted 证据；复杂字段、Z/M、样式／KMZ不在首期范围，现有 KML 读侧对扩展属性和空几何的限制仍保留。其余 DWG/VCT/UDB 等目标不由 KML 覆盖 |
| 清单 Excel 207 / B206 栅格导出 | IMG 的 HFA 读取和 COG 快显已有限定验收，IMG、PNG/JPG 的目标编码／保存链仍缺；需先明确原生波段、NoData、mask、定位及旁文件交付契约，再扩展既有持久化路径 |
| 清单 GRD / VCT / UDB | 未形成统一导出能力；GRD 后缀对应多种编码，须先明确要求的变体。VCT/UDB 先核官方格式和现有引擎能力，不据后缀宣称可用开源路线 |
| 其他扩展 | KMZ 尚无内置格式插件；SVG/AVIF/HEIC 的 descriptor 不代表完整媒体解析。当前不以这些扩展代替上述清单内缺口 |

静态轨迹及设备/时间范围读取已完成 CSV、records JSON、Point GeoJSON 三源 Hosted 与本地真实样例，不再列为待实现；每个所选范围仍受 2000 点限制，不自动包含轨迹回放。XLSX、3DS/DAE、SGM 等成功只覆盖各自报告范围；已解决的 Assimp 文件存在性缺陷、SGM 秋树丢图/alpha 不继续当当前故障。

## 已有格式的后续覆盖

| 格式/能力 | 尚需完善或验证 |
| --- | --- |
| CSV / TSV | 有／无表头声明、首条记录保留、普通刷新继承、重开、分页、模式恢复与原文件下载已有 Hosted T4 证据，见[验收证据索引](../spec/testing/验收证据索引.md#manager-预览与私有导入历史证据)。剩余边界：源大小、ETag 和修改时间全部不变的原位写入不能由索引版本校验识别，不提供不可变读取快照；UTF-32/GB18030 不由已有 UTF-16/显式 GBK 覆盖。既有 Meta 全量 PostgreSQL 入口的 Transfer schema-change 来源拒绝子例失败原记录保持，不由本轮 T4 改记通过 |
| JSON / GeoJSON | 空/混合 geometry 的扫描与跨批字段可空声明、JSON 空值同名列归并已有 Common Go T1；四份 NFS 样例的本机页面已验证可空字段、20＋5 分页、全空集合与 WKB 临时 CRS 地图，两类原文件下载哈希不变，见[验收证据索引](../spec/testing/验收证据索引.md#manager-预览与私有导入历史证据)。S3 SDK 协议夹具与 contentadapter 的元信息／分页，以及 Transfer 写前字段核验、普通 records JSON／GeoJSON 的跨批空值和 Parquet 输出已有确定性回归；动态 reader 尚无字段时复用或执行同格式 Describe，不跳过写前校验。仍缺该组合的真实对象存储／原生数据库集成和正式 Hosted T4；records JSON 尚未开放 EWKB 连续输出，另有大文件、复杂嵌套数组与更多 WKB/EWKB 变体。静态轨迹三源的完成不代替这些组合 |
| Parquet | whole scope 描述、分页和连续读取已共用 Range 打开路径；可用 Range／大小时不先下载完整文件，未提供可用大小的流输入仍需完整读取。后续包括单文件流输入成本、footer／单页预算、schema 不兼容诊断及复杂结构读取 |
| Shapefile | DBF `N` 小数字段的分页／连续读取已统一保留精确十进制文本，Manager JSON 出口有精度与空值回归；FTP／NFS 的 farmland 本机表格、地图及全 127 行精确值复验通过，范围见[验收证据索引](../spec/testing/验收证据索引.md#manager-预览与私有导入历史证据)，不替代 Hosted 或授权验收。后续覆盖 NFS/MinIO/嵌套 ZIP 的 refs 归并、物化 fallback 的 shx 分页及未支持 shape 诊断 |
| Excel | 大文件与复杂工作簿；公式计算、宏和原样图表不属于现有工作表预览 |
| SQLite / GeoPackage | GeoPackage 登记几何列的样本／连续读取已统一标准编码，默认 EWKB、保留 SRID／坐标／null，普通 BLOB 不作几何猜测；归档 snapped.gpkg 精确复现及 Manager 实际容器 child 编码回归、Common／Manager／Meta／Transfer Go T1 通过。原 Hosted 39269000076 的整体失败保留，第四地图／失败页／最终 v3 仍待拓扑负责会话按原 suite 复验。真实多表/layer 切换、只读失败与大库成本仍需扩展；child 字段不得误用父容器摘要 |
| ZIP | entry 截断、逐层嵌套、动态识别不回写 Meta，大包及远程 range-aware 读取 |
| Text / Markdown | 编码、大文件截断、安全渲染、链接/代码块和性能 |
| 图片 | 各编码变体、异常/预算提示与真实样例；已实现 EXIF/曝光/动画摘要不代表全部相机或媒体解析 |
| PDF | 加密诊断、真实 metadata、raw/range 与大文件传输；正文 extraction 另行确认 |
| DOC / DOCX / PPTX / WPS | DOCX 修订/复杂关系、PPTX 母版/隐藏页、大文件上限及格式变体；DOC descriptor 不等于文本 reader |
| 容器/特殊结构 | 可通用逻辑上移 format provider/dataitem/datatype；嵌套 child、动态预览与静态能力分别裁决 |

## 后续能力准入

- Avro：单文件 OCF 的普通标量／可空字段已补元信息、分页样本及连续读取，沿用共享格式接口；支持 null/deflate/snappy 并限制头部和块预算。复杂结构、逻辑类型、whole 合并、writer、真实存储及 Hosted 仍待补，不能将 Common T1 等同运行验收。
- ORC：现有 descriptor 只证明身份；table info/sample/reader/writer 与真实一致性未实现前不开放读取。
- SVG/AVIF/HEIC：明确安全渲染、解码依赖、MediaInfoProvider 和真实端到端边界。
- Video/Audio：固定 type_info.media 元信息与受控内容通道；URL 可播放不等于后端解析全部容器。
- 三维、点云及高斯扩展见[专门待办](三维与点云后续路线.md)，不复制另一份格式矩阵。

## 验证与维护

`ListFormatCapabilitySnapshots()` 的静态声明与当前进程接口实现保持一致；raw/range 是 engine/contentio/URL 内容通道，不登记为 Go format reader。每次格式完成同步删除 Meta/Manager/Transfer 的重复 format/engine 分支。unknown 只处理剩余二进制，文本识别沿唯一 LooksLikeTextContent 路径。

标准入口按 Common、Meta、Manager、Runtime 实际变更选择根 Makefile，Hosted 使用既有 Manager suite 及对应许可环境，文档整理不计作新的产品验收。已实现规则回写正式规范，最终证据与未完成项原位更新，历史失败/中止不改记通过。
