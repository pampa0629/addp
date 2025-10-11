# DataExplorer 重构版本 - 快速上手

## 🎉 重构完成

原2555行的 `DataExplorer.vue` 已成功重构为模块化组件系统!

---

## 🚀 立即开始

### 1. 启动开发服务器

```bash
cd manager/frontend
npm install  # 如果还没安装依赖
npm run dev
```

访问: http://localhost:5174

### 2. 查看效果

旧版本已备份为 `DataExplorer_old.vue`,新版本已生效!

---

## 📁 新架构一览

```
src/
├── views/
│   └── DataExplorer.vue          162行 ✨ 主组件(精简90%代码)
│
├── composables/                  可复用逻辑层
│   ├── useMapConfig.js           地图配置
│   ├── useGaodeMap.js            高德地图
│   ├── useOpenLayersMap.js       OpenLayers
│   └── useResizable.js           拖拽调整
│
├── components/
│   ├── map/                      地图组件层
│   │   ├── MapContainer.vue      地图容器
│   │   ├── GaodeMapRenderer.vue  高德渲染器
│   │   └── OpenLayersRenderer.vue  OL渲染器
│   │
│   ├── previews/                 预览组件层
│   │   ├── TablePreview.vue      表格+地图预览
│   │   ├── ObjectStoragePreview.vue  对象存储
│   │   ├── GeoJsonPreview.vue    GeoJSON+地图
│   │   ├── ImagePreview.vue      图片
│   │   ├── JsonPreview.vue       JSON
│   │   └── TextPreview.vue       文本
│   │
│   └── explorer/                 基础组件层
│       ├── ResourceTree.vue      资源树
│       ├── PreviewPanel.vue      预览面板
│       └── Splitter.vue          分隔器
│
├── plugins/previews/             插件系统 🔌
│   └── index.js                  自动路由到合适的预览组件
│
└── utils/
    ├── formatters.js             格式化工具
    └── treeTransform.js          树转换
```

---

## 🔌 最激动人心的特性: 插件化预览系统

### 无需修改核心代码,即可添加新文件类型支持!

#### 示例: 添加 CSV 预览

**步骤1**: 创建插件文件 `public/plugins/my-csv-preview.js`

```javascript
window.DataExplorerPlugins = window.DataExplorerPlugins || []

window.DataExplorerPlugins.push({
  name: 'csv',
  component: {
    template: `
      <el-table :data="rows">
        <el-table-column v-for="col in columns" :key="col" :prop="col" :label="col"/>
      </el-table>
    `,
    props: ['data'],
    computed: {
      rows() {
        // 解析 CSV
        const text = this.data.object?.content?.text || ''
        return parseCSV(text)
      }
    }
  },
  canHandle: (data) => data.object?.path?.endsWith('.csv'),
  priority: 50
})
```

**步骤2**: 在 `index.html` 引入

```html
<script src="/plugins/my-csv-preview.js"></script>
```

**完成!** 系统会自动识别并使用你的插件!

查看完整示例: [example-csv-preview.js](public/plugins/example-csv-preview.js)

---

## 🎯 核心改进

| 方面 | 旧版 | 新版 |
|------|------|------|
| **代码行数** | 2555行单文件 | ~2400行分布在21个文件 |
| **平均文件大小** | N/A | ~115行/文件 |
| **可维护性** | ❌ 难以定位问题 | ✅ 职责清晰 |
| **可扩展性** | ❌ 修改核心代码 | ✅ 插件化系统 |
| **可测试性** | ❌ 难以单元测试 | ✅ 组件独立可测 |
| **性能** | ⚠️  全量加载 | ✅ 可按需加载 |

---

## 🛠️ 开发工作流

### 修改预览逻辑

**旧版**: 需要在2555行中找到对应逻辑,小心不破坏其他功能

**新版**: 直接修改对应组件

```bash
# 修改表格预览
vim src/components/previews/TablePreview.vue

# 修改地图渲染
vim src/components/map/GaodeMapRenderer.vue

# 修改格式化工具
vim src/utils/formatters.js
```

### 添加新功能

**旧版**: 在巨型文件中插入代码,可能引发意外bug

**新版**: 创建新组件或插件

