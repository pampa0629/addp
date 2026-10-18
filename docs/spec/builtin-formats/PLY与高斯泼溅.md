# 内置格式：PLY与高斯泼溅

[返回内置格式规范](../addp内置数据类型与文件格式规范.md) · [通用写入与消费约束](../addp内置数据类型与文件格式规范.md#通用写入与消费约束)

PLY 的内容分类，以及 SPLAT、KSPLAT 的识别与消费。PLY 的数据类型由本页 header 规则判定。

阅读导航：[PLY](#ply)、[SPLAT](#splat)、[KSPLAT](#ksplat)。

## PLY

### 识别与组织

| 维度 | 取值 |
|---|---|
| `layout` | `single` |
| `data_type` | 按 PLY header 判定为 `model_3d`、`point_cloud` 或 `gaussian_splat` |
| `format` | `ply` |
| 主资源 | `meta_item.full_name` 指向 `.ply` 文件资源 |

PLY 是单资源、内容敏感格式。它既可能表达 polygon mesh，也可能只包含顶点集合或 Gaussian Splat 导出数据；扫描不得仅凭 `.ply` 扩展名臆造面、材质、三角面或点云事实。实现必须优先读取 PLY header，记录 format、version、element counts、vertex properties 和可识别的 Gaussian Splat 信号，并按 header 事实确定 `data_type`。

| header 事实 | `data_type` | `format_info.ply.layout` |
|---|---|---|
| `element face` 数量大于 0 | `model_3d` | `mesh` |
| 无 face，且具备 `x/y/z`、`opacity`、`scale_0..2`、`rot_0..3`、`f_dc_0..2` 或颜色字段 | `gaussian_splat` | `gaussian_splat` |
| 无 face，且具备 `chunk` element 与 `packed_position`、`packed_rotation`、`packed_scale`、`packed_color` vertex 属性 | `gaussian_splat` | `gaussian_splat` |
| 无 face，且不满足高斯泼溅字段组合 | `point_cloud` | `point_cloud` |

SuperSplat 压缩 PLY 仍归入 `data_type=gaussian_splat`、`format=ply`，并通过 `format_info.ply.is_compressed_splat=true` 表达格式私有事实。Manager 基础预览读取该标记选择非渐进加载策略，不把压缩 PLY 转入 GLB 快显路线。

### attributes 写入

| 分区 | 写入内容 |
|---|---|
| `item` | `layout=single`、`format=ply`、按 header 判定后的 `data_type` |
| `type_info.model_3d` | 仅 mesh PLY 写入；`model_kind=mesh_scene`、vertex count、mesh count 等跨格式三维模型轻量摘要 |
| `type_info.point_cloud` | 仅点云型 PLY 写入；`point_cloud_kind=raw_point_cloud`、点数、维度列表、颜色 / intensity 能力等跨格式点云摘要 |
| `type_info.gaussian_splat` | 仅 3DGS 型 PLY 写入；`representation=3d_gaussian_splatting`、splat count、opacity / scale / rotation / spherical harmonics 能力 |
| `format_info.ply` | PLY 私有摘要，例如 encoding、version、layout、vertex / face count、header line count、element counts、vertex properties 及其数量、是否 Gaussian Splat |

### 消费要求

Manager 按 `data_type` 选择消费路线。`data_type=model_3d` 的普通 mesh PLY 可通过 storage stream 交给前端 Three.js PLYLoader 做基础预览；该基础预览不查找、替换或依赖 Manager 受管 GLB artifact。点云型 PLY 走点云路线；Gaussian Splat PLY 走高斯泼溅 renderer，不得简单等同于 mesh GLB，也不开放 `model_3d` 的 GLB 快显任务入口。

mesh PLY 的后续快显转换可接 PLY -> GLB；这不改变当前基础预览独立读取源文件的契约。点云型 PLY 使用点云快显，高斯泼溅 PLY 使用高斯泼溅快显，不转换为 GLB。

PLY deep scan 不应读取完整大文件数据体；header 级事实足以完成首轮识别和轻量摘要。模型包围盒、材质、纹理、法线等事实只有在稳定解析后才可写入。

### 格式约束

- 不得把所有 `.ply` 固定归入 `model_3d` 或 `point_cloud`。
- 不得仅因 3DGS PLY 使用 vertex 记录就归为 `point_cloud`。
- 不得把高斯泼溅的渲染协议、排序结果、压缩产物或派生 splat artifact 写入 `type_info.gaussian_splat`。
- PLY header 私有属性列表进入 `format_info.ply`，跨格式通用摘要进入对应 `type_info.<data_type>`。

## SPLAT

### 识别与组织

| 维度 | 取值 |
|---|---|
| `layout` | `single` |
| `data_type` | `gaussian_splat` |
| `format` | `splat` |
| 主资源 | `meta_item.full_name` 指向 `.splat` 文件资源 |

`.splat` 是高斯泼溅的直接消费格式。Meta scan 通过扩展名和内置 descriptor 将其归为 `gaussian_splat`，不归入 `model_3d` 或 `point_cloud`。

### attributes 写入

| 分区 | 写入内容 |
|---|---|
| `item` | `layout=single`、`data_type=gaussian_splat`、`format=splat` |
| `type_info.gaussian_splat` | `representation=3d_gaussian_splatting`；不强制扫描完整文件计算基元数 |
| `format_info.splat` | `encoding=splat` 等格式私有轻量事实 |

### 消费要求

Manager 可使用高斯泼溅 renderer 直接读取 `.splat` URL 做基础预览；不得开放 GLB 快显生成入口。需要平台受管 artifact、统一对象存储发布或大模型优化时，走 `gaussian_splat_ksplat_generation` 专用路线，由 `model3d_workflow` 的 `gaussian_splat_to_ksplat` operator 转换为 KSplat artifact，不复用 `model_3d_glb_generation`。

## KSPLAT

### 识别与组织

| 维度 | 取值 |
|---|---|
| `layout` | `single` |
| `data_type` | `gaussian_splat` |
| `format` | `ksplat` |
| 主资源 | `meta_item.full_name` 指向 `.ksplat` 文件资源 |

`.ksplat` 是面向高斯泼溅渲染优化的压缩消费格式。Meta scan 通过扩展名和内置 descriptor 将其归为 `gaussian_splat`，不归入 `model_3d` 或 `point_cloud`。

### attributes 写入

| 分区 | 写入内容 |
|---|---|
| `item` | `layout=single`、`data_type=gaussian_splat`、`format=ksplat` |
| `type_info.gaussian_splat` | `representation=3d_gaussian_splatting`；不强制扫描完整文件计算基元数 |
| `format_info.ksplat` | `encoding=ksplat` 等格式私有轻量事实 |

### 消费要求

Manager 可使用高斯泼溅 renderer 直接读取 `.ksplat` URL 做基础预览；不得开放 GLB 快显生成入口，也不为源 `.ksplat` 创建 KSplat 快显任务。`.ksplat` 同时是高斯泼溅受管快显的目标文件格式；`gaussian_splat_ksplat_generation` 只对 `ply` / `splat` 源执行真实转换，不复用 `model_3d_glb_generation`。
