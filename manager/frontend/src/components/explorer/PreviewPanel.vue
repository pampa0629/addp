<template>
  <el-card shadow="never" class="preview-panel">
    <template #header>
      <div class="panel-header">
        <span>{{ title }}</span>
      </div>
    </template>

    <div v-if="!selectedNode" class="empty-state">
      <el-empty description="从左侧选择数据查看预览" />
    </div>

    <component
      v-else-if="previewComponent"
      :is="previewComponent"
      :data="previewData"
      :loading="loading"
      @page-change="handlePageChange"
      @navigate="handleNavigate"
    />

    <div v-else class="empty-state">
      <el-empty description="暂无可展示内容" />
    </div>
  </el-card>
</template>

<script setup>
import { computed } from 'vue'
import { getPreviewComponent } from '@/plugins/previews'

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

const previewComponent = computed(() => {
  if (!props.previewData) return null
  return getPreviewComponent(props.previewData)
})

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

const handleNavigate = (target) => {
  emit('navigate', target)
}
</script>

<style scoped>
.preview-panel {
  flex: 1;
  min-height: 600px;
  display: flex;
  flex-direction: column;
}

.preview-panel :deep(.el-card__body) {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.panel-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.empty-state {
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
}
</style>
