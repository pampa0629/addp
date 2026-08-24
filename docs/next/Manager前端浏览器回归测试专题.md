# Manager 前端浏览器回归测试专题

> 状态：待实施专题。本文定义 Manager 前端浏览器回归测试的目标、唯一技术路线、场景清单和验收标准，不表示相关依赖、命令或 CI 已经落地。当前可用测试入口仍以 `manager/frontend/package.json` 和根目录 `Makefile` 为准。

更新时间：2026-08-20

## 一、背景

Manager 的数据探查页面同时承载资源树、容器子项选择、表格、地图、分页、结构化字段、快显和多种文件预览。此类问题常由真实浏览器中的高度传递、`flex` 收缩、iframe 可视区、异步渲染或响应式布局共同触发，纯函数测试和生产构建不能证明最终界面正确。

2026-08-20 修复 Personal Geodatabase 空间子项预览时，页面数据已经成功加载，但 `ContainerPreview -> TablePreview` 的父级高度链不完整：地图按膨胀后的内容高度计算，将表格推到预览区之外，再被外层 `overflow: hidden` 裁掉。真实浏览器检查确认修复前表格存在但不可见，修复后地图和表格能同时进入可视区域。

当前 Manager 前端已有 Vitest 策略和纯函数测试，但没有 Vue 组件挂载测试、Playwright 配置或浏览器回归入口。仓库其他前端模块已使用 Playwright 1.61.1，具备可参考的独立端口 T3 模式。

本专题遵循 [ADDP 测试与验收规范](../spec/addp测试与验收规范.md)，只记录 Manager 浏览器回归的 owner 场景与实施任务，不建立第二套测试分层和命名。

## 二、目标与非目标

### 2.1 目标

1. 为 Manager 建立可重复、无真实业务环境依赖的 T3 Playwright Smoke / E2E 基线。
2. 覆盖必须由浏览器证明的预览布局、关键交互、路由恢复和响应式行为。
3. 首先固化“空间表格预览时地图与表格同时可见”的回归场景。
4. 使用确定性夹具隔离前端回归，不依赖开发者账号、个人登录态或持续变化的 NFS / Oracle / PGeo 数据。
5. 将真实 System 登录、Gateway、Meta、Manager Backend 和数据源组合验证保留在 T4 Online UI 验收中。
6. 测试失败时保留截图、trace 或视频到操作系统临时目录或 CI artifact，不向仓库写入运行产物。

### 2.2 非目标

- 不用浏览器测试重复后端已经覆盖的字段解析、SQL、空间转换或 Provider 单元测试。
- 不把现有 Vitest 纯函数测试全部迁移到 Playwright。
- 不在 `common-frontend` 和 Manager 分别建设两套平行 Playwright 工程。
- 不复用开发者个人登录态、浏览器 profile、现有开发服务或生产数据作为 T3 夹具。
- 不由 `restart.sh` 自动执行浏览器测试。
- 不为测试在生产代码中保留双路径、兼容开关或长期 mock 分支。

## 三、核心决策

### 3.1 唯一浏览器技术路线

Manager 的浏览器回归统一使用 `@playwright/test`，版本与仓库其他前端模块保持一致。目标文件结构为：

```text
manager/frontend/
├── e2e/
│   ├── fixtures/
│   │   └── preview-fixture.*
│   └── preview-layout.spec.js
├── playwright.config.js
└── package.json
```

实现时只增加一个模块命令 `npm run test:e2e`，并纳入统一测试方案最终确定的根 `Makefile` T3 入口。不得再增加同义脚本、第二份配置或兼容命令。

### 3.2 T3 与 T4 严格分离

| 层级 | 目的 | 数据与身份 | 典型断言 |
| --- | --- | --- | --- |
| T3 独立端口 E2E | 确定性 UI 回归，适合 PR | 前端夹具、受控 API mock、无真实账号 | 布局、路由、按钮状态、分页、响应式、可访问性 |
| T4 Online UI 验收 | 真实跨模块链路 | 专用测试 User、真实 System / Gateway / Manager / Meta、专用测试数据 | 登录、授权、资源定位、真实预览材料、端到端交互 |

同一个业务行为可以在两层分别证明不同事实，但不能在 T3 中悄悄连接开发环境，也不能以 T4 成功替代确定性 T3 回归。

### 3.3 测试宿主与共享组件边界

