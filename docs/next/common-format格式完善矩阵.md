# common/format 后续事项与验收边界

格式身份、组织、attributes 和各格式已开放读取/转换能力唯一维护于[内置格式规范](../spec/addp内置数据类型与文件格式规范.md)、[类型与格式能力规范](../spec/addp数据类型与格式能力规范.md)及实际 Owner 文档。本文只跟踪缺口，不保存页面快照、依赖调研往返、逐轮 CI、重启或样例哈希流水。

## 当前优先缺口

1. SGM 的静态几何、基础 RGB/alpha 贴图、同目录/Textures 精确解析已进入正式规范，并有本地真实样例和隔离技术验收；仍缺合法 SDK/许可环境中的正式 Linux Runner T4。私有厂商样例不上传公开 Hosted，不能用普通格式 suite 或单样例成功覆盖全部材质/版本。
2. 正常彩色 FBX 基础预览已有本地真实页面证据；固定盒子源声明 IndexToDirect 却缺颜色索引，仍不作为基础预览成功。已有 GLB 快显不等于基础解析缺陷已修复。Three.js 升级仍需 CAD/Giro3D 等消费者共同满足版本约束，不能强行 override、私有 FBXLoader 或第二份 Three.js。
3. 新增 FBX／known-item refresh、KML／GML／TopoJSON 的 S3 兼容消费，以及 CSV／TSV 索引源变化后的拒绝与刷新恢复，仍须收口当前完整 Manager Hosted 复验。CSV／TSV 源版本保护已实现，NFS 已有真实阶段证据；同大小改写 MinIO 对象后的 ETag、两页拒绝、下载、刷新重绑和原字节还原仍待对应四案例完整证据。TopoJSON 未知 CRS 的零值字段按 HTTP 契约省略，验收不得据此虚构 EPSG 身份。候选代码、局部阶段通过或已启动的 Hosted 轮次均不能计为完整 T4；逐项范围按现有 Manager 场景规范验收，MinIO 不代表外部云 S3 认证。

静态轨迹及设备/时间范围读取已完成 CSV、records JSON、Point GeoJSON 三源 Hosted 与本地真实样例，不再列为待实现；每个所选范围仍受 2000 点限制，不自动包含轨迹回放。XLSX、3DS/DAE、SGM 等成功只覆盖各自报告范围；已解决的 Assimp 文件存在性缺陷、SGM 秋树丢图/alpha 不继续当当前故障。

## 已有格式的后续覆盖

| 格式/能力 | 尚需完善或验证 |
| --- | --- |
| CSV / TSV | `ParseOptions.HasHeader=false` 已存在，但共享 parser 的字段识别、样本与连续读取仍把首行作为表头，须修复首行丢失并核对稀疏索引；Meta／Manager 尚无无表头声明入口，不能把 parser 选项当作页面支持。源大小、ETag 和修改时间全部不变的原位写入不能由索引版本校验识别，不提供不可变读取快照；UTF-32/GB18030 不由已有 UTF-16/显式 GBK 覆盖 |
| JSON / GeoJSON | 大文件、复杂嵌套数组、无/混合 geometry、WKB/EWKB 展示；静态轨迹三源的完成不代替这些组合 |
| Parquet | schema 不兼容诊断、row group 性能及有界 footer/range 读取 |
| Shapefile | NFS/MinIO/嵌套 ZIP 的 refs 归并、物化 fallback 的 shx 分页、未支持 shape 诊断 |
| Excel | 大文件与复杂工作簿；公式计算、宏和原样图表不属于现有工作表预览 |
| SQLite / GeoPackage | 真实多表/layer 切换、分页、只读失败与大库成本；child 字段不得误用父容器摘要 |
| ZIP | entry 截断、逐层嵌套、动态识别不回写 Meta，大包及远程 range-aware 读取 |
| Text / Markdown | 编码、大文件截断、安全渲染、链接/代码块和性能 |
| 图片 | 各编码变体、异常/预算提示与真实样例；已实现 EXIF/曝光/动画摘要不代表全部相机或媒体解析 |
| PDF | 加密诊断、真实 metadata、raw/range 与大文件传输；正文 extraction 另行确认 |
| DOC / DOCX / PPTX / WPS | DOCX 修订/复杂关系、PPTX 母版/隐藏页、大文件上限及格式变体；DOC descriptor 不等于文本 reader |
| 容器/特殊结构 | 可通用逻辑上移 format provider/dataitem/datatype；嵌套 child、动态预览与静态能力分别裁决 |

## 后续能力准入

- ORC、Avro：现有 descriptor 只证明身份；table info/sample/reader/writer 与真实一致性未实现前不开放读取。
- SVG/AVIF/HEIC：明确安全渲染、解码依赖、MediaInfoProvider 和真实端到端边界。
- Video/Audio：固定 type_info.media 元信息与受控内容通道；URL 可播放不等于后端解析全部容器。
- 三维、点云及高斯扩展见[专门待办](三维与点云后续路线.md)，不复制另一份格式矩阵。

## 验证与维护

`ListFormatCapabilitySnapshots()` 的静态声明与当前进程接口实现保持一致；raw/range 是 engine/contentio/URL 内容通道，不登记为 Go format reader。每次格式完成同步删除 Meta/Manager/Transfer 的重复 format/engine 分支。unknown 只处理剩余二进制，文本识别沿唯一 LooksLikeTextContent 路径。

标准入口按 Common、Meta、Manager、Runtime 实际变更选择根 Makefile，Hosted 使用既有 Manager suite 及对应许可环境，文档整理不计作新的产品验收。已实现规则回写正式规范，最终证据与未完成项原位更新，历史失败/中止不改记通过。
