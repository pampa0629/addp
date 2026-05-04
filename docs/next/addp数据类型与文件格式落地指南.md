# ADDP 数据类型与文件格式落地指南

更新时间：2026-05-04

本文说明 ADDP 在实现层面应如何识别数据、组织 meta item、判断数据家族、识别文件格式并提取扩展信息。它以 `meta` 扫描为第一入口，而不是从单个文件格式出发。

## 总体落地原则

实现层的顺序应与概念层有所区别。

概念层回答：

1. 数据长什么样
2. 一套数据如何组成 item
3. 单个文件如何编码
4. 具备哪些扩展语义

实现层应从扫描入口出发：

1. `meta` 先扫描资源树
2. 优先推断组合形态
3. 归并为 meta item
4. 在 item 层判断数据家族
5. 再识别文件格式
6. 最后提取字段、对象元数据和扩展信息

原因是：真实资源首先以“目录、文件、对象、表”的形式出现，系统必须先判断哪些资源共同构成一套数据，才能继续判断它是什么数据。

## 一、扫描主流程

### 第一步：资源树扫描

`meta` 从存储引擎获取资源树。

典型输入包括：

- 数据库 schema / table / collection
- 对象存储 bucket / prefix / object
- 文件系统目录 / 文件

这一阶段只回答“资源在哪里”和“资源有哪些”，不急于判断具体文件格式。

### 第二步：组合形态推断

组合形态推断必须优先于文件格式判断。

因为：

- Shapefile 不是单个 `.shp` 文件，而是一组配套文件
- Parquet 数据集可能是一个目录
- SQLite 是一个容器文件
- OSGB、影像镶嵌数据集可能是目录树

这一阶段应识别：

| 组合形态 | 例子 | meta 处理方式 |
|---|---|---|
| 单文件单 item | `data.csv`、`photo.jpg`、`report.pdf` | 一个文件生成一个 item |
| 多文件单 item | `roads.shp + roads.shx + roads.dbf + roads.prj` | 多个文件归并为一个 item |
| 容器文件单 item | `demo.sqlite` | 容器文件生成一个 item，并可展开内部子 item |
| 目录树单 item | `dataset/`、`scene/`、`mosaic/` | 目录整体生成一个 item |
| 混合集合单 item | 主文件 + 辅助文件 + 索引文件 | 以组合规则归并为一个 item |

### 第三步：meta item 归并

组合形态确认后，`meta` 应先生成稳定的 item。

item 应至少具备：

- item 类型
- 物理路径
- 组合形态
- 主文件或入口路径
- 组成文件列表
- 大小与修改时间
- fingerprint

这一步是后续预览、索引、传输的基础。

### 第四步：数据家族判断

在 item 层判断它属于哪类数据。

建议基础数据家族包括：

- 表格型数据
- 图片型数据
- 视频型数据
- 文档型数据

空间、采样、索引、EXIF、页数等不应作为主数据家族，而应作为扩展语义。

### 第五步：文件格式识别

文件格式识别发生在 item 归并之后。

格式识别可以基于：

- 主文件扩展名
- MIME 类型
- magic bytes
- 目录结构特征
- 容器内部结构

例如：

- CSV item 的主格式是 `csv`
- Shapefile item 的主格式是 `shapefile`
- SQLite item 的主格式是 `sqlite`
- Parquet 数据集的主格式是 `parquet`
- GeoTIFF item 的主格式是 `tiff`

### 第六步：元数据与扩展信息提取

在数据家族和文件格式确定后，调用对应 parser / extractor。

典型输出包括：

- `TableInfo`
- `ObjectInfo`
- `FieldInfo`
- `ExtensionInfo`

空间信息、媒体信息、文档页数、采样推断等都应作为扩展信息挂载。

## 二、各模块职责

### meta

`meta` 是第一入口，负责：

- 扫描资源树
- 推断组合形态
- 归并 meta item
- 判断数据家族
- 初步识别文件格式
- 提取可索引的元数据
- 维护 item fingerprint

