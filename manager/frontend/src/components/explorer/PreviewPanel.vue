<template>
  <el-card shadow="never" class="preview-panel">
    <template #header>
      <div class="panel-header">
        <span>{{ title }}</span>
      </div>
    </template>

    <!-- 无选择节点 -->
    <div v-if="!selectedNode" class="empty-state">
      <el-empty description="从左侧选择数据查看预览" />
    </div>

    <!-- 无预览数据 -->
    <div v-else-if="!previewData" class="empty-state">
      <el-empty description="暂无数据" />
    </div>

    <!-- 无可用预览组件 -->
    <div v-else-if="!hasPreviewComponent" class="empty-state">
      <el-empty description="暂不支持该文件类型的预览">
        <template #description>
          <p>不支持 {{ fileExtension || '该类型' }} 文件的在线预览</p>
          <p style="font-size: 12px; color: #909399; margin-top: 8px;">
            支持的格式：PDF、DOCX、PPTX、图片、JSON、GeoJSON、文本
          </p>
        </template>
      </el-empty>
    </div>

    <!-- 渲染预览组件 -->
    <div v-else class="preview-content">
      <!-- 使用 v-if 替代 component :is 以避免卸载时的 null 引用问题 -->
      <PdfPreview
        v-if="previewType === 'pdf'"
        :key="componentKey"
        :data="previewData"
        :loading="loading"
        @page-change="handlePageChange"
        @navigate="handleNavigate"
      />
      <DocxPreview
        v-else-if="previewType === 'docx'"
        :key="componentKey"
        :data="previewData"
        :loading="loading"
        @page-change="handlePageChange"
        @navigate="handleNavigate"
      />
      <PptxPreview
        v-else-if="previewType === 'pptx'"
        :key="componentKey"
        :data="previewData"
        :loading="loading"
        @page-change="handlePageChange"
        @navigate="handleNavigate"
      />
      <ImagePreview
        v-else-if="previewType === 'image'"
        :key="componentKey"
        :data="previewData"
        :loading="loading"
        @page-change="handlePageChange"
        @navigate="handleNavigate"
      />
      <GeoJsonPreview
        v-else-if="previewType === 'geojson'"
        :key="componentKey"
        :data="previewData"
        :loading="loading"
        @page-change="handlePageChange"
        @navigate="handleNavigate"
      />
      <JsonPreview
        v-else-if="previewType === 'json'"
        :key="componentKey"
        :data="previewData"
        :loading="loading"
        @page-change="handlePageChange"
        @navigate="handleNavigate"
      />
      <TablePreview
        v-else-if="previewType === 'table'"
        :key="componentKey"
        :data="previewData"
        :loading="loading"
        @page-change="handlePageChange"
        @navigate="handleNavigate"
      />
      <ObjectStoragePreview
        v-else-if="previewType === 'object-storage'"
        :key="componentKey"
        :data="previewData"
        :loading="loading"
        @page-change="handlePageChange"
        @navigate="handleNavigate"
      />
      <TextPreview
        v-else
        :key="componentKey"
        :data="previewData"
        :loading="loading"
        @page-change="handlePageChange"
        @navigate="handleNavigate"
      />
    </div>
  </el-card>
</template>

<script setup>
import { computed, watch } from 'vue'
import { getPreviewComponent } from '@/plugins/previews'
import PdfPreview from '@/components/previews/PdfPreview.vue'
import DocxPreview from '@/components/previews/DocxPreview.vue'
import PptxPreview from '@/components/previews/PptxPreview.vue'
import ImagePreview from '@/components/previews/ImagePreview.vue'
import GeoJsonPreview from '@/components/previews/GeoJsonPreview.vue'
import JsonPreview from '@/components/previews/JsonPreview.vue'
import TablePreview from '@/components/previews/TablePreview.vue'
import ObjectStoragePreview from '@/components/previews/ObjectStoragePreview.vue'
import TextPreview from '@/components/previews/TextPreview.vue'

const props = defineProps({
  selectedNode: {
    type: Object,
    default: null
  },
  previewData: {
    type: Object,
    default: null
  },
  loading: {
    type: Boolean,
    default: false
  }
})

