<template>
  <DataSourceSelectorDialog
    :visible="visible"
    api-base-url="/api/service"
    title="选择数据表"
    :engine-types="['postgresql', 'mysql', 'doris', 'clickhouse', 'mongodb', 'spark', 'minio', 's3']"
    :selectable-node-types="['table']"
    enable-geometry-detection
    show-geometry-tag
    @confirm="handleConfirm"
    @cancel="handleCancel"
    @update:visible="$emit('update:visible', $event)"
  />
</template>

<script setup>
import { DataSourceSelectorDialog } from '@addp/common-frontend'

const props = defineProps({
  visible: {
    type: Boolean,
    default: false
  }
})

const emit = defineEmits(['update:visible', 'table-selected'])

// 处理确认
const handleConfirm = (selection) => {
  // 转换为旧的数据结构（保持向后兼容）
  const table = {
    engineId: selection.engineId,
    schema: selection.schema,
    tableName: selection.tableName,
    fullName: selection.fullName,
    label: selection.tableName,
    locator: selection.locator,
    geometryColumn: selection.geometryColumn || '',
    srid: selection.srid || 0,
    geometryType: selection.geometryType || '',
    hasGeometry: selection.hasGeometry
  }

  emit('table-selected', table)
}

// 处理取消
const handleCancel = () => {
  emit('update:visible', false)
}
</script>

<style scoped>
/* 无需额外样式，DataSourceSelectorDialog 已包含 */
</style>
