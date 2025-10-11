<template>
  <div class="pdf-preview">
    <!-- PDF 工具栏 -->
    <div class="pdf-toolbar">
      <div class="toolbar-left">
        <el-button-group size="small">
          <el-button :disabled="currentPage <= 1" @click="prevPage">
            <el-icon><ArrowLeft /></el-icon>
            上一页
          </el-button>
          <el-button :disabled="currentPage >= totalPages" @click="nextPage">
            下一页
            <el-icon><ArrowRight /></el-icon>
          </el-button>
        </el-button-group>

        <span class="page-info">
          <el-input-number
            v-model="currentPage"
            :min="1"
            :max="totalPages"
            size="small"
            controls-position="right"
            @change="handlePageChange"
            style="width: 100px;"
          />
          / {{ totalPages }}
        </span>
      </div>

      <div class="toolbar-right">
        <el-button-group size="small">
          <el-button @click="zoomOut" :disabled="scale <= 0.5">
            <el-icon><ZoomOut /></el-icon>
          </el-button>
          <el-button @click="resetZoom">
            {{ Math.round(scale * 100) }}%
          </el-button>
          <el-button @click="zoomIn" :disabled="scale >= 3">
            <el-icon><ZoomIn /></el-icon>
          </el-button>
        </el-button-group>

        <el-button size="small" @click="downloadPDF">
          <el-icon><Download /></el-icon>
          下载
        </el-button>
      </div>
    </div>

    <!-- PDF 渲染区域 -->
    <div class="pdf-container" ref="containerRef" v-loading="loading">
      <div v-if="error" class="error-message">
        <el-alert type="error" :title="error" :closable="false">
          <template #default>
            <p>{{ errorDetail }}</p>
            <el-button size="small" @click="fallbackToIframe">
              尝试使用浏览器原生预览
            </el-button>
          </template>
        </el-alert>
      </div>

      <!-- PDF.js 渲染 -->
      <canvas
        v-show="!error && !useFallback"
        ref="canvasRef"
        class="pdf-canvas"
      ></canvas>

      <!-- 降级方案: iframe -->
      <iframe
        v-if="useFallback && pdfUrl"
        :src="pdfUrl"
        class="pdf-iframe"
        frameborder="0"
      ></iframe>

      <!-- 无数据提示 -->
      <div v-if="!loading && !pdfUrl && !error" class="empty-state">
        <el-empty description="无法获取 PDF 文件" />
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, watch } from 'vue'
import { ArrowLeft, ArrowRight, ZoomIn, ZoomOut, Download } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'

const props = defineProps({
  data: {
    type: Object,
    required: true
  }
})

// 状态
const loading = ref(false)
const error = ref('')
const errorDetail = ref('')
const useFallback = ref(false)
const currentPage = ref(1)
const totalPages = ref(0)
const scale = ref(1.0)

// DOM 引用
const containerRef = ref(null)
const canvasRef = ref(null)

// PDF 实例
let pdfDocument = null
let pdfLib = null

// ✅ 页面缓存: 避免重复渲染相同页面
const pageCache = new Map()

// PDF URL
const pdfUrl = computed(() => {
  // 优先使用 download_url
  if (props.data?.object?.download_url) {
    return props.data.object.download_url
  }

  // 如果有 base64 数据,转换为 blob URL
  const base64Data = props.data?.object?.content?.pdf_data || props.data?.object?.content?.data
  if (base64Data) {
    try {
      const binaryString = atob(base64Data)
      const bytes = new Uint8Array(binaryString.length)
      for (let i = 0; i < binaryString.length; i++) {
        bytes[i] = binaryString.charCodeAt(i)
      }
      const blob = new Blob([bytes], { type: 'application/pdf' })
      return URL.createObjectURL(blob)
    } catch (err) {
      console.error('转换 PDF base64 失败', err)
      return null
    }
  }

  // 尝试构造 URL (如果后端提供了 path)
  const path = props.data?.object?.path
  const resourceId = props.data?.resourceId || props.data?.object?.resource_id
  if (path && resourceId) {
    return `/api/preview/download?resource_id=${resourceId}&path=${encodeURIComponent(path)}`
  }

  return null
})

