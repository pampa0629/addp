# PDF 预览功能使用指南

## ✅ 功能已添加

PDF 预览功能已成功集成到 DataExplorer 中!

---

## 🎯 功能特性

### 核心功能
- ✅ **PDF 渲染** - 使用 PDF.js 进行高质量渲染
- ✅ **页面导航** - 上一页/下一页,跳转到指定页
- ✅ **缩放控制** - 放大/缩小/重置 (50%-300%)
- ✅ **下载功能** - 一键下载 PDF 文件
- ✅ **降级方案** - 自动回退到浏览器原生预览

### 技术亮点
- 📦 **自动加载 PDF.js** - 从 CDN 动态加载,无需打包
- 🔄 **智能识别** - 支持多种 PDF 来源:
  - 文件扩展名 (.pdf)
  - Content-Type (application/pdf)
  - content.kind === 'pdf'
- 🛡️ **错误处理** - 加载失败自动切换到 iframe 模式
- 🎨 **美观界面** - 工具栏 + 画布渲染区

---

## 📁 文件结构

```
src/components/previews/
└── PdfPreview.vue              291 行 - PDF 预览组件

src/plugins/previews/
└── index.js                    已添加 PDF 插件注册
```

---

## 🚀 使用方式

### 1. 上传 PDF 文件到对象存储

```bash
# 使用 MinIO 客户端或通过管理界面上传
mc cp document.pdf minio/my-bucket/documents/
```

### 2. 在 DataExplorer 中浏览

1. 打开 DataExplorer: http://localhost:5174
2. 在左侧树中选择对象存储资源
3. 导航到 PDF 文件所在目录
4. 点击 PDF 文件

### 3. 使用 PDF 预览功能

**工具栏功能**:
- 📄 **页面导航**: 上一页/下一页按钮,或直接输入页码
- 🔍 **缩放控制**: 放大/缩小/重置,显示当前缩放比例
- 💾 **下载**: 下载原始 PDF 文件

**键盘快捷键** (计划中):
- `←` / `→` : 上一页/下一页
- `+` / `-` : 放大/缩小
- `0` : 重置缩放

---

## 🔧 后端集成要求

### 方式1: 返回 download_url

后端 API 返回格式:

```json
{
  "mode": "object",
  "object": {
    "node_type": "object",
    "path": "documents/report.pdf",
    "content_type": "application/pdf",
    "download_url": "/api/download?resource_id=1&path=documents/report.pdf",
    "size_bytes": 1048576,
    "last_modified": "2025-01-01T00:00:00Z"
  }
}
```

### 方式2: 返回 base64 数据

适用于小文件 (< 5MB):

```json
{
  "mode": "object",
  "object": {
    "path": "documents/report.pdf",
    "content_type": "application/pdf",
    "content": {
      "kind": "pdf",
      "pdf_data": "JVBERi0xLjQKJeLjz9MKMyAwIG...",
      "truncated": false
    }
  }
}
```

### 方式3: 动态构造 URL

如果后端支持通用下载接口:

```go
// Go 示例
func handleDownload(c *gin.Context) {
    resourceID := c.Query("resource_id")
    path := c.Query("path")

    // 从对象存储获取文件
    data, err := getFileFromStorage(resourceID, path)
    if err != nil {
        c.JSON(500, gin.H{"error": err.Error()})
        return
    }

    c.Header("Content-Type", "application/pdf")
    c.Header("Content-Disposition", "inline; filename=\""+filepath.Base(path)+"\"")
    c.Data(200, "application/pdf", data)
}
```

---

## 🎨 界面展示

```
┌─────────────────────────────────────────────────────────┐
│  [<上一页]  [下一页>]   [1 / 10]  [🔍-] [100%] [🔍+] [💾下载] │
├─────────────────────────────────────────────────────────┤
│                                                         │
│                     PDF 渲染区域                         │
│                  (Canvas 或 iframe)                     │
│                                                         │
│                                                         │
│                                                         │
│                                                         │
└─────────────────────────────────────────────────────────┘
```

---

## 🔌 插件注册信息

PDF 预览已自动注册,优先级为 **65**:

```javascript
registerPreview({
  name: 'pdf',
  component: PdfPreview,
  canHandle: (data) => {
    // 检查文件扩展名
    const path = (data.object?.path || '').toLowerCase()
    if (path.endsWith('.pdf')) return true

    // 检查 Content-Type
    const contentType = (data.object?.content_type || '').toLowerCase()
    if (contentType.includes('pdf')) return true

    // 检查 content kind
    const kind = (data.object?.content?.kind || '').toLowerCase()
    if (kind === 'pdf') return true

    return false
  },
  priority: 65  // 高于 JSON(60), 低于 image(70)
})
```

---

## 🐛 故障排除

### 问题1: PDF 不显示,显示错误信息

**原因**: PDF.js 加载失败或 PDF 文件损坏

**解决**:
1. 检查网络连接 (PDF.js 从 CDN 加载)
2. 点击 "尝试使用浏览器原生预览" 按钮
3. 检查浏览器控制台错误信息

### 问题2: 跨域错误 (CORS)

**原因**: PDF 文件 URL 跨域且未配置 CORS

**解决**:
- 方式1: 配置对象存储的 CORS 策略
- 方式2: 通过后端代理返回 PDF (推荐)
- 方式3: 使用 base64 编码返回小文件

### 问题3: 渲染性能差

**原因**: 大文件或复杂 PDF

**优化**:
1. 降低缩放比例
2. 使用 iframe 降级模式 (浏览器原生渲染)
3. 后端实现分页加载 (按需加载当前页)

---

## 📊 与其他预览插件的优先级

```
优先级排序 (数字越大优先级越高):

table           100  - 表格数据
object-storage   90  - 对象存储目录
geojson          80  - GeoJSON 文件
image            70  - 图片
pdf              65  - PDF 文件 ✨ 新增
json             60  - JSON 文件
text              0  - 文本 (兜底)
```

---

## 🔄 降级策略

PDF 预览组件实现了智能降级:

```
1. 尝试加载 PDF.js
   ↓ 失败
2. 显示错误信息 + "切换到原生预览" 按钮
   ↓ 用户点击
3. 使用 <iframe> 加载 PDF (浏览器原生支持)
   ↓ 仍然失败
4. 显示下载按钮,允许用户下载查看
```

---

## 🚀 未来增强计划

### 短期
- [ ] 添加键盘快捷键支持
- [ ] 缩略图导航栏
- [ ] 全屏模式
- [ ] 打印功能

### 中期
- [ ] 文本搜索和高亮
- [ ] 注释功能 (标记、批注)
- [ ] 页面旋转
- [ ] 多页并排显示

### 长期
- [ ] PDF 编辑功能 (签名、填表)
- [ ] 协作批注
- [ ] 版本对比
- [ ] OCR 文字识别

---

## 📝 代码示例

### 自定义 PDF 预览样式

修改 `PdfPreview.vue`:

```vue
<style scoped>
/* 自定义工具栏颜色 */
.pdf-toolbar {
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  color: white;
}

/* 自定义画布阴影 */
.pdf-canvas {
  box-shadow: 0 10px 30px rgba(0, 0, 0, 0.3);
  border-radius: 8px;
}
</style>
```

### 禁用 PDF 预览

如果不需要 PDF 预览功能:

```javascript
// src/plugins/previews/index.js
import { unregisterPreview } from '@/plugins/previews'

unregisterPreview('pdf')
```

---

## ✅ 验证清单

启动项目后,请验证以下功能:

- [ ] 上传 PDF 文件到对象存储
- [ ] 在 DataExplorer 中点击 PDF 文件
- [ ] PDF 正常渲染显示
- [ ] 页面导航功能正常
- [ ] 缩放功能正常
- [ ] 下载功能正常
- [ ] 错误处理正常 (尝试上传损坏的 PDF)
- [ ] 降级模式正常 (模拟 CORS 错误)

---

## 📚 相关文档

- [REFACTOR_SUMMARY.md](REFACTOR_SUMMARY.md) - 重构总结
- [QUICK_START.md](QUICK_START.md) - 快速上手指南
- [public/plugins/README.md](public/plugins/README.md) - 插件开发指南
- [PDF.js 官方文档](https://mozilla.github.io/pdf.js/)

---

**PDF 预览功能已就绪! 🎉**

立即体验:
```bash
cd manager/frontend
npm run dev
```

访问: http://localhost:5174