```bash
# 方式1: 创建内置组件
cp src/components/previews/TextPreview.vue src/components/previews/MarkdownPreview.vue
# 修改后注册到 plugins/previews/index.js

# 方式2: 创建用户插件
vim public/plugins/markdown-preview.js
# 在 index.html 引入即可
```

---

## 📚 详细文档

- **[REFACTOR_SUMMARY.md](REFACTOR_SUMMARY.md)** - 重构总结和对比
- **[REFACTOR_PLAN.md](REFACTOR_PLAN.md)** - 完整重构方案
- **[public/plugins/README.md](public/plugins/README.md)** - 插件开发指南

---

## ✅ 功能验证清单

启动项目后,请验证以下功能:

### 基础功能
- [ ] 左侧资源树正常加载
- [ ] 点击树节点切换预览
- [ ] 拖拽调整树宽度

### 表格预览
- [ ] 显示表格数据
- [ ] 分页功能正常
- [ ] GeoJSON字段自动显示地图
- [ ] 地图和表格联动(点击要素高亮行)

### 对象存储预览
- [ ] 显示bucket/目录/文件元数据
- [ ] 双击目录进入
- [ ] 文件内容预览:
  - [ ] 图片 (.png, .jpg)
  - [ ] JSON (.json)
  - [ ] GeoJSON (.geojson) + 地图
  - [ ] 文本 (.txt)

### 地图功能
- [ ] 高德地图底图
- [ ] 天地图矢量底图
- [ ] 天地图影像底图
- [ ] 切换底图视角保持
- [ ] 点击要素显示弹窗

---

## 🐛 遇到问题?

### 常见问题

**Q: 构建失败,提示找不到模块?**
```bash
# 清理缓存重新安装
rm -rf node_modules package-lock.json
npm install
```

**Q: 地图不显示?**
- 检查是否配置了地图Key (高德/天地图)
- 查看浏览器控制台错误信息

**Q: 自定义插件不生效?**
- 检查 `index.html` 是否正确引入了插件文件
- 查看浏览器控制台是否有 `✅ 注册预览插件: xxx` 日志
- 确认 `canHandle` 函数返回 `true`

### 调试技巧

```javascript
// 1. 查看已注册的插件
import('@/plugins/previews').then(m => {
  console.log('已注册插件:', m.getRegisteredPlugins())
})

// 2. 测试 canHandle 函数
const testData = { object: { path: 'test.csv' } }
const plugin = window.DataExplorerPlugins[0]
console.log('能否处理:', plugin.canHandle(testData))

// 3. 查看选中的组件
// 打开 Vue DevTools,查看 PreviewPanel 组件的 previewComponent 属性
```

---

## 🎓 学习建议

### 新手入门路径

1. **先理解整体架构** (10分钟)
   - 阅读 [REFACTOR_SUMMARY.md](REFACTOR_SUMMARY.md)
   - 查看文件结构

2. **运行并测试** (20分钟)
   - 启动项目
   - 测试各种预览功能
   - 尝试调整面板大小

3. **修改一个简单组件** (30分钟)
   - 修改 `TextPreview.vue`,改变样式
   - 修改 `formatters.js`,改变日期格式

4. **创建一个自定义插件** (1小时)
   - 参考 [example-csv-preview.js](public/plugins/example-csv-preview.js)
   - 创建自己的插件
   - 测试效果

### 进阶开发路径

1. **深入理解 Composables** (2小时)
   - 阅读 `useGaodeMap.js` 源码
   - 理解地图状态管理

2. **创建复杂预览组件** (4小时)
   - 参考 `TablePreview.vue`
   - 创建带交互的预览组件

3. **优化性能** (半天)
   - 实现预览组件懒加载
   - 优化地图渲染性能

---

## 🚀 下一步行动

### 立即可做

1. ✅ 启动项目验证功能
2. ✅ 创建一个自定义插件
3. ✅ 阅读详细文档

### 中期计划

1. 为组件编写单元测试
2. 添加更多文件类型支持(PDF, Excel, Video)
3. 实现插件配置UI

### 长期规划

1. 发布官方插件市场
2. 支持NPM包形式的插件
3. 集成到 Storybook

---

## 📞 需要帮助?

- 查看完整文档: [REFACTOR_PLAN.md](REFACTOR_PLAN.md)
- 插件开发指南: [public/plugins/README.md](public/plugins/README.md)
- 提交Issue到项目仓库

---

**祝开发愉快! 🎉**
