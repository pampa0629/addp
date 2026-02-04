<template>
  <el-dialog
    :model-value="visible"
    :title="title"
    :width="width"
    @update:model-value="$emit('update:visible', $event)"
    @close="handleClose"
  >
    <DataSourceCascader
      ref="cascaderRef"
      :api-base-url="apiBaseUrl"
      :engine-types="engineTypes"
      :data-source-types="dataSourceTypes"
      :selectable-node-types="selectableNodeTypes"
      :selection-mode="selectionMode"
      :enable-geometry-detection="enableGeometryDetection"
      :require-geometry="requireGeometry"
      :show-geometry-tag="showGeometryTag"
      :show-selection-info="showSelectionInfo"
      :initial-engine-id="initialEngineId"
      @update:selection="handleSelectionUpdate"
      @engine-change="$emit('engine-change', $event)"
      @level-change="$emit('level-change', $event)"
      @geometry-detected="$emit('geometry-detected', $event)"
      @loading-change="$emit('loading-change', $event)"
      @error="$emit('error', $event)"
    />

    <template #footer>
      <span class="dialog-footer">
        <el-button @click="handleCancel">{{ cancelText }}</el-button>
        <el-button
          type="primary"
          :disabled="!currentSelection"
          @click="handleConfirm"
        >
          {{ confirmText }}
        </el-button>
      </span>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref } from 'vue'
import DataSourceCascader from './DataSourceCascader.vue'

const props = defineProps({
  // Dialog 配置
  visible: {
    type: Boolean,
    default: false
  },
  title: {
    type: String,
    default: '选择数据源'
  },
  width: {
    type: String,
    default: '600px'
  },
  confirmText: {
    type: String,
    default: '确定'
  },
  cancelText: {
    type: String,
    default: '取消'
  },

  // DataSourceCascader props
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
  selectableNodeTypes: {
    type: Array,
    default: () => ['table', 'object']
  },
  selectionMode: {
    type: String,
    default: 'single'
  },
  enableGeometryDetection: {
    type: Boolean,
    default: true
  },
  requireGeometry: {
    type: Boolean,
    default: false
  },
  showGeometryTag: {
    type: Boolean,
    default: true
  },
  showSelectionInfo: {
    type: Boolean,
    default: true
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
  'update:selection',
  'engine-change',
  'level-change',
  'geometry-detected',
  'loading-change',
  'error'
])

const cascaderRef = ref(null)
const currentSelection = ref(null)

const handleSelectionUpdate = (selection) => {
  currentSelection.value = selection
  emit('update:selection', selection)
}

const handleConfirm = () => {
  if (!currentSelection.value) return
  emit('confirm', currentSelection.value)
  emit('update:visible', false)
}

const handleCancel = () => {
  emit('cancel')
  emit('update:visible', false)
}

const handleClose = () => {
  // 重置选择
  currentSelection.value = null
}

// 暴露方法
defineExpose({
  getSelection: () => cascaderRef.value?.getSelection(),
  clearSelection: () => cascaderRef.value?.clearSelection()
})
</script>

<style scoped>
.dialog-footer {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
</style>
