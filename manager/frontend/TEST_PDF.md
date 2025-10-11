# PDF 预览功能测试指南

## 🧪 快速测试

### 步骤 1: 启动项目

```bash
# 启动后端 (Manager 服务)
cd manager/backend
go run cmd/server/main.go

# 启动前端
cd manager/frontend
npm run dev
```

### 步骤 2: 准备测试 PDF

**方式1: 使用系统自带 PDF**
```bash
# macOS
open -a Preview /System/Library/CoreServices/Sample.pdf
# 或任何系统自带的 PDF 文件
```

**方式2: 生成简单 PDF**
```bash
# 使用在线工具生成: https://www.pdf-online.com/osa/convert.aspx
# 或在浏览器打印页面为 PDF
```

**方式3: 下载示例 PDF**
```bash
curl -O https://www.w3.org/WAI/ER/tests/xhtml/testfiles/resources/pdf/dummy.pdf
```

### 步骤 3: 上传到对象存储

假设你已配置 MinIO 资源:

```bash
# 使用 MinIO 客户端
mc alias set myminio http://localhost:9000 minioadmin minioadmin
mc cp dummy.pdf myminio/test-bucket/documents/

# 或通过 MinIO Web UI
# 访问: http://localhost:9001
# 上传到 test-bucket/documents/ 目录
```

### 步骤 4: 在 DataExplorer 中查看

1. 打开浏览器: http://localhost:5174
2. 点击左侧树中的 MinIO 资源
3. 展开 test-bucket → documents
4. 点击 dummy.pdf

**预期效果**:
- ✅ 显示 PDF 工具栏 (页面导航、缩放、下载)
- ✅ PDF 内容正常渲染
- ✅ 可以翻页、缩放
- ✅ 可以下载原文件

---

## 🔍 测试场景

### 场景1: 正常 PDF 文件

**输入**: 标准 PDF 文件 (如 dummy.pdf)
**预期**: 正常渲染,所有功能可用

### 场景2: 大 PDF 文件

**输入**: 大于 10MB 的 PDF
**预期**:
- 加载时显示 loading
- 渲染成功后可正常使用
- 性能可能稍慢,但可用

### 场景3: 损坏的 PDF

**输入**: 损坏或无效的 PDF 文件
**预期**:
- 显示错误信息
- 提供 "切换到浏览器原生预览" 按钮
- 点击后尝试用 iframe 加载

### 场景4: 无 PDF.js 的情况

**测试方法**:
1. 打开浏览器开发者工具
2. Network 标签中,右键 cdn.jsdelivr.net 域名
3. 选择 "Block request domain"
4. 刷新页面

**预期**:
- 显示 "无法加载 PDF 渲染引擎" 错误
- 自动切换到 iframe 降级模式

### 场景5: CORS 错误

**测试方法**: 上传 PDF 到不支持 CORS 的对象存储

**预期**:
- 显示 CORS 错误
- 提供降级选项

---

## 🎯 功能检查清单

### 基础功能
- [ ] PDF 文件正确识别 (不误识别为文本)
- [ ] PDF 正常渲染显示
- [ ] 显示正确的总页数

### 页面导航
- [ ] 上一页按钮功能正常
- [ ] 下一页按钮功能正常
- [ ] 首页/末页按钮禁用状态正确
- [ ] 输入页码跳转功能正常
- [ ] 页码范围限制正常 (不能输入 0 或超过总页数)

### 缩放功能
- [ ] 放大按钮功能正常
- [ ] 缩小按钮功能正常
- [ ] 重置缩放功能正常
- [ ] 缩放比例显示正确
- [ ] 缩放范围限制正常 (50%-300%)
- [ ] 缩放后画布更新正确

### 下载功能
- [ ] 下载按钮功能正常
- [ ] 下载的文件名正确
- [ ] 下载的文件完整可用

### 错误处理
- [ ] 加载失败显示错误信息
- [ ] 提供降级选项
- [ ] 降级模式正常工作

