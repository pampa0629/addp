<template>
  <el-card shadow="never" class="engine-panel">
    <template #header>
      <div class="panel-header">
        <span class="header-title">{{ t('manager.explorer.dataPreview') }}</span>
      </div>
    </template>

    <div v-if="!engine" class="empty-state">
      <el-empty :description="t('manager.explorer.selectDataToPreview')" />
    </div>

    <div v-else class="panel-content">
      <el-descriptions :column="1" border>
        <el-descriptions-item :label="t('manager.explorer.engineName')">{{ engine.name || '-' }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.explorer.engineType')">{{ engine.engine_type || '-' }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.explorer.hostPort')">{{ hostPort }}</el-descriptions-item>
        <el-descriptions-item :label="t('meta.nodeChildren')">{{ topNodeCount }}</el-descriptions-item>
        <el-descriptions-item :label="t('meta.itemCount')">{{ itemCount }}</el-descriptions-item>
      </el-descriptions>
    </div>
  </el-card>
</template>

<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const props = defineProps({
  engine: {
    type: Object,
    default: null
  },
  treeRoot: {
    type: Object,
    default: null
  }
})

const hostPort = computed(() => {
  const conn = props.engine?.connection_info || {}
  const host = conn.host || conn.hostname || conn.endpoint || '-'
  const port = conn.port ? `:${conn.port}` : ''
  return `${host}${port}`
})

const topNodeCount = computed(() => {
  return props.treeRoot?.children?.length || 0
})

const itemCount = computed(() => {
  const root = props.treeRoot
  if (!root) return 0

  const itemTypes = new Set(['table', 'view', 'collection', 'label', 'relationship', 'file', 'object'])
  let count = 0
  const walk = (node) => {
    if (!node) return
    if (itemTypes.has(node.type)) count += 1
    if (Array.isArray(node.children)) {
      node.children.forEach(walk)
    }
  }
  walk(root)
  return count
})
</script>

<style scoped>
.engine-panel {
  height: 100%;
  display: flex;
  flex-direction: column;
  border: none;
}

:deep(.el-card__body) {
  flex: 1;
  overflow: auto;
  min-height: 0;
}

.panel-content {
  padding: 8px 0;
}

.header-title {
  font-weight: 600;
}

.empty-state {
  padding-top: 40px;
}
</style>
