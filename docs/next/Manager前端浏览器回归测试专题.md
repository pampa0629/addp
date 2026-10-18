# Manager 浏览器回归后续验收

Manager 已有 `@playwright/test`、`playwright.config.js`、`e2e/` 与经共享 `frontend-browser-gate.mjs` 调度的唯一 `test:e2e`，沿现有模块/前端 CI 自动发现。原专题中“尚无 Playwright”“固定 4174 新端口”“另建私有测试入口”的提案不再作为现行路线。

稳定规则见[测试与验收规范](../spec/testing/Owner补充门禁.md#manager-预览浏览器门禁)，产品预览边界见[Manager 预览语义协议](../../manager/docs/数据预览语义协议.md)。本文只保留需要逐项取得证据的场景，不把测试文件存在当作已经运行通过。

## 尚需核对的场景覆盖

| 范围 | 必须证明的行为 |
| --- | --- |
| 空间容器布局 | 地图、表格、子项和分页同时可见；高度传递及 flex 收缩正常，地图开关释放空间、拖动遵守最小高度 |
| 普通表格 | 无 geometry 不出现地图，表格内部纵向/横向滚动，不撑宽页面，分页保持可见 |
| 响应式 | 常规、中等和窄窗口的真实 bounding box、可见性与顺序；产品最小视口先按规范确认，不由旧建议强行固定 |
| 异步与恢复 | 子项/页码/page size 原子更新，结构化字段弹窗、地图视角与 locator 刷新恢复，失败/迟到响应不污染当前目标 |
| Console 组合 | Manager 仅验证自身根容器，外层 iframe/会话/navigation 由 Console 门禁唯一负责 |
| 真实数据 T4 | 正式 User/System/Gateway/Meta/Manager 与普通空间表、容器格式分别验收；Oracle、PGeo、FileGDB、SDE 按能力独立声明 |

首批“地图和表格同时可见”的回归覆盖须对照现有 e2e 用例逐项核验，缺失时复用现有宿主补用例，不复制另一套组件或测试工程。不同格式的成功不互相替代。

## 验证与完成条件

使用 `make test-manager-frontend` 或完整 `make test-module MODULE=manager`，覆盖生产构建、现有确定性测试与共享编排的浏览器门禁。真实跨模块验收仅使用已登记 suite 和隔离夹具；不连接个人 NFS/Oracle 或重启开发环境。T3 断言使用计算后的布局与状态，失败产物写临时目录/Artifact，不以 CSS 字符串或整页截图替代行为证明。

全部所需场景取得对应证据后，补入正式测试规范和 Owner 用例，删除本专题；本次文档整理未运行浏览器或产品门禁。
