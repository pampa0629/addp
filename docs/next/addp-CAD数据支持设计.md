# ADDP CAD 后续路线

更新时间：2026-08-04

状态说明：二维 DWG / DXF 的格式识别、Meta 扫描、Manager 预览和 Transfer 原文件传输已经进入正式主线。本文只记录尚未实现或尚未完成端到端验证的 CAD 能力，不再重复维护现行实现和历史迁移验收。

正式文档入口：

- `docs/concepts/addp数据类型和格式体系图.md`
- `docs/spec/addp内置数据类型与文件格式规范.md`
- `docs/spec/addp元数据attributes规范.md`
- `docs/spec/addp任务体系规范.md`
- `manager/docs/数据预览语义协议.md`
- `manager/docs/快显实现规范.md`
- `manager/docs/tables/cad_previews表.md`
- `manager/docs/tables/cad_preview_tasks表.md`
- `engines/supermap-workflow/README.md`

## 一、当前稳定边界

1. 当前只支持 `layout=single + data_type=cad + format=dwg|dxf` 的二维 CAD 图纸。
2. Basic scan 只做 DWG / DXF header 识别，不依赖 SuperMap。
3. Deep scan 只通过 `supermap_workflow` 的 `cad.inspect` 读取图纸结构摘要，不遍历 Geometry。
4. Manager 只通过 `cad.render_preview` 生成受管 WebP 瓦片，不把 CAD entity 转为 WKB / GeoJSON 交给前端重画。
5. Transfer 只复制原始 CAD 文件，不隐式执行 CAD→GIS 或 DWG↔DXF 转换。
6. SuperMap iObjects C++ 是 CAD 深度扫描、渲染和后续导入的唯一 provider，不保留 Java/GPA、独立 ODA 或 LibreDWG 执行路线。

上述边界已经稳定，不再在 `docs/next` 中维护实现清单、样例耗时、历史容器状态或迁移过程。

## 二、CAD→GIS 导入

`cad.import` 尚未实现。开始实现前必须先确定 owner、任务语义和输出资源契约，不能直接沿用扫描或预览的 direct 调用方式。

必须明确：

1. 由 Develop 工作流、Transfer 任务还是独立的数据准备能力拥有导入定义、执行记录、重试和结果生命周期。
2. Public Operator Spec 如何表达源 CAD item、目标父资源与目标名称；Runtime 不得解析 ADDP locator。
3. entity / layer / layout / block 的选择规则，以及点、线、面、文本、标注和块引用的 GIS 映射规则。
4. CAD 本地坐标、单位和可选 CRS 如何转换为目标空间参考；没有可靠 CRS 时不得伪造 EPSG。
5. 属性字段命名、重复字段、空值、颜色、线型、图层名和块属性的保留规则。
6. 不支持实体、三维实体和异常 Geometry 的失败或跳过语义，以及导入报告结构。
7. 输出必须是新的 `data_type=table + capabilities.spatial` item，并通过目标 locator 进入 Meta scan；不得覆盖或改写源 CAD item。

`cad.inspect`、`cad.render_preview` 和后续 `cad.import` 必须保持三个独立算子，不共享领域结果状态。

## 三、外部引用、字体与资源预算

正式规范已经给出 Xref 和字体安全边界，但以下内容仍缺少真实恶意样例和跨存储端到端验证：

1. Xref 只允许源文件同目录或同 object prefix 下的相对路径；拒绝绝对路径、网络路径、父目录逃逸和跨租户引用。
2. SHX / TTF 只从平台受控只读目录加载，不读取源文件声明的任意宿主机路径。
3. Object Storage 输入物化到任务私有临时目录，执行结束后清理；Xref 集合必须整体受路径和容量预算约束。
4. 在现有 25,000 瓦片上限之外，补齐最大源文件大小、最大展开大小、最大 layout / layer / Xref 数、超时和临时空间验证。
5. 使用缺失字体、缺失 Xref、循环 Xref、路径逃逸和超大图纸样例验证失败信息与清理行为。

这些边界验证完成前，不扩大 CAD 预览的输入信任范围。

## 四、扩展格式与交互

以下能力不属于当前二维 DWG / DXF 主线，只有出现明确业务场景和真实样例后才单独设计：

- DGN 等其他 CAD 格式。
- 三维 CAD、B-Rep、装配体和参数化模型。
- entity 级拾取、查询、筛选和属性交互。
- CAD 编辑、标注修改和原格式回写。
- 实时按需渲染替代预生成瓦片。

若未来把预生成瓦片切换为实时渲染，必须沿用同一 Manager 预览 API 并删除旧实现，不保留两条可选路线。

## 五、收口条件

本文在以下事项完成后删除：

1. `cad.import` 的 owner、任务语义、Public / Adapter / Runtime Operator Spec 和产物登记路径进入正式规范。
2. Xref、字体、临时目录和资源预算通过真实安全样例验证，稳定约束回写正式规范与 Engine / Manager 文档。
3. 需要推进的扩展格式或实体交互已形成独立、边界明确的专题；没有明确价值的候选能力直接移除。