const emit = defineEmits(['page-change', 'navigate'])

// 获取预览组件
const previewComponent = computed(() => {
  if (!props.previewData) {
    return null
  }

  try {
    const component = getPreviewComponent(props.previewData)
    if (component) {
      console.log('✅ 找到预览组件')
    } else {
      console.log('⚠️ 未找到匹配的预览组件')
    }
    return component
  } catch (error) {
    console.error('❌ 获取预览组件失败:', error)
    return null
  }
})

// 检查是否有可用的预览组件
const hasPreviewComponent = computed(() => {
  return previewComponent.value !== null && previewComponent.value !== undefined
})

// 获取预览类型名称（用于 v-if 渲染）
const previewType = computed(() => {
  if (!props.previewData) {
    return null
  }

  // 使用插件系统判断类型
  const component = getPreviewComponent(props.previewData)
  if (!component) {
    return 'text' // 默认使用 text 预览
  }

  // 根据 data 特征判断类型
  const mode = props.previewData.mode
  if (mode === 'table') {
    return 'table'
  }

  if (mode === 'object') {
    const nodeType = (props.previewData.object?.node_type || '').toLowerCase()
    if (['directory', 'prefix', 'bucket'].includes(nodeType)) {
      return 'object-storage'
    }

    const kind = (props.previewData.object?.content?.kind || '').toLowerCase()
    if (kind) {
      // 根据 kind 返回对应类型
      const kindMap = {
        'pdf': 'pdf',
        'docx': 'docx',
        'pptx': 'pptx',
        'image': 'image',
        'geojson': 'geojson',
        'json': 'json',
        'text': 'text',
        'unsupported': 'text'
      }
      return kindMap[kind] || 'text'
    }

    // 如果是 object 但没有 content，显示对象信息
    if (nodeType === 'object' && !props.previewData.object?.content) {
      return 'object-storage'
    }
  }

  return 'text' // 兜底
})

// 获取文件扩展名（用于错误提示）
const fileExtension = computed(() => {
  if (!props.selectedNode) return ''
  const path = props.selectedNode.path || props.selectedNode.label || ''
  const match = path.match(/\.([^.]+)$/)
  return match ? match[1].toUpperCase() : ''
})

// 生成组件唯一 key
const componentKey = computed(() => {
  if (!props.selectedNode || !props.previewData) {
    return 'empty'
  }

  const nodeId = props.selectedNode.id || ''
  const nodePath = props.selectedNode.path || props.selectedNode.table || ''
  const contentType = props.previewData?.object?.content_type || ''
  const contentKind = props.previewData?.object?.content?.kind || ''

  return `preview-${nodeId}-${nodePath}-${contentType}-${contentKind}`
})

// 监听数据变化，输出调试信息
watch(
  () => props.previewData,
  (newData) => {
    if (newData) {
      console.log('📦 PreviewPanel 收到新数据:', {
        mode: newData.mode,
        contentKind: newData.object?.content?.kind,
        contentType: newData.object?.content_type,
        previewType: previewType.value,
        hasComponent: hasPreviewComponent.value
      })
    }
  },
  { immediate: true, deep: true }
)

const title = computed(() => {
  if (!props.selectedNode) return '数据预览'

  const node = props.selectedNode
  const nodeType = node.nodeType || node.type

  // 对象存储类型
  if (['object', 'directory', 'bucket'].includes(nodeType)) {
    const path = node.path || node.table || ''
    if (path) {
      return `${node.schema}/${path} - 数据预览`
    }
    return `${node.schema || node.label || ''} - 数据预览`
  }

  // 表格类型
  if (node.schema && node.table) {
    return `${node.schema}.${node.table} - 数据预览`
  }

  return `${node.label || ''} - 数据预览`
})

const handlePageChange = (page) => {
  emit('page-change', page)
}

const handleNavigate = (path) => {
  emit('navigate', path)
}
</script>

<style scoped>
.preview-panel {
  height: 100%;
  display: flex;
  flex-direction: column;
}

.preview-panel :deep(.el-card__body) {
  flex: 1;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

.panel-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-weight: 600;
}

.empty-state {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
}

.preview-content {
  flex: 1;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}
</style>
