<template>
  <el-card :shadow="shadow" class="data-source-selector-card">
    <template v-if="showHeader" #header>
      <div class="card-header">
        <span class="title">{{ title }}</span>
        <slot name="header-actions"></slot>
      </div>
    </template>

    <div v-loading="loading">
      <DataSourceSelector
        ref="selectorRef"
        v-bind="selectorProps"
        @update:selection="handleSelectionUpdate"
        @loading-change="loading = $event"
        @error="$emit('error', $event)"
      />
    </div>
  </el-card>
</template>

<script setup>
import { ref, computed } from 'vue'
import DataSourceSelector from './DataSourceSelector.vue'

const props = defineProps({
  // Card 配置
  title: {
    type: String,
    default: '选择数据源'
  },
  shadow: {
    type: String,
    default: 'always',  // 'always' | 'hover' | 'never'
    validator: (value) => ['always', 'hover', 'never'].includes(value)
  },
  showHeader: {
    type: Boolean,
    default: true
  },

  // DataSourceSelector 的所有 props
  apiBaseUrl: {
    type: String,
    required: true
  },
  engineTypes: {
    type: Array,
    default: () => []
  },
  dataSourceTypes: {
    type: Array,
    default: () => []
  },
  nodeTypes: {
    type: Array,
    default: () => []
  },
  enableGeometryDetection: {
    type: Boolean,
    default: true
  },
  requireGeometry: {
    type: Boolean,
    default: false
  },
  selectionMode: {
    type: String,
    default: 'single'
  },
  selectableNodeTypes: {
    type: Array,
    default: () => ['table', 'object']
  },
  showEngineSelector: {
    type: Boolean,
    default: true
  },
  showGeometryTag: {
    type: Boolean,
    default: true
  },
  showSelectionInfo: {
    type: Boolean,
    default: true
  },
  treeHeight: {
    type: String,
    default: '400px'
  },
  initialEngineId: {
    type: Number,
    default: null
  }
})

const emit = defineEmits([
  'update:selection',
  'error'
])

// 状态
const selectorRef = ref(null)
const loading = ref(false)

// 转发给 DataSourceSelector 的 props
const selectorProps = computed(() => {
  const {
    title: _removed1,
    shadow: _removed2,
    showHeader: _removed3,
    ...rest
  } = props

  return rest
})

// 处理选择更新
const handleSelectionUpdate = (selection) => {
  emit('update:selection', selection)
}

// 暴露方法供父组件调用
const getSelection = () => {
  return selectorRef.value?.getSelection()
}

const clearSelection = () => {
  selectorRef.value?.clearSelection()
}

defineExpose({
  getSelection,
  clearSelection
})
</script>

<style scoped>
.data-source-selector-card {
  width: 100%;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.card-header .title {
  font-weight: 500;
  font-size: 16px;
}

:deep(.el-card__body) {
  padding: 20px;
}
</style>
