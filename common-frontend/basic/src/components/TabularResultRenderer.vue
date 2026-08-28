<template>
  <el-table :data="rows" border stripe height="100%" class="result-table" highlight-current-row @row-click="selectRow">
    <el-table-column
      v-for="column in visibleColumns"
      :key="column"
      :prop="column"
      :label="fieldLabels[column] || column"
      min-width="140"
      show-overflow-tooltip
    >
      <template #default="scope">{{ formatResultCell(scope.row?.[column]) }}</template>
    </el-table-column>
  </el-table>
</template>

<script setup>
import { computed } from 'vue'
import { formatResultCell, resultSelectionFromRow } from '../utils/tabularResult'

const props = defineProps({
  rows: { type: Array, default: () => [] },
  columns: { type: Array, default: () => [] },
  fields: { type: Array, default: () => [] }
})
const emit = defineEmits(['result-select'])

function selectRow(row) {
  const selection = resultSelectionFromRow(props.rows, row)
  if (selection) emit('result-select', selection)
}

const visibleColumns = computed(() => {
  if (props.columns.length > 0) return props.columns
  if (props.fields.length > 0) return props.fields.map((field) => field.name)
  return Object.keys(props.rows[0] || {})
})
const fieldLabels = computed(() => Object.fromEntries(
  props.fields.map((field) => [field.name, field.comment || field.name])
))
</script>

<style scoped>
.result-table { width: 100%; min-height: 240px; }
</style>