### 性能
- [ ] 加载速度可接受 (< 3秒 for 小文件)
- [ ] 翻页响应迅速
- [ ] 缩放响应迅速
- [ ] 没有内存泄漏 (多次切换文件)

---

## 📊 性能基准

### 小文件 (< 1MB)
- 加载时间: < 1秒
- 渲染时间: < 500ms
- 翻页延迟: < 100ms

### 中等文件 (1-5MB)
- 加载时间: 1-3秒
- 渲染时间: < 1秒
- 翻页延迟: < 200ms

### 大文件 (5-20MB)
- 加载时间: 3-10秒
- 渲染时间: 1-3秒
- 翻页延迟: < 500ms

---

## 🐛 常见问题

### Q: PDF 不显示,控制台报 "Failed to load PDF.js"

**A**: 网络问题,PDF.js CDN 无法访问
```
解决:
1. 检查网络连接
2. 尝试 VPN 或更换 DNS
3. 或本地部署 PDF.js (修改 PdfPreview.vue 中的 CDN 链接)
```

### Q: 显示 "CORS policy blocked"

**A**: 跨域问题
```
解决:
1. 配置对象存储 CORS 策略
2. 使用后端代理返回 PDF
3. 点击 "切换到浏览器原生预览"
```

### Q: 大 PDF 文件渲染卡顿

**A**: 性能问题
```
优化:
1. 降低缩放比例 (减小画布尺寸)
2. 使用 iframe 降级模式
3. 后端实现分页加载
```

---

## 🔧 调试技巧

### 查看 PDF 加载日志

```javascript
// 在浏览器控制台执行
localStorage.setItem('pdfjs.verbosity', '5')  // 启用详细日志
location.reload()

// 查看日志后关闭
localStorage.removeItem('pdfjs.verbosity')
location.reload()
```

### 测试不同 PDF

```javascript
// 创建测试 PDF URL
const testUrls = [
  'https://www.w3.org/WAI/ER/tests/xhtml/testfiles/resources/pdf/dummy.pdf',
  'https://www.adobe.com/support/products/enterprise/knowledgecenter/media/c4611_sample_explain.pdf'
]

// 手动测试
testUrls.forEach(url => {
  console.log('Testing:', url)
  // 在组件中手动设置 pdfUrl 进行测试
})
```

### 模拟后端响应

```javascript
// 在浏览器控制台
const mockData = {
  mode: 'object',
  object: {
    path: 'test.pdf',
    content_type: 'application/pdf',
    download_url: 'https://example.com/test.pdf'
  }
}

// 检查插件是否识别
import('@/plugins/previews').then(m => {
  const component = m.getPreviewComponent(mockData)
  console.log('Selected component:', component)
})
```

---

## 📸 测试截图位置

建议保存测试截图到:
```
manager/frontend/test-screenshots/
├── pdf-preview-normal.png      # 正常预览
├── pdf-preview-zoomed.png      # 放大状态
├── pdf-preview-navigation.png  # 页面导航
├── pdf-preview-error.png       # 错误状态
└── pdf-preview-fallback.png    # 降级模式
```

---

## ✅ 测试报告模板

```markdown
# PDF 预览功能测试报告

**测试日期**: 2025-01-XX
**测试人员**: XXX
**环境**: macOS/Windows/Linux + Chrome/Firefox/Safari

## 测试结果

| 功能项 | 状态 | 备注 |
|-------|------|------|
| PDF 识别 | ✅/❌ | |
| 正常渲染 | ✅/❌ | |
| 页面导航 | ✅/❌ | |
| 缩放功能 | ✅/❌ | |
| 下载功能 | ✅/❌ | |
| 错误处理 | ✅/❌ | |
| 性能表现 | ✅/❌ | 加载时间: Xs |

## 发现的问题

1. [问题描述]
   - 重现步骤: ...
   - 预期行为: ...
   - 实际行为: ...

## 改进建议

1. [建议]
```

---

**准备好测试了吗? 启动项目开始吧! 🚀**