`ContainerPreview` 位于 `common-frontend/basic`，`TablePreview` 位于 `common-frontend/map`，但二者在 Manager 的 iframe、卡片和数据探查布局中组合使用。首个布局回归由 Manager 的真实宿主页面承载，因为问题只有在完整高度链中才成立。

`common-frontend` 继续负责共享组件实现和可独立表达的 T1 组件逻辑；Manager 负责其业务页面组合后的 T3 浏览器行为。只有多个宿主出现稳定、相同的浏览器夹具需求后，才评估抽取共享测试 helper，不提前建设公共测试框架。

### 3.4 独立端口和服务生命周期

Playwright `webServer` 自行启动 Manager 前端测试实例，使用 `127.0.0.1`、`strictPort` 和独立测试端口。建议预留 `4174`，与 Manager 开发端口 `5174` 对应；正式实施前必须先更新 `docs/spec/addp端口分配.md`，确认没有冲突后才能固化配置。

测试不得接管或重启完整 ADDP 开发环境。`reuseExistingServer` 默认为 `false`，避免旧页面或错误构建冒充本次测试对象。

### 3.5 夹具路线

T3 使用显式测试夹具提供最小预览材料，夹具必须复用生产组件和生产路由，不复制一套“测试版预览组件”。优先顺序为：

1. 通过 Playwright route interception 返回稳定的 Manager、Meta 和 System API 响应。
2. 仅在统一认证入口无法无后端启动时，使用模块内 E2E fixture 页面建立受控认证上下文。
3. 不读取真实 Oracle、PGeo、FileGDB、NFS 或对象存储。

夹具数据需要覆盖：

- `container-preview` 外层对象；
- 至少一个带 geometry 的表格子项；
- 26 列、20 行当前页、总数 265 等足以触发布局和分页的规模；
- 标准 GeoJSON geometry 与明确 CRS；
- 空 geometry、普通属性和较长字段值；
- 子项切换和分页后的第二份响应。

夹具是测试事实，不是产品兼容协议。后端预览响应契约变化时，应同步更新唯一夹具并删除旧形态。

## 四、首批回归场景

### 4.1 P0：空间容器预览布局

以 Personal Geodatabase / FileGDB 一类容器数据的空间子项预览为代表，验证：

1. 容器摘要、子项选择器和子项元数据可见。
2. 地图区域可见且高度不小于产品定义的最小地图高度。
3. 表格区域可见且高度不小于 `180px`。
4. 地图底部、分隔条、表格顶部、分页区按顺序排列，没有重叠。
5. `TablePreview` 高度不超过可用预览区，外层 `scrollHeight` 不因组件内容异常膨胀。
6. 当前页存在 20 行，分页显示总数 265。
7. 切换地图开关后，表格仍可见并获得释放出的空间。
8. 拖动地图与表格分隔条时，双方都遵守最小高度。

布局断言以 bounding box、可见性和顺序关系为主，不使用像素级整页截图作为唯一成功依据。截图只作为失败诊断证据。

### 4.2 P0：普通表格预览

- 无 geometry 时不渲染地图控制和地图容器。
- 表格占用剩余空间，内部纵向滚动正常。
- 横向列滚动不会撑宽外层页面。
- 分页始终位于预览区内。

### 4.3 P0：响应式高度矩阵

至少覆盖：

| 视口 | 目的 |
| --- | --- |
| `1280 x 800` | 常规开发和 CI 基线 |
| `1100 x 780` | 中等宽度、工具栏换行风险 |
| `900 x 700` | 窄窗口和最小高度边界 |

若产品明确不支持更小视口，应在前端规范中定义最小视口，而不是让测试猜测。

### 4.4 P1：预览交互

- 容器子项切换后地图、表格、行数和分页共同更新。
- 页码与 page size 变化发出唯一请求，加载态不破坏布局。
- 结构化字段弹窗可打开、滚动和关闭。
- 保存的地图视角恢复后不改变表格高度。
- 页面 URL 中的 locator 和子项状态刷新后可恢复。

### 4.5 P1：Console iframe 组合

Manager 模块自身的 T3 首先测试独立入口。Console iframe 的尺寸传递、模块导航和登录会话归 Console T3 或 T4 场景；不得在 Manager 和 Console 同时复制同一组业务断言。Manager 只保留 iframe 内部根容器必须 `height: 100%`、`min-height: 0` 的组件边界断言。

