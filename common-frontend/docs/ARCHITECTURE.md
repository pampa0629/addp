# Common Frontend 共享前端架构

`common-frontend` 按依赖与职责划分共享源码。业务模块复用规范实现，只承担领域组合和协议适配；组件唯一所有权遵循[开发原则](../../docs/spec/addp开发原则.md#8-dry-dont-repeat-yourself--不要重复自己)。

## 目录结构

| 源码位置 | 职责与入口 |
| --- | --- |
| [basic/src](../basic/src/index.js) | 不依赖地图库的基础 UI、资源选择、认证、主题、国际化、表格与分页；也是包根入口。 |
| [basic/src/previews.js](../basic/src/previews.js) | 文件预览的独立入口，避免基础入口直接导出带预览依赖的组件。 |
| [map/src](../map/src/index.js) | 地图、空间预览、CRS 与底图展示，组合基础表格和分页。 |
| [chart/src](../chart/src/index.js) | 数据服务结果图表与受控图表配置。 |
| [dag/src](../dag/src/index.js) | DAG 查看器、编辑交互与共享 composable。 |
| [graph/src](../graph/src/index.js) | 图结果、Schema、本体与血缘查看器。 |
| [agent-ui/src](../agent-ui/src/index.js) | Agent A2UI 协议到受信任共享 Vue 组件的适配。 |

组件与结果契约的详细说明见[共享前端 README](../README.md)，地图与 DAG 的专项边界分别见[Map 说明](../map/README.md)和[DAG 说明](../dag/README.md)。

## 使用方法

### 基础组件（System, Transfer 等不需要地图的模块）

`@common-ui` 指向 `basic/src`；包名 `@addp/common-frontend` 的主入口也为 `basic/src/index.js`。例如：

```javascript
import { StorageEngineForm, ResourceTree } from '@common-ui'
```

基础入口的依赖不止 Vue 和 Element Plus；以[包依赖声明](../package.json)及消费模块的 `package.json`、锁文件为准。

### 预览组件（Manager 等需要文件预览的模块）

从 `@common-ui/previews` 单独导入，使用方声明对应预览依赖，不把这些组件重新导出到基础主入口：

```javascript
import { ImagePreview, MarkdownPreview, PdfPreview } from '@common-ui/previews'
```

### 地图组件（Manager 等需要地图的模块）

`@common-ui-map` 指向 `map/src`：

```javascript
import { MapContainer, GeoJsonPreview, TablePreview } from '@common-ui-map'
```

地图使用方声明 `ol`、`@amap/amap-jsapi-loader` 等实际依赖；预览、图、DAG 和图表也按使用的入口维护依赖，不以模块名称推断。版本约束遵循[技术栈规约](../../docs/spec/addp技术栈规约.md)，具体别名和去重配置可核对[Manager Vite 配置](../../manager/frontend/vite.config.js)与[System Vite 配置](../../system/frontend/vite.config.js)。

## 表格预览组合边界

- `basic/TabularResultRenderer.vue` 是表格预览和有界结果渲染的唯一基础表格实现，负责列描述、标量与结构化值呈现、行选择和结构化值详情。
- `basic/DataPagination.vue` 是预览场景的唯一受控分页实现，只接收 `currentPage`、`pageSize`、`total` 并发出分页变化；它不请求 API，也不在内部切片数据。窄面板下控件分组换行，不能用横向裁剪或滚动条遮挡上一页、下一页及页码控件。
- `map/TablePreview.vue` 只组合地图、空间字段联动、字段选择、基础表格和分页。地图依赖不得进入 `basic/`。
- Manager 在分页变化时向后端加载对应页；Develop 对单次执行已经加载的有界结果做客户端切片。两种策略共享相同的表格与分页呈现，不建立两套组件。表格预览以响应的总数与当前页大小表达记录范围，不显示与实际分页相矛盾的固定 50 行上限。
- Container Preview、Workbench 和 Agent UI 的协议适配器应复用同一个基础表格，不得重新实现同类单元格格式化和结构化值展示。

## 接入与验证

新模块的依赖、别名、单一 Vue 实例、主题及语言初始化统一按[新模块开发指南](../../docs/spec/addp新模块开发指南.md#创建新模块前端)接入，不在本文重复整份 Vite 配置或保留旧迁移步骤。现有模块仍须通过 `overrides`、alias 与 dedupe 保持 Vue 单一实例，`common-frontend` 不保留自己的 `node_modules`。

视觉与交互要求见[前端风格规范](addp前端风格设计规范.md)。共享组件与唯一所有权通过根 `make test-common-frontend` 验证；消费模块按实际影响选择标准门禁，确定性浏览器回归与真实运行验收分别记录，规则见[测试与验收规范](../../docs/spec/addp测试与验收规范.md)。
