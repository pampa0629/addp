<template>
  <button type="button" class="resource-cell" @click="emit('open', row)">
    <span class="resource-name">{{ resourceName(row) }}</span>
    <span class="resource-path">{{ resourcePath(row) }}</span>
    <span class="resource-meta">
      <el-tag size="small" effect="plain">{{ itemTypeLabel(row.target_snapshot?.item_type) }}</el-tag>
      <span>{{ engineLabel(row.target_snapshot?.engine_id) }}</span>
    </span>
  </button>
</template>

<script setup>
defineProps({
  row: { type: Object, required: true },
  resourceName: { type: Function, required: true },
  resourcePath: { type: Function, required: true },
  itemTypeLabel: { type: Function, required: true },
  engineLabel: { type: Function, required: true }
})

const emit = defineEmits(['open'])
</script>

<style scoped>
.resource-cell { display: flex; width: 100%; flex-direction: column; align-items: flex-start; gap: 5px; padding: 4px 0; color: inherit; text-align: left; border: 0; background: transparent; cursor: pointer; }
.resource-name { color: var(--el-color-primary); font-size: 15px; font-weight: 600; }
.resource-path { max-width: 100%; overflow: hidden; color: var(--addp-text-secondary); text-overflow: ellipsis; white-space: nowrap; }
.resource-meta { display: flex; align-items: center; gap: 8px; color: var(--addp-text-tertiary); font-size: 12px; }
</style>
