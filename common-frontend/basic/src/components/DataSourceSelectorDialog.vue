<template>
  <el-dialog
    :model-value="visible"
    :title="title"
    :width="width"
    @close="handleClose"
  >
    <div v-loading="loading">
      <DataSourceSelector
        ref="selectorRef"
        v-bind="selectorProps"
        @update:selection="handleSelectionUpdate"
        @loading-change="loading = $event"
        @error="$emit('error', $event)"
      />
    </div>

    <template #footer>
      <el-button @click="handleCancel">{{ cancelText }}</el-button>
      <el-button
        type="primary"
        :disabled="!hasSelection || loading"
        @click="handleConfirm"
      >
        {{ confirmText }}
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref, computed } from 'vue'
import DataSourceSelector from './DataSourceSelector.vue'

const props = defineProps({
  // Dialog 配置
  visible: {
    type: Boolean,
    required: true
  },
  title: {
    type: String,
    default: '选择数据源'
  },
  width: {
    type: String,
    default: '700px'
  },
  confirmText: {
    type: String,
    default: '确定'
  },
  cancelText: {
    type: String,
    default: '取消'
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
  'update:visible',
  'confirm',
  'cancel',
  'error'
])

// 状态
const selectorRef = ref(null)
const currentSelection = ref(null)
const loading = ref(false)

// 转发给 DataSourceSelector 的 props
const selectorProps = computed(() => {
  const {
    visible: _removed1,
    title: _removed2,
    width: _removed3,
    confirmText: _removed4,
    cancelText: _removed5,
    ...rest
  } = props

  return rest
})

// 是否有选择
const hasSelection = computed(() => {
  if (props.selectionMode === 'multiple') {
    return currentSelection.value && currentSelection.value.length > 0
  }
  return !!currentSelection.value
})

// 处理选择更新
const handleSelectionUpdate = (selection) => {
  currentSelection.value = selection
}

// 处理关闭
const handleClose = () => {
  emit('update:visible', false)
}

// 处理取消
const handleCancel = () => {
  emit('cancel')
  handleClose()
}

// 处理确认
const handleConfirm = () => {
  if (!hasSelection.value) {
    return
  }

  emit('confirm', currentSelection.value)
  handleClose()
}
</script>

<style scoped>
/* Dialog 内容样式 */
:deep(.el-dialog__body) {
  padding: 20px;
}
</style>