`meta` 不应只按单文件扩展名生成 item。

### common/format

`common/format` 负责：

- 文件格式识别
- `TableInfo` / `ObjectInfo` 模型
- `FieldInfo` 类型标准化
- `ExtensionInfo` 定义
- parser / extractor 注册

它不直接决定 meta item 如何归并，但应为归并后的 item 提供解析能力。

### common/dataitem

`common/dataitem` 负责组合形态推断和数据项识别。

它应放在 `common` 下作为平台级共享能力，而不是放在 `meta` 内部。

原因：

- `meta` 是第一调用方，但不是组合形态规则的唯一消费者
- `manager`、`transfer`、`asset` 等模块后续都可能复用 item 识别结果
- 组合形态推断发生在文件格式识别之前，不应放在 `common/format` 内部
- 它也不是引擎插件能力本身，不宜放在 `common/engine/plugin`

建议目录：

```text
common/dataitem/
  types.go
  detector.go
  registry.go
  resolver.go
```

其中：

- `types.go` 定义 `CompositionType`、`DataFamily`、`DetectedItem`
- `detector.go` 定义组合形态 detector 接口
- `registry.go` 负责 detector 注册与优先级
- `resolver.go` 负责基于目录、文件组、对象列表执行统一推断

`common/dataitem` 重点识别：

- 多文件组合
- 目录树数据集
- 容器型文件
- 混合文件集合

它应服务于 `meta` 扫描阶段。

### manager

`manager` 不应重新发明扫描语义。

它应优先使用 `meta` 已识别的：

- item 类型
- 组合形态
- 数据家族
- 文件格式
- 扩展信息
- physical path

然后选择合适的预览 provider。

### transfer

`transfer` 应消费标准化后的 item 与元数据。

它不应重复判断组合形态，也不应重复推断字段类型。

## 三、新增能力的推荐步骤

### 新增单文件格式

1. 明确它属于哪个数据家族
2. 明确它默认是单文件单 item
3. 实现格式识别
4. 实现 parser / extractor
5. 注册到 `common/format`
6. 接入 `meta` 扫描
7. 接入 `manager` 预览

### 新增多文件组合格式

1. 定义组合规则
2. 在 `common/dataitem` 实现 detector
3. 在 `meta` 中先归并 item
4. 明确主文件或入口文件
5. 判断数据家族
6. 实现 parser / extractor
7. 接入预览和索引

### 新增容器文件格式

1. 定义容器文件如何成为 item
2. 定义容器内部是否展开为子 item
3. 实现容器识别
4. 实现内部元数据枚举
5. 实现预览与字段抽取

### 新增目录树数据集

1. 定义目录结构特征
2. 在 `common/dataitem` 实现目录 detector
3. 在 `meta` 中将目录归并为 item
4. 记录入口目录和组成文件
5. 判断数据家族
6. 提取扩展信息

## 四、实现约束

- 不要先按单个文件格式生成 item，再事后合并。
- 不要把目录型、容器型放进数据家族，它们属于组合形态。
- 不要把空间放进数据家族，空间属于扩展语义。
- 不要默认空间字段名一定是 `geom`。
- 不要让 `manager` 重复实现 `meta` 的组合识别规则。
- 不要让 `transfer` 重复推断字段类型。
- 不要把组合形态 detector 长期留在 `meta` 或 `common/format` 中。

## 五、推荐验证方式

新增或修改能力后，至少验证：

1. `meta` 是否正确归并 item
2. item 的组合形态是否正确
3. 数据家族是否正确
4. 文件格式是否正确
5. 字段或对象元数据是否完整
6. 扩展信息是否可被 `manager` 使用
7. fingerprint 是否稳定

## 六、结论

落地实现必须从 `meta` 扫描出发。

正确顺序是：

**资源树扫描 -> 组合形态推断 -> meta item 归并 -> 数据家族判断 -> 文件格式识别 -> 元数据与扩展信息提取。**
