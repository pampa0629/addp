<template>
  <div class="node-panel" v-if="selected">
    <div class="panel-header">
      <span class="panel-title">
        <el-tag
          v-if="selected.type === 'node'"
          :style="{
            backgroundColor: selected.visual_color || selected.color,
            borderColor: selected.visual_color || selected.color,
            color: selected.visual_label_color
          }"
          effect="dark"
          size="small"
        >
          {{ nodeTypeLabel }}
        </el-tag>
        <el-tag v-else type="info" size="small">{{ t('graph.nodePanel.relation') }}</el-tag>
        <span class="panel-name">{{ displayName }}</span>
      </span>
      <el-button :icon="Close" text size="small" @click="$emit('close')" />
    </div>

    <el-divider style="margin: 8px 0" />

    <div class="panel-body">
      <!-- 属性列表 -->
      <div class="section-title">{{ t('graph.nodePanel.properties') }}</div>
      <el-empty v-if="!hasProperties" :description="t('graph.nodePanel.noProperties')" :image-size="40" />
      <div v-else class="props-table">
        <div v-for="property in visibleProperties" :key="property.key" class="prop-row">
          <span class="prop-key">{{ property.key }}</span>
          <span class="prop-val">{{ formatValue(property.value) }}</span>
        </div>
      </div>
    </div>

    <!-- 节点操作按钮（仅节点有） -->
    <div v-if="selected.type === 'node'" class="panel-actions">
      <el-button size="small" type="primary" plain @click="$emit('expand', selected.id)">
        {{ t('graph.nodePanel.expandNeighbors') }}
      </el-button>
      <el-button size="small" type="warning" plain @click="$emit('set-path-node', selected.id)">
        {{ t('graph.nodePanel.setPathNode') }}
      </el-button>
    </div>
  </div>
  <div v-else class="panel-empty">
    <el-empty :description="t('graph.nodePanel.clickToView')" :image-size="60" />
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { Close } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const props = defineProps({
  selected: { type: Object, default: null }
})

defineEmits(['close', 'expand', 'set-path-node'])

const displayName = computed(() => {
  if (!props.selected) return ''
  if (props.selected.type === 'node') {
    return props.selected.display_name || props.selected.id
  }
  return props.selected.relation_type || props.selected.type || props.selected.id
})

const hasProperties = computed(() => {
  return visibleProperties.value.length > 0
})

const visibleProperties = computed(() => {
  const source = props.selected?.properties || {}
  return Object.entries(source)
    .filter(([key]) => !isTechnicalProperty(key))
    .map(([key, value]) => ({ key, value }))
})

const nodeTypeLabel = computed(() => {
  if (!props.selected || props.selected.type !== 'node') return ''
  if (props.selected.entity_type) return props.selected.entity_type
  if (Array.isArray(props.selected.labels) && props.selected.labels.length > 0) {
    return props.selected.labels.join('+')
  }
  return t('graph.nodePanel.node')
})

function formatValue(val) {
  if (val === null || val === undefined) return '—'
  if (typeof val === 'object') return JSON.stringify(val)
  return String(val)
}

function isTechnicalProperty(key) {
  return ['_created_at', '_updated_at', '_update_at', '_deleted_at'].includes(String(key || '').trim().toLowerCase())
}
</script>

<style scoped>
.node-panel {
  height: 100%;
  display: flex;
  flex-direction: column;
  padding: 12px;
  overflow: hidden;
}

.panel-empty {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 100%;
  color: var(--addp-text-tertiary);
}

.panel-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.panel-title {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: 1;
  overflow: hidden;
}

.panel-name {
  font-weight: 500;
  font-size: 13px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.panel-body {
  flex: 1;
  overflow-y: auto;
}

.section-title {
  font-size: 12px;
  color: var(--addp-text-tertiary);
  margin-bottom: 8px;
}

.props-table {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.prop-row {
  display: flex;
  gap: 8px;
  font-size: 12px;
  padding: 4px 0;
  border-bottom: 1px solid var(--addp-border-color-light);
}

.prop-key {
  color: var(--addp-text-secondary);
  min-width: 80px;
  flex-shrink: 0;
  font-weight: 500;
}

.prop-val {
  color: var(--addp-text-primary);
  word-break: break-all;
}

.panel-actions {
  padding-top: 12px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.panel-actions .el-button {
  width: 100%;
}
</style>
