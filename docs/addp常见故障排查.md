# ADDP 常见故障排查指南

本文档记录 ADDP 平台开发和使用过程中遇到的常见问题及解决方案。

---

## 前端问题

### 1. Manager 数据预览显示"暂无数据"（双重 .data 访问问题）

#### 问题现象

- 用户在 Manager 模块的数据浏览器中点击表格节点
- 右侧预览面板显示"暂无数据"
- 后端 API 返回 200 成功状态，数据完整
- 浏览器控制台无明显错误

#### 问题根因

**双重 `.data` 访问导致数据丢失**

数据流跟踪：

1. **后端返回**（正确）
   ```json
   HTTP 200 OK
   {
     "mode": "table",
     "columns": ["id", "name", ...],
     "rows": [{...}, {...}]
   }
   ```

2. **createAPIClient 自动提取**（正确）
   - 配置：`extractData = true`（默认开启）
   - 响应拦截器：`return response.data`
   - 结果：API 调用返回已提取的数据对象

3. **问题发生点**（错误）
   ```javascript
   // manager/frontend/src/views/DataExplorer.vue
   const response = await dataExplorerAPI.getPreview(params)
   // response = { mode: "table", columns: [...], rows: [...] } ✅

   previewData.value = normalizePreviewPayload(response.data, selectedNode.value)
   //                                           ^^^^^^^^^^^^^
   //                                           undefined! ❌
   ```

4. **渲染层判断**（触发"暂无数据"）
   ```vue
   <div v-else-if="!previewData" class="empty-state">
     <el-empty description="暂无数据" />
   </div>
   ```

#### 技术细节

**common-frontend 的 createAPIClient 设计：**

位置：`common-frontend/basic/src/composables/useAuth.js`

```javascript
export function createAPIClient(getAuthStore, options = {}) {
  const {
    extractData = true,  // ← 默认开启自动提取
    // ...
  } = options

  // 响应拦截器
  client.interceptors.response.use(
    (response) => {
      const processedResponse = refreshOnFulfilled(response)
      return extractData ? processedResponse.data : processedResponse
      //                    ^^^^^^^^^^^^^^^^^^^^ 已经提取了 .data
    },
    // ...
  )
}
```

**为什么问题被隐藏：**
- 后端日志显示成功（200 状态）
- 无 JavaScript 运行时错误（`undefined.data` 不抛出异常）
- 用户体验误导（"暂无数据"让用户以为数据库是空的）
- 开发者习惯性写 `.data` 而不知道已自动提取

#### 解决方案

**关键发现：不同 API 的响应格式不一致**

检查后端代码发现：
- **PreviewTable API**（`/api/data-explorer/preview`）：直接返回数据对象，无 `{data: ...}` 包装
- **ListResources API**（`/api/data-explorer/resources`）：返回 `{data: resources}` 格式
- **GetTree API**（`/api/data-explorer/tree`）：返回 `{data: tree}` 格式

因此 **只需修改预览 API 的调用**，资源列表和树接口保持原样。

**修改：数据预览（唯一需要修改的地方）**

文件：`manager/frontend/src/views/DataExplorer.vue`，第 532 行

```diff
const response = await dataExplorerAPI.getPreview(params)
- previewData.value = normalizePreviewPayload(response.data, selectedNode.value)
+ previewData.value = normalizePreviewPayload(response, selectedNode.value)
```

**不需要修改的地方**（保持 `.data` 访问）：
- 第 376 行：`response.data` ✅ 正确（ListResources 返回 `{data: ...}`）
- 第 476 行：`response.data` ✅ 正确（GetTree 返回 `{data: ...}`）

#### 验证方法

1. **修改代码后重启前端**
   ```bash
   # Manager 前端会自动热重载
   # 如果未生效，手动重启
   bash scripts/dev/restart.sh
   ```

2. **浏览器测试**
   - 刷新 Manager 页面
   - 点击左侧树中的表格节点
   - 应该看到表格数据而不是"暂无数据"

3. **添加临时调试（可选）**
   ```javascript
   const response = await dataExplorerAPI.getPreview(params)
   console.log('🔍 API 响应:', response)
   console.log('🔍 Response 类型:', typeof response)
   console.log('🔍 Response 键:', Object.keys(response || {}))
   ```

#### 预防措施

1. **后端响应格式规范化（建议）**
   - 统一所有 API 使用 `c.JSON(http.StatusOK, gin.H{"data": ...})` 格式
   - 或统一所有 API 直接返回数据对象
   - 当前混合格式容易导致混淆

2. **代码审查检查清单**
   - 检查后端 handler 的响应格式（是否有 `gin.H{"data": ...}` 包装）
   - 前端调用时根据后端格式决定是否使用 `.data`
   - 搜索 `response.data.data`（双重访问）避免错误

3. **使用 TypeScript（推荐）**
   ```typescript
   // 类型定义明确返回值
   function getPreview(params): Promise<TablePreview>  // 直接返回数据
   function getResources(): Promise<{data: Resource[]}>  // 包装格式
   ```

#### 相关问题

如果遇到类似"数据加载成功但不显示"的问题，检查以下几点：

1. **后端 API 是否返回 200**
   - 打开浏览器开发者工具 → Network 标签
   - 检查响应状态码和响应体

2. **前端是否正确接收数据**
   - 在控制台打印 `response` 对象
   - 检查是否误用了 `.data`

3. **预览组件是否匹配数据格式**
   - 检查 `mode` 字段是否存在
   - 检查预览插件的 `canHandle` 函数

#### 修复日期

- **发现日期：** 2025-12-18
- **修复版本：** v0.0.15+
- **影响范围：** Manager 模块数据浏览器

---

## 后端问题

（待补充）

---

## 数据库问题

（待补充）

---

## 网络问题

（待补充）

---

## 性能问题

（待补充）

---

## 更新日志

| 日期 | 问题 | 修复人员 |
|------|------|---------|
| 2025-12-18 | Manager 数据预览"暂无数据"（双重 .data 访问） | Claude Code |