const fileName = computed(() => {
  return props.data?.object?.path?.split('/').pop() || 'document.pdf'
})

/**
 * 加载 PDF.js 库
 */
const loadPDFJS = async () => {
  if (pdfLib) return pdfLib

  try {
    // 尝试从 CDN 加载 PDF.js
    if (!window.pdfjsLib) {
      await new Promise((resolve, reject) => {
        const script = document.createElement('script')
        script.src = 'https://cdn.jsdelivr.net/npm/pdfjs-dist@3.11.174/build/pdf.min.js'
        script.onload = resolve
        script.onerror = reject
        document.head.appendChild(script)
      })

      // 设置 worker
      window.pdfjsLib.GlobalWorkerOptions.workerSrc =
        'https://cdn.jsdelivr.net/npm/pdfjs-dist@3.11.174/build/pdf.worker.min.js'
    }

    pdfLib = window.pdfjsLib
    return pdfLib
  } catch (err) {
    console.error('加载 PDF.js 失败', err)
    throw new Error('无法加载 PDF 渲染引擎')
  }
}

/**
 * 加载 PDF 文档
 */
const loadPDF = async () => {
  if (!pdfUrl.value) {
    error.value = '无法获取 PDF 文件'
    return
  }

  loading.value = true
  error.value = ''
  errorDetail.value = ''

  try {
    // 加载 PDF.js
    const lib = await loadPDFJS()

    // ✅ 优化: 使用流式加载配置
    const loadingTask = lib.getDocument({
      url: pdfUrl.value,

      // 关键优化: 启用范围请求 (HTTP Range Requests)
      rangeChunkSize: 65536,       // 每次请求 64KB 分块
      disableAutoFetch: true,       // 禁用自动预加载所有页面
      disableStream: false,         // 启用流式传输

      // 性能优化
      enableXfa: false,             // 禁用 XFA 表单渲染(提升性能)

      // 支持认证
      withCredentials: true
    })

    // ✅ 监听加载进度 (可用于显示进度条)
    loadingTask.onProgress = (progressData) => {
      if (progressData.total > 0) {
        const percent = (progressData.loaded / progressData.total * 100).toFixed(1)
        console.log(`📄 PDF 加载进度: ${percent}%`)
        // 未来可以添加进度条: loadingProgress.value = percent
      }
    }

    pdfDocument = await loadingTask.promise

    totalPages.value = pdfDocument.numPages
    currentPage.value = 1

    // 渲染第一页
    await renderPage(1)

    console.log(`✅ PDF 加载成功: ${totalPages.value} 页 (流式加载模式)`)
  } catch (err) {
    console.error('加载 PDF 失败', err)
    error.value = 'PDF 加载失败'
    errorDetail.value = err.message || '未知错误'

    // 如果是跨域问题或其他加载问题,自动切换到 fallback
    if (err.name === 'MissingPDFException' || err.message.includes('CORS')) {
      fallbackToIframe()
    }
  } finally {
    loading.value = false
  }
}

/**
 * 渲染指定页面
 */