## 五、断言与稳定性规则

1. 元素选择优先使用角色、标签和稳定 `data-testid`；不得依赖 Element Plus 生成的内部 class 层级作为长期主选择器。
2. 只有无法通过语义定位的布局容器才增加少量 `data-testid`，不新增用户可见测试文本。
3. 异步等待使用响应、元素状态或 Playwright assertion，不使用固定长时间 `sleep`。
4. 地图底图网络请求必须在 T3 中拦截或使用无外网的确定性底图配置，不能依赖 OpenStreetMap、高德等在线服务。
5. 测试输出写入操作系统临时目录；CI 可归档 screenshot、trace、video，不提交 `test-results/`。
6. T3 失败不得自动重试掩盖确定性缺陷；如 CI 确需重试，只能由统一测试方案定义且报告首次失败。
7. 不对 CSS 类名字符串做替代性单元测试；布局必须由浏览器计算后的尺寸证明。

## 六、实施阶段

### 阶段 0：规范与入口确认

- 将 `4174` 测试端口写入端口规范，或选择规范确认的唯一空闲端口。
- 确认统一 T3 根入口和 CI 触发方式。
- 确认 Manager 测试身份不复用个人账号。

### 阶段 1：最小 Playwright 基线

- 在 Manager 增加 `@playwright/test`，版本与仓库统一。
- 增加唯一 `playwright.config.js` 和 `test:e2e` 命令。
- 输出目录使用操作系统临时目录。
- 建立独立端口启动和最小健康 fixture。
- 删除实施过程中被替代的临时脚本或旁路入口。

### 阶段 2：预览夹具与 P0 场景

- 建立标准表格和空间容器预览夹具。
- 增加普通表格、空间地图 + 表格、响应式高度矩阵测试。
- 覆盖地图开关和分隔条最小高度。
- 将当前 PGeo 预览布局缺陷固化为回归用例。

### 阶段 3：关键交互与路由恢复

- 增加子项切换、分页、结构化字段和视角恢复。
- 增加 locator 刷新恢复验证。
- 按场景稳定性决定是否进入 PR 必跑矩阵。

### 阶段 4：T4 Online UI 验收

- 使用专用测试 Tenant 和 User。
- 通过真实 Console、System、Gateway、Meta 和 Manager 验证登录、资源定位和预览。
- 首批真实数据建议覆盖普通数据库空间表和一种容器格式；Oracle Spatial、PGeo、FileGDB、SDE 分别按各自能力门禁声明，不相互替代。
- 每次运行使用唯一 Run ID，结束后检查夹具残留为零。

## 七、目标变更清单

实施时预计一次性修改：

- `manager/frontend/package.json`
- `manager/frontend/package-lock.json`
- `manager/frontend/playwright.config.js`
- `manager/frontend/e2e/fixtures/*`
- `manager/frontend/e2e/preview-layout.spec.js`
- 必要的少量稳定 `data-testid`
- `docs/spec/addp端口分配.md`
- 根 `Makefile` 或统一测试 runner（以统一测试专题最终落地为准）
- CI T3 变更路径矩阵

不得为同一场景同时保留模块私有 shell 脚本和根 Make 入口两条长期路线。

## 八、验收标准

专题第一阶段完成必须满足：

1. `npm run test:e2e` 能在没有运行完整 ADDP 服务的环境中自行启动、执行和退出。
2. 测试不会连接开发业务库、真实 NFS、Oracle、PGeo、FileGDB 或外部地图服务。
3. 空间容器预览能证明地图和表格同时可见，且三种目标视口均通过。
4. 普通表格预览能证明无地图时表格正确占满剩余空间。
5. 失败时在临时目录或 CI artifact 中提供可诊断证据，仓库工作区不产生测试结果文件。
6. Manager 生产构建、现有 Vitest 和新增 Playwright 全部通过。
7. 端口规范、模块命令、根测试入口和 CI 调用一致，不存在兼容入口。
8. `restart.sh` 不执行或接管浏览器测试。

## 九、后续实施建议

本专题实施时，先完成阶段 0-2，形成一条稳定的 P0 预览布局门禁，再扩展交互和 T4。不要一次性把 Manager 所有页面加入浏览器测试；优先选择纯函数和组件测试无法证明、且历史上真实发生过的高风险浏览器行为。
