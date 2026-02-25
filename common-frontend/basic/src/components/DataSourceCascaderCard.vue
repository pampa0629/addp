<template>
  <el-card :shadow="shadow" class="data-source-cascader-card">
    <template v-if="showHeader" #header>
      <div class="card-header">
        <span>{{ title }}</span>
        <el-button v-if="showClearButton" text size="small" @click="handleClear">
          清空
        </el-button>
      </div>
    </template>

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
      @update:selection="$emit('update:selection', $event)"
      @engine-change="$emit('engine-change', $event)"
      @level-change="$emit('level-change', $event)"
      @geometry-detected="$emit('geometry-detected', $event)"
      @loading-change="$emit('loading-change', $event)"
      @error="$emit('error', $event)"
    />
  </el-card>
</template>

<script setup>
import { ref } from 'vue'
import DataSourceCascader from './DataSourceCascader.vue'

const props = defineProps({
  // Card 配置
  title: {
    type: String,
    default: '数据源选择'
  },
  shadow: {
    type: String,
    default: 'never'
  },
  showHeader: {
    type: Boolean,
    default: true
  },
  showClearButton: {
    type: Boolean,
    default: true
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
  'update:selection',
  'engine-change',
  'level-change',
  'geometry-detected',
  'loading-change',
  'error'
])

const cascaderRef = ref(null)

const handleClear = () => {
  cascaderRef.value?.clearSelection()
}

// 暴露方法
defineExpose({
  getSelection: () => cascaderRef.value?.getSelection(),
  clearSelection: () => cascaderRef.value?.clearSelection()
})
</script>

<style scoped>
.data-source-cascader-card {
  width: 100%;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-weight: 500;
}
</style>