const renderPage = async (pageNum) => {
  if (!pdfDocument || !canvasRef.value) return

  try {
    // ✅ 缓存键: 页码 + 缩放比例
    const cacheKey = `${pageNum}-${scale.value.toFixed(2)}`

    // ✅ 检查缓存
    if (pageCache.has(cacheKey)) {
      const cachedImageData = pageCache.get(cacheKey)
      const canvas = canvasRef.value
      const context = canvas.getContext('2d')

      // 恢复画布尺寸
      canvas.width = cachedImageData.width
      canvas.height = cachedImageData.height

      // 从缓存恢复图像
      context.putImageData(cachedImageData, 0, 0)
      console.log(`📦 使用缓存: 第 ${pageNum} 页 (${scale.value}x)`)
      return
    }

    // 获取页面
    const page = await pdfDocument.getPage(pageNum)
    const viewport = page.getViewport({ scale: scale.value })

    const canvas = canvasRef.value
    const context = canvas.getContext('2d')

    canvas.width = viewport.width
    canvas.height = viewport.height

    const renderContext = {
      canvasContext: context,
      viewport: viewport
    }

    // 渲染页面
    await page.render(renderContext).promise

    // ✅ 缓存渲染结果
    const imageData = context.getImageData(0, 0, canvas.width, canvas.height)
    pageCache.set(cacheKey, imageData)

    // ✅ 限制缓存大小 (最多缓存 10 页)
    if (pageCache.size > 10) {
      const firstKey = pageCache.keys().next().value
      pageCache.delete(firstKey)
      console.log(`🗑️  清理缓存: ${firstKey}`)
    }

    console.log(`✅ 渲染完成: 第 ${pageNum} 页 (${scale.value}x)`)
  } catch (err) {
    console.error('渲染 PDF 页面失败', err)
    ElMessage.error('渲染失败: ' + err.message)
  }
}

/**
 * 页面导航
 */
const prevPage = () => {
  if (currentPage.value > 1) {
    currentPage.value--
    renderPage(currentPage.value)
  }
}

const nextPage = () => {
  if (currentPage.value < totalPages.value) {
    currentPage.value++
    renderPage(currentPage.value)
  }
}

const handlePageChange = (page) => {
  if (page >= 1 && page <= totalPages.value) {
    renderPage(page)
  }
}

/**
 * 缩放控制
 */
const zoomIn = () => {
  scale.value = Math.min(3, scale.value + 0.25)
  renderPage(currentPage.value)
}

const zoomOut = () => {
  scale.value = Math.max(0.5, scale.value - 0.25)
  renderPage(currentPage.value)
}

const resetZoom = () => {
  scale.value = 1.0
  renderPage(currentPage.value)
}

/**
 * 下载 PDF
 */
const downloadPDF = () => {
  if (!pdfUrl.value) {
    ElMessage.warning('无法下载 PDF')
    return
  }

  const link = document.createElement('a')
  link.href = pdfUrl.value
  link.download = fileName.value
  link.click()
}

/**
 * 降级到 iframe 方案
 */
const fallbackToIframe = () => {
  useFallback.value = true
  error.value = ''
  errorDetail.value = ''
  ElMessage.info('已切换到浏览器原生预览模式')
}

// 监听数据变化，自动重新加载
watch(
  () => props.data,
  (newData, oldData) => {
    const newPath = newData?.object?.path
    const oldPath = oldData?.object?.path

    if (newPath && newPath !== oldPath) {
      console.log(`🔄 PDF 文件切换: ${oldPath} → ${newPath}`)
      // 重置状态
      currentPage.value = 1
      pageCache.clear()
      if (pdfUrl.value) {
        loadPDF()
      }
    }
  },
  { deep: true }
)

onMounted(() => {
  if (pdfUrl.value) {
    loadPDF()
  }
})
</script>

<style scoped>
.pdf-preview {
  display: flex;
  flex-direction: column;
  height: 100%;
  gap: 12px;
}

.pdf-toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 8px 12px;
  background: var(--el-fill-color);
  border-radius: 4px;
  flex-shrink: 0;
}

.toolbar-left,
.toolbar-right {
  display: flex;
  align-items: center;
  gap: 12px;
}

.page-info {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
  color: var(--el-text-color-regular);
}

.pdf-container {
  flex: 1;
  overflow: auto;
  background: var(--el-fill-color-lighter);
  border: 1px solid var(--el-border-color-light);
  border-radius: 6px;
  display: flex;
  justify-content: center;
  align-items: flex-start;
  padding: 20px;
  min-height: 500px;
}

.pdf-canvas {
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1);
  background: white;
  max-width: 100%;
  height: auto;
}

.pdf-iframe {
  width: 100%;
  height: 100%;
  min-height: 600px;
  background: white;
}

.error-message {
  width: 100%;
  max-width: 600px;
}

.empty-state {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 100%;
  height: 100%;
}
</style>
