<template>
  <button type="button" class="resource-cell" @click="emit('open', identity.resource)">
    <span class="resource-name">{{ identity.name }}</span>
    <span class="resource-path">{{ identity.path }}</span>
    <span class="resource-meta">
      <el-tag size="small" effect="plain">{{ identity.itemType }}</el-tag>
      <span>{{ identity.engine }}</span>
    </span>
  </button>
</template>

<script setup>
defineProps({
  identity: { type: Object, required: true }
})

const emit = defineEmits(['open'])
</script>

<style scoped>
.resource-cell { display: flex; width: 100%; flex-direction: column; align-items: flex-start; gap: 5px; padding: 4px 0; color: inherit; text-align: left; border: 0; background: transparent; cursor: pointer; }
.resource-name { color: var(--el-color-primary); font-size: 15px; font-weight: 600; }
.resource-path { max-width: 100%; overflow: hidden; color: var(--addp-text-secondary); text-overflow: ellipsis; white-space: nowrap; }
.resource-meta { display: flex; align-items: center; gap: 8px; color: var(--addp-text-tertiary); font-size: 12px; }
</style>
